# Go hotel booking demo

This is a Go and Oracle Database adaptation of the
[hotel booking challenge](https://github.com/sses79/hotel-booking/blob/dev/challenge.md).
Phases 1–4 provide the complete local booking API, Oracle Database Free,
a Pub/Sub emulator, a separate CloudEvent function, and scheduled outbox recovery.
Phase 5 (hosted Oracle and GCP deployment) is deferred by request.

## Requirements

- Docker Desktop with Docker Compose, on ARM64 or AMD64
- Enough Docker memory/disk for a real Oracle database; the first image download
  and database initialization can take several minutes

Go, Oracle Instant Client, and the Google Cloud CLI are not required on the host.
The app uses the pure-Go `go-ora/v2` driver through `database/sql`.

## Run locally

```bash
docker compose -f infra/local/compose.yaml up --wait --wait-timeout 600 -d
curl -i http://localhost:8080/health/ready
curl -i -X POST http://localhost:8080/api/v1/admin/seed
curl 'http://localhost:8080/api/v1/hotels?name=Grand'
curl -i -X POST http://localhost:8080/api/v1/admin/reset
curl 'http://localhost:8080/api/v1/hotels?name=Grand'
```

The hotel search returns an array. After seeding it contains Grand Plaza Hotel;
after resetting it is empty. Seeding replaces all demo data, then inserts one
hotel with rooms 101, 102, 201, 202, 301, and 302. No bookings are seeded.
Seed and reset each run in one transaction and roll back on failure.

Compose waits for Oracle's health check, runs the one-shot schema command, and
then starts the API. Oracle uses a persistent `oracle-data` named volume:
ordinary restarts and `docker compose down` preserve the schema and data.
API startup never seeds or resets data. Readiness checks all application tables;
liveness does not require database access.

The API container uses the repository as a bind mount. After editing Go source,
run `docker compose -f infra/local/compose.yaml restart api` to recompile it.
Rerun schema setup explicitly after adding supported DDL:

```bash
docker compose -f infra/local/compose.yaml run --rm schema
```

The runner applies numbered SQL files in order and supports `CREATE TABLE` and
`CREATE UNIQUE INDEX` with the definitions used in this demo. It validates
existing columns, primary keys, and unique indexes, creates missing objects,
and rejects incompatible definitions. It does not alter existing tables.
Oracle DDL commits independently, so a failure can leave created objects;
rerunning resumes setup after correcting the cause. Incompatible schema changes
currently require an explicit local rebuild or a separately authored migration.

To remove **all local database data** and rebuild from scratch:

```bash
docker compose -f infra/local/compose.yaml down -v
docker compose -f infra/local/compose.yaml up --wait --wait-timeout 600 -d
```

## Configuration

Defaults in Compose make the demo runnable without a `.env` file. To override
them, copy [`.env.example`](.env.example) to `.env` and pass `--env-file .env`
to the Compose commands before the subcommand. Change `API_PORT` if needed.
Oracle port 1521 and the API port bind to localhost only.

The app reads `ORACLE_HOST`, `ORACLE_PORT`, `ORACLE_SERVICE`, `ORACLE_USER`, and
`ORACLE_PASSWORD`. Inside Compose, the database host is `oracle`, port `1521`,
and service `FREEPDB1`; use `localhost` for host-side tools. The dedicated demo
user is `hotel_booking`. `ORACLE_ADMIN_PASSWORD` configures only the database
container, and is never passed to the API. All supplied passwords are local demo
values. Container user/password initialization runs only on the first startup
of an empty volume; changing `.env` later does not change existing database users.
Do not commit real credentials; use managed secrets for hosted deployments.

Admin API routes are registered only when `APP_ENV=local` or `demo`. The schema
command also requires local/demo mode and creates objects only in the connected
user's schema; it does not provision a database or cloud resources. Hosted Oracle
networking, credentials, and provisioning belong to the later GCP phase.

Timestamps use UTC `TIMESTAMP(6)`, string lengths use Oracle character semantics,
and stay dates use midnight-normalized `DATE` values.

## Checks

Run unit tests and vet in the Go development container without requiring Oracle:

```bash
docker compose -f infra/local/compose.yaml run --rm --no-deps api go test ./...
docker compose -f infra/local/compose.yaml run --rm --no-deps api go vet ./...
docker compose -f infra/local/compose.yaml run --rm --no-deps api \
  sh -c 'apk add --no-cache gcc musl-dev >/dev/null && CGO_ENABLED=1 go test -race ./...'
docker compose -f infra/local/compose.yaml run --rm --no-deps api \
  sh -c 'test -z "$(gofmt -l .)"'
docker build -t hotel-booking-go:local .
```

The Oracle container creates a separate `hotel_booking_test` user for integration
tests on first initialization. Run the integration suite with:

```bash
docker compose -f infra/local/compose.yaml --profile test run --rm test
```

These tests drop/recreate tables **only in the local test schema**, exercise
schema reruns and incompatibility checks, search and the fixed six-room seed,
and inject insert/delete failures to prove transaction rollback. Booking tests
also cover stable selection, back-to-back stays, late outbox failure, concurrent
room ownership, partial-night rollback, duplicate processing, and replay. They require
`ORACLE_INTEGRATION=1`, `APP_ENV=local`, and `ORACLE_USER=hotel_booking_test`.
The suite skips database tests during ordinary `go test ./...`.

## Full API demo

Open [Swagger UI](http://localhost:8080/docs) to seed, search, check availability,
book, look up a reference, and reset. The OpenAPI specification is served at
`/openapi.yaml`. Swagger UI 5.29.0 assets and its license are bundled in the
binary so documentation needs no external CDN.

Run the full HTTP cycle, including the last-room concurrency check, with Docker
only (the script calculates future dates):

```bash
sh scripts/demo.sh
```

It resets, seeds, searches for `Grand`, finds rooms for two guests, books room
201, looks up the returned reference, and confirms that room is unavailable.
It also races two HTTP bookings for the last deluxe room and requires exactly
one `201` and one `409`, then resets and confirms hotel search is empty.
These scripts replace the fictional local demo data; run them sequentially.

Booking requests use this JSON shape (replace dates with future UTC dates):

```json
{
  "hotel_id": "00000000-0000-0000-0000-000000000001",
  "check_in": "2030-01-01",
  "check_out": "2030-01-04",
  "guests": 2,
  "guest_name": "Fictional Guest",
  "room_type": "double"
}
```

`room_type` is optional and accepts `single`, `double`, or `deluxe`. Stays last
1–30 nights, start today or later in UTC, and exclude the checkout night.
Rooms are selected by capacity, type, and room number. A booking response
contains hotel/room details, a random `HB-` plus 32 uppercase hex reference,
and a `Location` lookup URL. Invalid input returns `400`, missing resources
`404`, exhausted availability `409`, and database failures `503` with JSON
`code`, `message`, and `request_id`.

Each candidate transaction inserts the booking, all occupied nights in date
order, and a stable outbox event. Oracle's unique room/date primary key decides
concurrent ownership. A collision rolls back the candidate before trying the
next suitable room; unrelated unique violations propagate as database failures.
An uncertain commit is never retried automatically.

## Local Pub/Sub and CloudEvent function

Start the local events profile with its API overlay:

```bash
docker compose -f infra/local/compose.yaml -f infra/local/events.yaml \
  --profile events up -d --wait --wait-timeout 600
sh scripts/events.sh
```

The overlay enables API publishing to `local-hotel/booking-created` in the
emulator. The base Compose API remains usable with no Pub/Sub service.
The profile runs `BookingCreated` through the actual Go Functions Framework
in a separate worker container. A local bridge preserves the wrapped Pub/Sub
push body, adds CloudEvents headers, and forwards function success/failure
back to the emulator. This is a push adapter, not an Eventarc emulator.
[Cloud Run event-driven functions use Eventarc](https://docs.cloud.google.com/run/docs/function-triggers)
in the deferred cloud deployment.

The setup command creates the topic and push subscription idempotently.
The emulator has in-memory state; recreate its topic/subscription after restart:

```bash
docker compose -f infra/local/compose.yaml --profile events \
  run --rm --no-deps pubsubsetup
```

`event_delivery` is `pending` or `published`. A confirmed Oracle commit always
returns `201`, even when Pub/Sub is offline. `published` means Pub/Sub accepted
the message; processing is audited separately in `ProcessedEvents`.
Events contain stable IDs, the booking reference, hotel/room IDs, and creation
time. They exclude the guest name. The worker inserts the event ID transactionally;
redelivery, including concurrent redelivery, produces one audit row.

## Outbox recovery

The independent `outbox` service retries up to 100 pending rows every five
seconds (`OUTBOX_INTERVAL`, minimum `1s`). Each scan has a 30-second deadline.
There is no request-owned background goroutine: delivery continues without API
traffic. Rows are marked published only after Pub/Sub acceptance. A crash or
failed mark can cause another publish with the same event ID; the worker's
primary key prevents duplicate effects. Failed rows stay pending.

Run the real outage/recovery demonstration:

```bash
sh scripts/recovery.sh
```

This stops Pub/Sub and the retry publisher, verifies a booking still commits
with three occupied nights and a pending event, restarts the emulator, recreates
its topic/subscription, and restarts the publisher. It waits for delivery through
the function and checks duplicate publication leaves one audit row. A shell trap
restores the stopped services on failure.

Run a single replay batch manually:

```bash
docker compose -f infra/local/compose.yaml --profile events \
  run --rm --no-deps -e OUTBOX_ONCE=1 outbox
```

To return to the API-only stack without deleting Oracle data:

```bash
docker compose -f infra/local/compose.yaml --profile events \
  stop outbox pubsub eventbridge worker
docker compose -f infra/local/compose.yaml up -d api
```

## Persistence and runtime image

Verify a booking, occupied nights, and its outbox row survive an ordinary Oracle
restart:

```bash
sh scripts/persistence.sh
```

The multi-stage runtime image contains `hotel-api` (both function targets),
`hotel-schema`, `hotel-outbox`, and `hotel-eventbridge`, and runs as a non-root
user. To run the API/worker from that image instead of the development bind mount:

```bash
docker build -t hotel-booking-go:local .
docker compose -f infra/local/compose.yaml -f infra/local/events.yaml \
  -f infra/local/runtime.yaml --profile events up -d --wait --wait-timeout 600
```

See [docs/verification.md](docs/verification.md) for executed checks,
[docs/architecture.md](docs/architecture.md) for the architecture/interview
walkthrough, and [plan.md](plan.md) for phase status.
