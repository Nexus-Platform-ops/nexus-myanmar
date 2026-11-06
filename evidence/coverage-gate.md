# Coverage gate — Go and Flutter

Date: 2026-11-06. Scope: close the Stage 2 coverage gate recorded as READINESS
item 1 (Go API and Flutter both below the source DoD of >=80%).

## Metric definitions

- **Go** — statement coverage of package `nexus/internal/api`, the same metric
  that produced the historical 72.7% figure. It is not `-coverpkg=./...` and not
  an aggregate across `cmd/*` entrypoints; those remain unmeasured and unclaimed.
- **Flutter** — line coverage over `apps/mobile/lib` from the `lcov.info` produced
  by `flutter test --coverage`, i.e. the `DA:` records with a non-zero hit count.

## Before / after

| Surface | Before | After | Threshold |
|---|---|---|---|
| Go `nexus/internal/api` statements | 72.7% | **80.5%** (1512/1879) | >=80% |
| Flutter `lib/` lines | 23.15% (320/1382) | **95.88%** (1325/1382) | >=80% |

Per-file Flutter line coverage after the change:

| File | Lines |
|---|---|
| `lib/api.dart` | 70/76 (92.1%) |
| `lib/chat.dart` | 160/181 (88.4%) |
| `lib/community.dart` | 295/308 (95.8%) |
| `lib/discover.dart` | 176/186 (94.6%) |
| `lib/l10n.dart` | 12/12 (100%) |
| `lib/main.dart` | 176/180 (97.8%) |
| `lib/profile.dart` | 281/284 (98.9%) |
| `lib/ui.dart` | 155/155 (100%) |

## What changed

- `internal/api/server_test.go`: additional integration subtests and invalid-input
  matrices for safety reports/blocks/notifications, comments, chat history paging,
  CORS headers, staging SMS fail-closed behaviour, Meet-Safe limits, WebSocket
  ticket probes, search pagination (20+1) and moderation error paths; plus config
  unit tests. Dead `closeRows` helper and its `fmt` import removed from
  `internal/api/server.go`.
- `apps/mobile/test/api_test.dart` (new, 16 tests): `Api` request/response
  behaviour with `MockClient`, bearer and JSON headers, error-code fallback,
  401 → refresh → retry (including failed refresh), accept/restore/logout, the
  geolocator channel (disabled service, denied/denied-forever permission,
  low-accuracy fix, happy path) and client disposal.
- `apps/mobile/test/screens_test.dart` (new, 32 tests): a shared `FakeApi` and
  mount harness driving every screen — onboarding and sign-in, `RemoteView`
  error/retry/reload, Discover (radar, like/pass, location, block), Community
  (reactions, comments, events, groups, compose, search), Chat (list, whisper
  reveal/send, lifecycle, safety capture), Profile (editors, export, logout,
  Safety Center, Inbox, sessions, deletion).
- Test totals: 10 → 58 Flutter tests. `flutter analyze --fatal-infos` reports no
  issues; `dart format --set-exit-if-changed` is applied.

## Commands used

```
go test -count=1 -coverprofile=coverage-api.out ./internal/api
go tool cover -func=coverage-api.out | tail -1     # total: 80.5%

cd apps/mobile
flutter test --coverage
awk -F'[:,]' '/^DA:/{t++; if ($3 > 0) h++} END { ... }' coverage/lcov.info
                                                    # 95.88% (1325/1382)
```

## CI enforcement

`.github/workflows/verify.yml` now fails the backend lane when the
`nexus/internal/api` statement coverage is below 80%, and the mobile lane when
the lcov line coverage is below 80% (awk thresholds, printed to the log). The
same commit adds the Go tests and both Flutter test files.

## Status

Local measurement and the threshold steps are in place; the enforcement commit
has **not yet been pushed**, so this gate is not CI-verified and is **not
CLOSED**. Closure requires a green "Verify Nexus" run on the commit that carries
these changes (run ID to be recorded in `docs/STAGE0_CLOSURE.md` when it lands).
Local green is not closure.

## Honest boundaries

- Coverage is not correctness. The Flutter suite drives widgets against a
  `FakeApi`, so it does not prove real HTTP, TLS, proxy or server behaviour; the
  existing onboarding image is widget-rendered, not a device capture.
- Go coverage is limited to `internal/api`; `cmd/*`, `internal/retention` and
  other packages are not part of the gate and their numbers are not claimed.
- No native Android/iOS build, device test, k6 load run, pentest or compliance
  evidence is produced here.
