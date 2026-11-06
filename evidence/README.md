# Actual verification scope

Local source verification completed on 2026-11-05. See verification.json and raw logs.

- Go: 6 unit tests + 37 database integration scenarios passed with race detector.
- Go API statement coverage: 80.5% (`nexus/internal/api`); aggregate including CLI entrypoints is lower and not claimed.
- Flutter: 58 tests passed; measured line coverage 95.88%. A CI threshold step enforces >=80% for both; the enforcement commit is not yet CI-verified (see `coverage-gate.md`).
- Flutter analyzer: no issues; release JavaScript build succeeded. Wasm is NOT claimed.
- GraphQL: TypeScript build and 5 tests passed; HTTP gateway smoke also passed.
- Node production dependency audit reported zero known vulnerabilities in that scan only.
- Test screenshot is widget-rendered onboarding, NOT a real-device capture or all-screen UI parity proof.

Native/Docker/cloud/load/security/compliance release checks are NOT complete. Coverage thresholds (>=80%) are enforced in CI as of the unverified enforcement commit; local measurement is 80.5% Go / 95.88% Flutter. No public production approval is implied.
