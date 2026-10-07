#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
docker compose -f infra/local/compose.yaml run --rm --no-deps \
  -e HTTP_INTEGRATION=1 -e API_BASE_URL=http://api:8080 api \
  go test -count=1 -v ./tests -run '^TestHTTPDemo$'
