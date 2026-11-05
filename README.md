# Nexus — tested developer project · v0.1.0

**အခြေအနေ: developer integration baseline ဖြစ်ပြီး production-certified release မဟုတ်သေးပါ။**
The uploaded Build Guide is the target specification, not evidence that this implementation has passed every production gate. `APP_ENV=production` deliberately refuses startup until the missing safety integrations and release gates are engineered and reviewed. Do not remove this gate just to deploy.

## Start here

1. Read `docs/PROJECT_BUILD_GUIDE.md` for exact setup and file placement.
2. Read `docs/READINESS.md` before connecting real users or real personal data.
3. Read `docs/SOURCE_TRACEABILITY.csv` for implemented / partial / absent scope.
4. Check `evidence/verification.json` and the raw verification logs.

## Included code

Flutter + Riverpod Android/iOS/web source; Go HTTP API; typed TypeScript GraphQL reader; PostgreSQL/PostGIS schemas; Redis rate limits/tickets; transactional reactions/swipes/Whisper deletion; safety controls; local Compose; CI workflow; automated regression tests; original specification documents. No marketplace, item trading, coins, checkout, or seller tooling is implemented. The original HTML is archived under docs/sources only, never served as the app.

## Local development

Requires Docker Compose v2, Go >=1.22, Node >=22 and Flutter matching `apps/mobile/pubspec.lock` (verified Flutter 3.47.9). Docker is an alternative to manually installing PostgreSQL 16+PostGIS and Redis 7.

```bash
cp .env.example .env
docker compose up --build -d
# In a second terminal:
cd apps/mobile
flutter pub get
flutter run -d chrome --web-port 3000 --dart-define=API_BASE=http://localhost:8080
```

Local OTP requests return `development_code` in development/test only. No real SMS is sent in that mode. Choose a synthetic adult DOB, social name and separate dating nickname. Radar is OFF by default. Enable it in the dating editor and approve a location fix to participate; insufficient local density returns the deliberate quiet state, never fake nearby users.

Release Flutter builds reject HTTP. Example compile-only check:

```bash
flutter build web --release --no-wasm-dry-run --dart-define=API_BASE=https://api.nexus.example
```

`api.nexus.example` is a non-deployable example host, not a provisioned service. For Android emulator development use `http://10.0.2.2:8080`; iOS simulator uses localhost. Use a real TLS staging domain for physical devices. Native platform projects exist, but native builds/signing/device tests were NOT executed here.

## Verification

```bash
bash scripts/verify.sh
# Isolated PostGIS database named nexus_test and Redis DB 15 must exist first:
bash scripts/integration.sh
```

Never run destructive integration fixtures against a non-test database. The script refuses any database name other than `nexus_test`. CI configuration is included but has not run on your GitHub account.

**No promise of zero defects, full feature parity, 300K DAU capacity, screenshot prevention, E2EE, or complete production readiness is made.**
