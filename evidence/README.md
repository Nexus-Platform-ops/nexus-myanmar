# Actual verification scope

Local source verification completed on 2026-11-05. See verification.json and raw logs.

- Go: 6 unit tests + 37 database integration scenarios passed with race detector.
- Go API statement coverage: 72.7%; aggregate including CLI entrypoints is lower.
- Flutter: 10 tests passed; measured line coverage 23.15%.
- Flutter analyzer: no issues; release JavaScript build succeeded. Wasm is NOT claimed.
- GraphQL: TypeScript build and 5 tests passed; HTTP gateway smoke also passed.
- Node production dependency audit reported zero known vulnerabilities in that scan only.
- Test screenshot is widget-rendered onboarding, NOT a real-device capture or all-screen UI parity proof.

Native/Docker/cloud/load/security/compliance release checks are NOT complete. >=80% module coverage requirement is NOT met. No public production approval is implied.
