# Go Hotel Booking Backend — GCP Project Plan

## Goal

Rebuild the [hotel booking demo](https://github.com/sses79/hotel-booking) as a small Go project on Google Cloud. Complete the original challenge's API test cycle—reset, seed, search, availability, booking, lookup, and verification—before adding Pub/Sub and a separate event-handling GCF extension. Demonstrate Go services, Oracle SQL and transactions, Pub/Sub, cloud deployment, and production-minded delivery. The current implementation covers the local foundation: readiness, seed/reset, and hotel search. Availability, booking, and events remain later phases.

The [original challenge](https://github.com/sses79/hotel-booking/blob/dev/challenge.md), [.NET solution summary](https://github.com/sses79/hotel-booking/blob/dev/docs/hotel-booking-challenge-solution.md), and [.NET implementation](https://github.com/sses79/hotel-booking) are the primary behavior reference. The [Python project](https://github.com/sses79/hotel-booking-python) is a secondary reference for container workflow and a stronger booking-reference format. This Go project explores a different stack; it is not a submission that meets the original challenge's C#/EF Core requirement.

## Local-First Approach And Lessons From The Existing Demos

Start with a fully containerized local workflow before any GCP deployment. Both existing demos use Compose to run an API and SQL database; the Python demo also separates schema migration from API startup. Preserve the .NET demo's full reviewer workflow, partial hotel-name search, stable room ordering, and six-room Grand Plaza dataset. Translate its transaction behavior to Oracle Database instead of copying SQL Server or PostgreSQL locking, migrations, or cloud infrastructure.

The local Go stack uses **Oracle Database Free** in Docker, pinned to `gvenzl/oracle-free:23.26.3-slim` with native ARM64 and AMD64 support. Run the API through the same Go Functions Framework entry point intended for deployment. The database container creates the dedicated application user in `FREEPDB1`; a one-shot Go schema command applies ordered, versioned Oracle DDL to that schema. Provide `POST /api/v1/admin/seed` and `POST /api/v1/admin/reset` for the demo cycle; schema changes remain a separate command. A named volume preserves schema and data across ordinary restarts. Deleting that volume is an explicit local database teardown.

The first local milestone should be usable without installing Go or `gcloud` on the host: Docker and Docker Compose are the only required tools. Bind the source tree into a Go development container, keep the module cache in a named volume, and use `go run` for the local function server. A separate multi-stage Dockerfile should produce a small non-root image for deployment. Pin toolchain and dependency versions rather than relying on moving `latest` tags.

## Scope

### Core API demo

- Search hotels by an optional partial name and return a list, matching the .NET demo.
- List rooms available for a whole stay, filtered by dates, guest count, and optional room type.
- Create a booking for one suitable room and return a public booking reference.
- Retrieve a booking by reference.
- Expose API seed and reset operations for a complete, repeatable local/demo cycle.
- Serve OpenAPI and Swagger UI so the full cycle can be tested without writing a client.

### Pub/Sub and GCF extension

- Publish a `booking.created` event to Pub/Sub after a successful Oracle commit.
- Process the event in a separate Go CloudEvent function and record an idempotent delivery/audit result.

No frontend, payments, customer accounts, or real notifications are needed. Use fictional guest data in demos.

## Architecture

```mermaid
flowchart LR
    Client[API client] --> API[Go HTTP Cloud Run function]
    API -->|SQL read/write transaction| DB[(Oracle Database)]
    API -->|after commit| Topic[Pub/Sub booking.created]
    Topic --> Worker[Go event-driven Cloud Run function]
    Worker -->|idempotent audit write| DB
    Topic -. optional analytics .-> BQ[(BigQuery)]
```

The **local events profile** runs the HTTP API, the Pub/Sub-triggered Go function, and Oracle plus the Pub/Sub emulator in containers. Both functions use the same source image with different Functions Framework targets. A small local-only bridge translates Pub/Sub emulator push requests into Eventarc-style CloudEvents HTTP requests for the function; the cloud deployment uses Eventarc directly.

- Deploy the API and event consumer independently as **Cloud Run functions**, using the Go Functions Framework.
- Use one Oracle application schema as the operational SQL store. Use Go `database/sql` with the pure-Go `github.com/sijms/go-ora/v2` driver pinned to `v2.9.0`; no Oracle Instant Client or CGO is required. Spanner is fully replaced; dual-database support and existing-data migration are outside this change.
- Use Pub/Sub for asynchronous delivery. The consumer must tolerate duplicate delivery.
- Keep BigQuery optional and use it only for aggregate reporting, not booking creation or point lookups.
- Reuse one bounded Oracle `sql.DB` pool and one Pub/Sub publisher per warm function instance; pass request contexts and deadlines to calls. Start with 10 open connections, 5 idle connections, and a 5-minute connection lifetime. Bound cloud instance scaling against the hosted database connection budget.
- Keep the API demo usable when Pub/Sub is absent. The GCF event handler extends the completed API rather than being required for reset, seed, search, availability, booking, or lookup.

## Data Model

```text
Hotels(HotelId PK, Name, CreatedAt)
Rooms(RoomId PK, HotelId, RoomNumber, RoomType, Capacity)
Bookings(BookingId PK, Reference UNIQUE, HotelId, RoomId,
         GuestName, GuestCount, CheckInDate, CheckOutDate, CreatedAt)
OccupiedNights(RoomId, StayDate, BookingId, PK(RoomId, StayDate))
OutboxEvents(EventId PK, BookingId, Payload, CreatedAt, PublishedAt)
ProcessedEvents(EventId PK, BookingId, ProcessedAt)
```

Use application-generated UUIDs, UTC timestamps, and half-open stays `[check_in, check_out)`. Validate `check_in < check_out`, guest count, capacity, and a bounded stay length (for example, at most 30 nights). A night is occupied for each date from check-in through the day before check-out. Back-to-back stays are valid.

Preserve the .NET demo's rule that check-in is today or later. Make the current UTC date injectable for deterministic tests. Reuse its fixed hotel/room IDs. Generate references such as `HB-` followed by 32 uppercase hexadecimal characters: recognizable like the .NET demo, with the stronger random format from the Python demo.

`OccupiedNights` replaces the source projects' database-specific overlap techniques. A room/date primary key can belong to only one booking. Oracle transactions atomically insert the booking, occupied-night rows, and outbox event. The primary key is the final concurrency guard; an availability read alone cannot guarantee booking success. Normalize Oracle `DATE` stay values to midnight and store application timestamps as UTC `TIMESTAMP(6)`. Use `VARCHAR2(... CHAR)` for strings, `NUMBER(19,0)` for counts, and `CLOB` for outbox payloads. Do not copy SQL Server serializable range-lock code or PostgreSQL exclusion constraints.

## Booking Transaction And Event Flow

1. Validate request and select suitable rooms in stable order: capacity, room type, room number.
2. For each candidate, begin a short Oracle read-committed transaction, check the requested nights, and insert the booking, all `OccupiedNights` rows, and the outbox event. Insert nights in date order. If the room/date primary key raises a uniqueness conflict, roll back the entire candidate transaction before trying the next room; return `409 Conflict` when no candidate remains. Distinguish that constraint from unrelated unique violations. Use explicit bounded retries only for known retryable transaction failures, within the request deadline; do not assume automatic Spanner-style retries or retry an uncertain commit blindly.
3. Keep all external side effects outside the transaction. In particular, **never publish to Pub/Sub before the commit**; rolled-back candidates must produce neither bookings nor events.
4. After a confirmed commit, publish a `booking.created` message containing a stable `event_id`, booking reference, hotel ID, room ID, and creation time. Avoid guest name and other personal data in the event.
5. The worker records `event_id` in `ProcessedEvents` using a transaction. If it already exists, acknowledge the duplicate without repeating work.

The Oracle commit and Pub/Sub publish are **not one atomic transaction**. Create `OutboxEvents` with the booking schema and write an outbox row in the booking transaction from the start. When Pub/Sub is added, attempt immediate publication after commit and use an independently invoked publisher to retry pending rows. Use the same `event_id` for every attempt and make the consumer idempotent. Return booking success after a confirmed commit even when immediate publication fails; report delivery as pending separately.

## API Contract

```text
GET  /api/v1/hotels?name=...                 # optional partial name; list response
GET  /api/v1/hotels/{hotel_id}/rooms/available?check_in=...&check_out=...&guests=...&room_type=...
POST /api/v1/bookings
GET  /api/v1/bookings/{reference}
POST /api/v1/admin/seed
POST /api/v1/admin/reset
GET  /health/live
GET  /health/ready
GET  /openapi.yaml
GET  /docs
```

`POST /bookings` returns `201 Created`, a booking body, and `Location: /api/v1/bookings/{reference}` after the database commit. Include hotel name, room number/type/capacity, guest count, stay dates, and creation time in booking details, as the .NET demo does. Document the event delivery state separately from booking success. Seed first resets all demo data and then creates the fixed hotel and six rooms without bookings; reset removes bookings, occupied nights, outbox/processed-event rows, rooms, and hotels, returning `204`. Neither operation changes the schema. Return `400` for invalid input, `404` for unknown resources, `409` for unavailable rooms, and `503` for a database outage. Use a stable JSON error shape with `code`, `message`, and request ID. Serve the repository OpenAPI specification and a Swagger UI locally for manual testing.

The local demo has no application authentication, as in the challenge. Register admin routes only in explicit `local` or `demo` mode. A GCP demo deployment can enable those routes to run the full cycle through the API, with authenticated Cloud Run invocation controlling access to the whole service. Do not enable reset/seed routes in a production configuration.

## Code Structure

```text
hotel-booking-go/
  cmd/schema/               # one-shot local/demo Oracle application-schema setup
  cmd/local/                # local Functions Framework launcher
  cmd/eventbridge/          # local-only Pub/Sub push to CloudEvent adapter
  functions.go              # registers both deployable function entry points
  internal/httpapi/         # HTTP routes and response mapping
  internal/eventhandler/    # Pub/Sub CloudEvent handler
  internal/booking/         # booking rules and use cases
  internal/hotel/           # search and availability
  internal/testdata/        # API seed/reset use cases
  internal/store/           # Oracle queries, connection pool, and transactions
  internal/dbschema/        # ordered DDL and existing-object compatibility checks
  internal/events/          # event contract and Pub/Sub publisher
  schema/                   # versioned Oracle DDL
  api/openapi.yaml
  tests/
  infra/                   # deployment scripts or Terraform
  README.md
  go.mod
```

Deploy the two registered entry points separately from the same module root, as required by the Go Functions Framework source layout. Keep HTTP and CloudEvent handlers thin. Put domain decisions in services, Oracle access in the store, and event serialization/publishing behind a small interface. Avoid a generic repository framework for this demo.

## Local Development And Verification

- `infra/local/compose.yaml` starts Oracle Database Free and the Go HTTP function. Publish the API on a configurable host port and bind Oracle port 1521 to localhost. Supply `ORACLE_HOST`, `ORACLE_PORT`, `ORACLE_SERVICE`, `ORACLE_USER`, and `ORACLE_PASSWORD`; construct connection URLs with the driver helper and never log credentials. Keep database administrator credentials in the database container only. Local example passwords are fictional; use managed secrets for hosted deployments.
- Provide a one-shot Compose service for schema setup, after Oracle's database health check succeeds; start the API only after schema setup succeeds. Apply numbered SQL files in order. The schema runner supports additive `CREATE TABLE` and `CREATE UNIQUE INDEX` scripts, checks existing column types/lengths/nullability, primary keys, and unique indexes, and fails on incompatible definitions rather than altering data. Oracle DDL commits independently: do not promise migration rollback. A rerun resumes missing objects after partial setup. Require explicit local/demo mode for schema setup. Seed/reset use transactional `DELETE` and never change the schema or run automatically at API startup.
- The first vertical slice is `docker compose up --build`, schema setup, `POST /api/v1/admin/seed`, `GET /health/ready`, and partial-name hotel search. Complete availability, booking, lookup, and reset through HTTP before introducing Pub/Sub.
- Document one end-to-end script using future dates: reset, seed, search for `Grand`, list suitable rooms, book room 201, look up its reference, confirm room 201 is no longer available for those nights, then reset and confirm search returns an empty list.
- Add an `events` Compose profile containing the Pub/Sub emulator, a one-shot topic/push-subscription setup command, the Go function container with `FUNCTION_TARGET=BookingCreated`, and a small event bridge. Configure the emulator with `PUBSUB_EMULATOR_HOST=pubsub:8085` and create a push subscription pointing at the bridge on the Compose network. The bridge keeps the wrapped Pub/Sub message body, adds the CloudEvents headers Eventarc would provide, forwards to the function, and passes success/failure status back so failed handling can be retried. Test that translation with a representative Pub/Sub push fixture. Do not describe the emulator as an Eventarc emulator.
- Run database integration tests against real Oracle in a dedicated `hotel_booking_test` local schema, isolated from the demo data. Cover schema reruns and incompatible definitions, readiness before setup, seed replacement, the six-room dataset, case-insensitive literal substring search, ordering, reset, and rollback after injected insert/delete failures. Verify ordinary database restarts preserve data. Retain a small hosted concurrency smoke test in the later deployment phase.
- Test date boundaries, capacity, stable room selection, back-to-back stays, unknown resources, and event serialization with focused unit tests.
- Run an integration test where two concurrent requests try to book the last room for the same nights: exactly one succeeds; the other receives a conflict. Verify no partial `OccupiedNights` rows remain.
- Test a failed Pub/Sub publish after a committed booking, then successful outbox replay without duplicate worker effects.
- Run `go test ./...`, `go test -race ./...`, `go vet ./...`, and formatting checks. Keep the deployed smoke test small and repeatable.

## GCP Deployment And Cost Boundary

- Keep the functions and Pub/Sub in a dedicated GCP project and region. Provision a hosted Oracle database separately in the later deployment phase; hosting selection and provisioning are deferred from the local migration. Establish private network connectivity from Cloud Run functions and manage database credentials through Secret Manager. Separate schema-owner privileges from runtime DML privileges in hosted environments. Grant each function only the Pub/Sub and secret access it needs. Use authenticated invocation for a deployed demo API and disable seed/reset outside explicit local/demo mode.
- Local Oracle Database Free does not establish a free hosted Oracle deployment. Review database hosting, licensing, networking, and GCP service costs when selecting the later cloud environment; set a budget and document teardown before deployment.
- Publish through the application outbox and Pub/Sub. Database CDC and continuously running Dataflow pipelines are outside this demo.
- Keep schema creation, seed, deploy, smoke test, and teardown documented as separate commands. Never commit credentials, `.env` files, or Terraform state.

## Delivery Phases

1. **Containerized local foundation:** Go module, Functions Framework HTTP entry point, configuration, structured logging, health routes, Dockerfile, Compose with persistent Oracle Database Free, versioned Oracle schema including the outbox table, restart-safe schema setup command, API seed/reset endpoints, partial hotel search, and local run instructions. Exit when a clean Docker-only checkout can start the API, reach readiness, seed the six rooms, search for `Grand`, reset, and search an empty database.
2. **Complete API demo:** Availability endpoint, transactional booking and lookup, served OpenAPI and Swagger UI, end-to-end HTTP demo script, and focused integration tests. Exit when the full reset-to-booking-to-reset cycle works through the Compose API and room-night uniqueness holds.
3. **Pub/Sub and GCF extension:** Pub/Sub emulator, post-commit publisher, local push-to-CloudEvent bridge, separately containerized CloudEvent function entry point for GCF, stable event ID, idempotent processing, and failure tests. Exit when an API booking publishes through the emulator and invokes the actual function container, producing one auditable result even after duplicate delivery.
4. **Reliability:** Outbox retry publisher, explicit retry scheduling, and recovery demonstration after a failed publish. Exit when a pending event is eventually delivered without duplicate worker effects.
5. **GCP demo:** Select and provision a hosted Oracle endpoint, configure private connectivity and secrets for GCP functions, configure Eventarc retries and IAM, run smoke and final concurrency tests, document costs and teardown, and capture an architecture diagram plus short interview walkthrough.

## Interview Walkthrough

Be ready to explain how the complete API demo maps to the original .NET challenge; why booking correctness moved from SQL Server's serializable range locking to Oracle transactions and unique room-night keys; why Pub/Sub publishing follows the commit; how duplicate events are handled; what happens when publishing fails; and why the event handler is a separate GCF extension. State clearly that this is a personal Go adaptation and distinguish it from production Go work on your CV.

## Reference Documentation

- [Oracle transactions and implicit DDL commits](https://docs.oracle.com/en/database/oracle/oracle-database/18/tdddg/dml-and-transactions.html)
- [Go database/sql transaction guidance](https://go.dev/doc/database/execute-transactions)
- [go-ora driver](https://github.com/sijms/go-ora)
- [Oracle Database Free container workflow](https://github.com/gvenzl/oci-oracle-free)
- [Cloud Run functions in Go](https://docs.cloud.google.com/run/docs/write-functions)
- [Pub/Sub-triggered functions](https://docs.cloud.google.com/run/docs/function-triggers)
- [Cloud Run private networking](https://docs.cloud.google.com/run/docs/configuring/vpc-direct-vpc)
- [Cloud Run secrets](https://docs.cloud.google.com/run/docs/configuring/services/secrets)
