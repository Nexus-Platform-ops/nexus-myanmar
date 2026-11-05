#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
# Must point only at an isolated test database, never the application database.
export TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://nexus:nexus_local_only@localhost:5432/nexus_test?sslmode=disable}"
DATABASE_URL="$TEST_DATABASE_URL" go run ./cmd/migrate
NEXUS_INTEGRATION=1 go test -race -count=1 -v ./internal/api
