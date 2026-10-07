#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
compose() { docker compose -f infra/local/compose.yaml "$@"; }
compose run --rm --no-deps -e HTTP_INTEGRATION=1 -e PERSISTENCE_STAGE=before api \
  go test -count=1 -v ./tests -run '^TestPersistenceBeforeRestart$'
compose restart oracle
compose up -d --wait --wait-timeout 600 oracle api
compose run --rm --no-deps -e HTTP_INTEGRATION=1 -e EVENTS_INTEGRATION=1 \
  -e PERSISTENCE_STAGE=after api go test -count=1 -v ./tests -run '^TestPersistenceAfterRestart$'
