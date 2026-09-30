# Go hotel booking demo

This is a Go and Spanner adaptation of the
[hotel booking challenge](https://github.com/sses79/hotel-booking/blob/dev/challenge.md).
Phase 1 provides a containerized local API,
Spanner emulator, schema setup, and the seed/search/reset part of the demo.
Availability, booking, Pub/Sub, and the event function follow in later phases.

## Requirements

- Docker Desktop with Docker Compose

Go and the Google Cloud CLI are not required on the host.

## Run locally

```bash
docker compose -f infra/local/compose.yaml up --wait -d
curl -i http://localhost:8080/health/ready
curl -i -X POST http://localhost:8080/api/v1/admin/seed
curl 'http://localhost:8080/api/v1/hotels?name=Grand'
curl -i -X POST http://localhost:8080/api/v1/admin/reset
curl 'http://localhost:8080/api/v1/hotels?name=Grand'
```

The hotel search returns an array. After seeding it contains Grand Plaza Hotel;
after resetting it is empty. Seeding resets all demo data, then inserts one hotel
with rooms 101, 102, 201, 202, 301, and 302. No bookings are seeded.

The Spanner emulator stores data in memory. If it restarts, recreate the whole
stack so the one-shot `schema` service runs again, then seed through the API:

```bash
docker compose -f infra/local/compose.yaml down
docker compose -f infra/local/compose.yaml up --wait -d
```

The API container uses the repository as a bind mount. After editing Go source,
run `docker compose -f infra/local/compose.yaml restart api` to recompile it.
After changing schema DDL, use the complete clean restart above.

API port can be changed with `API_PORT` in a local `.env` file; see
[`.env.example`](.env.example). The emulator ports bind to localhost for
debugging. Admin routes are registered only when `APP_ENV=local` or `demo`.
The schema command requires `SPANNER_EMULATOR_HOST` and cannot create cloud
resources.

## Checks

Run Go checks in the same container environment and module cache:

```bash
docker compose -f infra/local/compose.yaml run --rm --no-deps api go test ./...
docker compose -f infra/local/compose.yaml run --rm --no-deps api go vet ./...
docker compose -f infra/local/compose.yaml run --rm --no-deps api \
  sh -c 'apk add --no-cache gcc musl-dev >/dev/null && CGO_ENABLED=1 go test -race ./...'
```

See [plan.md](plan.md) for the full API and event delivery phases.
