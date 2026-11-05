SHELL := /bin/bash
.PHONY: test api migrate mobile verify integration
api:
	go run ./cmd/api
migrate:
	go run ./cmd/migrate
test:
	go test -race -count=1 ./...
mobile:
	cd apps/mobile && flutter run --dart-define=API_BASE=http://localhost:8080
verify:
	bash scripts/verify.sh
integration:
	NEXUS_INTEGRATION=1 go test -race -count=1 ./internal/api -run Integration -v
