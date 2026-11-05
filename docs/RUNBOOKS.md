# Operations runbooks (not executed production drills)

## Privacy exposure: friend or coordinate leak
Disable Discover/Radar exposure immediately at the ingress/feature layer; if a reliable feature switch is not yet deployed, stop the service rather than returning cached people. Revoke exposed sessions, preserve metadata-only audit evidence, prevent raw PII from entering logs, assign incident commander, notify legal/safety, identify all affected read paths, patch the shared gate and run the full regression suite before reopening. No claim of automatic rollback exists in this baseline.

## Whisper retention failure
Stop accepting new Whisper messages, keep normal chat separately gated, investigate live-row deletion/tombstones, deny read access to expired content, inspect WAL/backup policy with security counsel. Do not promise forensic deletion from backups. Measure actual read-to-live-delete latency; tests on localhost are not a fleet SLO. Missing media object cleanup or evidence escrow blocks media Whisper launch.

## Dependency degradation
Redis unavailable: fail auth abuse controls/tickets closed; do not bypass OTP/probe limits. PostgreSQL unavailable: readiness 503; live health can remain alive. This baseline has no replicated store or cached-read failover. Source-proposed Redis-GEO/PostGIS fallback and AI-rule fallback need implementation before claiming graceful degradation.

## Moderation queue / high-risk accounts
Posts/comments/events fail closed under review. Trusted CLI decisions are intended only for synthetic/test content until named operator authentication, appeals and staffed queue routing exist. HIGH reports immediately hold exposure: add anti-brigading and authorized review tooling before live use. Never auto-clear restrictions on a timeout.

## Backups and disaster recovery
Production target is source RPO15min/RTO1h, but not demonstrated. Provision encrypted database storage/backups, private access and region controls. Rehearse restore to a new isolated stack, verify schema/session revocation/Whisper retention behavior, replay only safe content-free events, and collect timed recovery evidence. A local Compose volume is not a backup strategy.

## Release / rollback
Use immutable images and reviewed migrations; never roll a database schema backward by deleting tables. Canary target5%→25%→100% requires real SLO telemetry, traffic control and incident staffing. The included workflow tests source only, not live rollout. Retain the prior tested application image; rehearse rollback after a forward-compatible migration on staging.

## Secret provisioning
The .env.example values are deliberately local-only. Never reuse them outside local development. Supply independent JWT/OTP/data encryption secrets through a secret manager, rotate with a migration plan, enforce TLS, restrict DB roles, disable body/query/proxy logs and prevent bearer tokens from entering telemetry. Production mode is intentionally blocked until the missing integrations are implemented and independently reviewed.

## Database role topology (002_privileges)
`cmd/migrate` connects only with the bootstrap role (DDL + role administration). Application traffic runs as `nexus_app`; content decisions as `nexus_moderator`; retention as `nexus_janitor`; analytics as `nexus_readonly` (NOLOGIN until granted). All operational roles are created NOLOGIN and activated by migrate from `NEXUS_APP_DB_PASSWORD` / `NEXUS_MODERATOR_DB_PASSWORD` / `NEXUS_JANITOR_DB_PASSWORD` — migrate fails closed if any is unset, under 16 chars, or reused. Run `moderate` and `janitor` via the compose `ops` profile, which injects the scoped DSN per binary. Break-glass: the bootstrap superuser remains for migrations and incident DDL only; using it for application traffic bypasses the guardrails and must be logged as an incident action.

