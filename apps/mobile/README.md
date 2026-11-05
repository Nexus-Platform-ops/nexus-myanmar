# Nexus Flutter client

Read ../../docs/PROJECT_BUILD_GUIDE.md and ../../docs/READINESS.md from the repository root.

Run flutter pub get, then flutter run -d chrome --web-port 3000 --dart-define=API_BASE=http://localhost:8080 for local debug use.

Release builds require an HTTPS API_BASE. Android emulator uses http://10.0.2.2:8080 in debug only. iOS/Android signed release builds and device tests have not been performed. This client is a developer baseline, not a store-ready release.
