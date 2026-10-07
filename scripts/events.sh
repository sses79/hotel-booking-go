#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
docker compose -f infra/local/compose.yaml -f infra/local/events.yaml --profile events \
  up -d --wait --wait-timeout 600
docker compose -f infra/local/compose.yaml -f infra/local/events.yaml --profile events \
  run --rm --no-deps -e HTTP_INTEGRATION=1 -e EVENTS_INTEGRATION=1 api \
  go test -count=1 -v ./tests -run '^TestLocalEvents$'
