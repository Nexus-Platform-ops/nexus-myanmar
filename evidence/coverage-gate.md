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
| Go `nexus/internal/api` statements | 72.7% | **83.63%** (1492/1784) | >=80% |
| Flutter `lib/` lines | 23.15% (320/1382) | **95.88%** (1325/1382) | >=80% |

The Go figure is measured with the CI toolchain (Go 1.25.5). Earlier local
measurement on Go 1.27.1 reported 80.5% (1512/1879): the newer toolchain
instruments 95 more statements in the same package, so the two percentages are
not comparable. CI measures 1784 statements, and that is the denominator this
gate uses.

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
- `internal/api/server_test.go` (coverage fix, commit `ba1108f`): deterministic
  sweeps for the malformed-body path on 28 mutating endpoints, the read-only
  gate on 16 write endpoints with a ghosted account, room/geo authorization
  edges (single-member room, ghosted/blocked/spoof-risk peer, location throttle,
  stale blip, duplicate and blocked chat members, undecodable WS ticket),
  profileless account behaviour, and unit tests for `wrap`, `appealable`,
  `hmacEqual`, `itoa` and `captureWriter`. The sweeps clear the shared per-IP
  limiter first (`fixture.resetIPLimit`) because every request originates from
  127.0.0.1 and the 300/min budget is otherwise inherited from earlier subtests.
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
GOTOOLCHAIN=go1.25.5 go test -count=1 -coverprofile=/tmp/api125.out ./internal/api
awk -v min=80 'NR>1 && $1 ~ /^nexus\/internal\/api\// { t += $2; if ($3 > 0) h += $2 }
  END { printf "%.2f%% (%d/%d)\n", 100*h/t, h, t }' /tmp/api125.out
                                                    # 83.63% (1492/1784)
GOTOOLCHAIN=go1.25.5 go vet ./...
$(GOTOOLCHAIN=go1.25.5 go env GOROOT)/bin/gofmt -l cmd internal   # no output

cd apps/mobile
flutter test --coverage
awk -F'[:,]' '/^DA:/{t++; if ($3 > 0) h++} END { ... }' coverage/lcov.info
                                                    # 95.88% (1325/1382)
```

## CI enforcement

`.github/workflows/verify.yml` now fails the backend lane when the
`nexus/internal/api` statement coverage is below 80%, and the mobile lane when
the lcov line coverage is below 80% (awk thresholds, printed to the log). The
threshold steps landed with the coverage tests (`78a2007`); `54bf140` changed
the backend gate to reuse the profile from the race step instead of re-running
the suite.

## CI history for this gate

1. Run 42198661290 (head of the push carrying `78a2007`, `75ca2ea`, `e9fe11f`)
   failed only at the gate step, which at that point re-ran the integration
   suite; the step reported `Process completed with exit code 1.`
2. Commit `54bf140` changed the gate to reuse the `coverage.out` produced by the
   race step. Run 42202118487 then failed with the explicit annotation
   `API coverage 79.54% < 80%`, exposing that the local 80.5% came from Go
   1.27.1 while CI pins Go 1.25.5 (`go.mod` has no `toolchain` line).
3. Commit `ba1108f` adds the coverage tests above. Run 42203777688 completed
   `success` for all three lanes on commit `ba1108f`; backend 54s, mobile 1m45s,
   graphql 8s.

The gate is fail-closed: zero matched API statements produce 0% and exit 1, so a
green gate step proves the awk pass, not an empty profile.

## Status

**CLOSED** 2026-11-06 with CI run 42203777688 on commit `ba1108f`, evidenced in
`docs/STAGE0_CLOSURE.md`. Local green alone was not treated as closure; the
preceding two failing runs are recorded above.

## Honest boundaries

- Coverage is not correctness. The Flutter suite drives widgets against a
  `FakeApi`, so it does not prove real HTTP, TLS, proxy or server behaviour; the
  existing onboarding image is widget-rendered, not a device capture.
- Go coverage is limited to `internal/api`; `cmd/*`, `internal/retention` and
  other packages are not part of the gate and their numbers are not claimed.
- CI step logs are sign-in gated for anonymous readers; the evidence available
  here is the run status, lane durations and the absence of gate annotations on
  a fail-closed check.
- No native Android/iOS build, device test, k6 load run, pentest or compliance
  evidence is produced here.
