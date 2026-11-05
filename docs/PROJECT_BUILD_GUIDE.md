# Project build & developer handoff

## 1. File placement / ဖိုင်ထားရမည့်နေရာ

Extract the ZIP once into an empty workspace. It contains a single `nexus/` project root. Keep that root intact: do not move individual files into unrelated app directories. `PATHS.csv` lists **shipped source/artifact relative paths, byte sizes and SHA-256 hashes**; `SHA256SUMS` verifies their content. The two manifest files themselves are excluded from recursive self-hashing. No credentials, node_modules, Flutter SDK, database data, build caches or machine-specific local.properties are shipped.

| Exact relative path | Responsibility |
|---|---|
| `apps/mobile/lib/main.dart` | App theme, onboarding, phone OTP entry, navigation, auth gate |
| `apps/mobile/lib/api.dart` | Authenticated API, refresh rotation, raw-fix ingestion only |
| `apps/mobile/lib/ui.dart` | Shared components, error/empty states, safety report sheet |
| `apps/mobile/lib/discover.dart` | Dating cards, Look Around, anonymous synthetic radar rings |
| `apps/mobile/lib/community.dart` | Feed, optimistic reactions with rollback, post composer, comments, search, event/group listings |
| `apps/mobile/lib/chat.dart` | Authorized message history, realtime invalidation, explicit Whisper read action |
| `apps/mobile/lib/profile.dart` | Social/dating editors, privacy controls, safety, sessions, export/delete |
| `apps/mobile/assets/i18n/{en,my,zh}.json` | Matching localization resource keys |
| `apps/mobile/assets/fonts/` | Locally bundled Noto Sans Myanmar + OFL license |
| `apps/mobile/android/`, `apps/mobile/ios/`, `apps/mobile/web/` | Generated platform projects with targeted safety/config changes |
| `internal/api/{server,auth,geo,social,chat,safety}.go` | Logical backend module boundaries |
| `internal/api/server_test.go` | Real PostgreSQL/PostGIS + Redis regression tests; fixtures only in test DB |
| `cmd/{api,migrate,moderate,janitor}/main.go` | Server, schema apply, trusted review CLI, retention cleanup |
| `db/migrations/001_core.sql` | Versioned initial schemas and constraints |
| `services/bff/src/` | TypeScript GraphQL reader and contract tests |
| `packages/design-tokens/tokens.json` | Reference indigo palette + layout tokens |
| `packages/proto/privacy.proto` | Future extraction contract sketch; not deployed gRPC |
| `compose.yaml`, `infra/` | Local stack and reference ingress/security policies |
| `.github/workflows/verify.yml` | Proposed repository verification workflow |
| `e2e/` | Not-executed device/load harnesses (not proof of acceptance) |
| `docs/sources/` | Uploaded original files, preserved rather than silently rewritten |
| `evidence/` | Actual local test/build evidence; scope and coverage limitations |

## 2. Architectural decision

The two source documents describe a 20-sprint, multi-team platform. This handoff implements an **executable modular monolith with a separate GraphQL reader**, not eight pretend microservices. Go module boundaries are explicit; social, dating, private, safety and chat data live in separate database schemas. The local application role can still access them: independent roles/partitions, stronger least-privilege grants and analytics boundaries are release blockers.

The reader has no independent datastore and forwards the authenticated session to REST. Its GraphQL types do not expose social real names on a DatingCard. The mobile client currently uses REST; migrating read traffic to the BFF is not claimed complete. No Kafka, OpenSearch, ClickHouse, S3, MQTT or AI service is secretly running in this package.

## 3. Manual local setup (without Docker)

Install PostgreSQL 16, PostGIS and pgcrypto; install Redis 7. Create the development database/role. PostGIS extension installation may need a DBA. Run at the **nexus project root**:

```bash
cp .env.example .env
set -a
source .env
set +a
go mod download
go run ./cmd/migrate
go run ./cmd/api
```

The migration runner holds an advisory lock, applies schema version 1 transactionally, and is safe to re-run at that version. It does not auto-migrate at API startup. Database repair/backups remain DBA responsibilities; never edit an applied version in a deployed environment.

Optional GraphQL reader, separate terminal:

```bash
cd services/bff
npm ci
npm test
API_UPSTREAM=http://127.0.0.1:8080 PORT=8081 npm start
```

Reader binds loopback by default. Public deployment requires an authenticated TLS proxy, bounded concurrency, service network policy and centralized rate limiting. There are three typed read operations: discover, feed and social people search. Mutations remain REST. Browser origin defaults to `http://localhost:3000` and must match the app's actual origin exactly.

## 4. First usable account

Phone in E.164 format → request six-digit OTP → enter development code only in local mode → adult DOB → social name/township → Me/Dating editor → separate nickname, prompts, interests and privacy switches. Request location **only while using**. Radar opt-in expires server-side after 15 minutes; enabling it again is explicit. Keep nearby density honest; do not seed real-looking people into a live environment to hide an empty state.

Tier numbering is not self-service: clients cannot grant tier, phone verification, spoof score or restricted state. A successful phone OTP sets the separate phone_verified flag. Higher tiers require a future verified provider workflow. Event publication requires tier 3. Do not grant it using arbitrary production SQL.

## 5. Review and retention operations

New posts/comments/events remain UNDER_REVIEW, visible only to the author where applicable, until a trusted operator reviews them. There is no automatic 'approve everything' fallback.

```bash
# Uses a trusted operator's database credentials, never exposed through a public API.
go run ./cmd/moderate post RESOURCE_UUID LIVE
go run ./cmd/moderate comment RESOURCE_UUID REMOVED
# Run from a controlled scheduler after validating policy:
go run ./cmd/janitor
```

The CLI records decision metadata, but named operator identity, SSO/RBAC, appeals UI, case triage and SLA paging are NOT complete. HIGH reports hold target exposure pending human review; this is vulnerable to abusive false reporting without the missing safety operations controls. Janitor removes consumed/expired OTPs, expired sessions, old content-free outbox hints and old tombstones. Dedup metadata remains to prevent a wiped Whisper being resurrected by replay. Define a reviewed retention/legal-hold policy before live use.

## 6. Testing and reproducibility

`go vet`, `gofmt`, Go race tests, real DB integration, `flutter analyze`, Flutter widget tests, release JS compilation, TypeScript compilation and GraphQL tests were run. See exact outputs and measured coverage in evidence. Widget tests use a fake API **only inside tests**; backend integration tests exercise real local PostGIS + Redis. They do not prove Android/iOS capture behavior, k6 throughput, pentest resistance or production SLOs.

The integration test truncates identity.accounts with cascading fixtures in `nexus_test` and flushes Redis DB 15. Use a fully isolated test Redis instance in a shared environment, not merely a logical DB on production Redis. Provision the test DB and extensions before running `scripts/integration.sh`.

## 7. Build artifacts

This ZIP is a source handoff. It is not an APK, IPA, Play Store release, configured cloud tenant, signed binary, or hosted backend. Android release no longer signs using the generated debug key. Your release team must provision a proper signing configuration. macOS/Xcode, device attestation, release bundle IDs, assets, privacy manifests/labels, native device testing and store review remain outstanding.

All API contracts in `packages/contracts/openapi.json` describe the shipped baseline, not every aspirational endpoint from the original design. Full feed ranking, media, calls, subscriptions, follows, Meet Safe, AI moderation, pushes and microservice extraction need further implementation as listed in READINESS.
