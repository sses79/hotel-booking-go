# Local verification

Verified on 2026-10-07 using Docker Desktop, the pinned Go 1.25.0 development
container, and real Oracle Database Free 23.26.3. No host Go installation or
cloud project is required.

| Check | Evidence |
| --- | --- |
| `go test ./...` | Unit suite passes; live suites skip unless explicitly enabled |
| `go test -race ./...` | Race suite passes |
| `go vet ./...` and `gofmt` | Pass |
| Dedicated Oracle integration suite | Schema reruns/incompatibility, fixed seed/search/reset, rollback injection, booking boundaries/order, concurrent room ownership, partial-night rollback, duplicate audits, replay, discarded-event redelivery after reset |
| `sh scripts/demo.sh` | Full HTTP cycle; room 201 selected then unavailable; last-room race returns one 201 and one 409 |
| `sh scripts/events.sh` | API → emulator → bridge → actual CloudEvent function; duplicate publication produces one audit row |
| `sh scripts/recovery.sh` | Pub/Sub stopped during booking: 201, three occupied nights, pending outbox; scheduled replay processes the event after restart |
| `sh scripts/persistence.sh` | Booking, occupied nights, and outbox survive an ordinary Oracle restart |
| Runtime image | Multi-stage build passes; both function targets, bridge, and retry process run as a non-root user with no source mount |

Reproduce the database suite:

```sh
docker compose -f infra/local/compose.yaml --profile test run --rm test
```

The integration suite drops/recreates objects only when both `APP_ENV=local`
and `ORACLE_USER=hotel_booking_test` are explicit. Live HTTP/event/persistence
scripts operate on the separate fictional demo schema and replace demo data.

Phase 5 cloud exit criteria were not run, as cloud deployment is outside the
requested scope. Pub/Sub emulator verification exercises the actual registered
function via HTTP CloudEvents, rather than substituting a mocked consumer.
