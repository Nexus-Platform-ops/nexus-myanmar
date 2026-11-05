#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
test -z "$(gofmt -l cmd internal)"
go vet ./...
go test -race -count=1 ./...
go build ./cmd/...
python3 scripts/guardrails.py
(cd apps/mobile && flutter pub get && dart format --output=none --set-exit-if-changed lib test && flutter analyze --fatal-infos && flutter test --timeout 45s && flutter build web --release --no-wasm-dry-run --dart-define=API_BASE=https://api.nexus.example)
(cd services/bff && npm ci --no-fund && npm test)
