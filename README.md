# Go hotel booking demo

This is a Go and Oracle Database adaptation of the
[hotel booking challenge](https://github.com/sses79/hotel-booking/blob/dev/challenge.md).
Phase 1 provides a containerized local API, Oracle Database Free, schema setup,
and the seed/search/reset part of the demo. Availability, booking, Pub/Sub,
and the event function follow in later phases. Cloud Run functions and Pub/Sub
remain the planned GCP deployment architecture; hosted Oracle is deferred.

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
and stay dates will use midnight-normalized `DATE` values.

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
and inject insert/delete failures to prove transaction rollback. They require
`ORACLE_INTEGRATION=1`, `APP_ENV=local`, and `ORACLE_USER=hotel_booking_test`.
The suite skips database tests during ordinary `go test ./...`.

See [plan.md](plan.md) for the full API and event delivery phases.
