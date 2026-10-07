#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
compose() { docker compose -f infra/local/compose.yaml -f infra/local/events.yaml --profile events "$@"; }
compose up -d --wait --wait-timeout 600
# The emulator stores topics/subscriptions in memory; recreate them on restart.
restore() { compose start pubsub; compose run --rm --no-deps pubsubsetup; compose start outbox; }
trap restore EXIT INT TERM
compose stop outbox pubsub
compose run --rm --no-deps -e HTTP_INTEGRATION=1 -e EVENTS_INTEGRATION=1 \
  -e RECOVERY_STAGE=outage api go test -count=1 -v ./tests -run '^TestPublishOutageBooking$'
restore
compose run --rm --no-deps -e HTTP_INTEGRATION=1 -e EVENTS_INTEGRATION=1 \
  -e RECOVERY_STAGE=recovered api go test -count=1 -v ./tests -run '^TestOutboxRecovery$'
trap - EXIT INT TERM
