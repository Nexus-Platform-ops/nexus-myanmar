# Release readiness — explicit NO-GO

## Decision

**Not ready for public production.** The application and API baseline compile and the recorded tests pass. The uploaded Build Guide §10 requires substantially more than successful compilation. The server refuses `APP_ENV=production` by code, not an easily bypassed environment boolean. Do not describe this ZIP as zero-error or production-certified. Staging with real user data is also inappropriate until the applicable privacy/security controls are reviewed.

## Module status, compared with Build Guide §4 A–I

| Module | Implemented and locally exercised | Still incomplete |
|---|---|---|
| A Auth | Phone OTP contract, encrypted phone storage, attempt/cooldown limits, 18+ DOB, JWT expiry, refresh rotation, session ownership | Real SMS vendor test, account merge UX, recovery, external age verification, device attestation; full onboarding <90s device measurement |
| B Community | Text posts/review state, cursor feed, audience controls, reaction replace/rollback, two-level comments, bookmarks, share-action records | Media/reels/tags/stickers, true reshare-feed entry, COPY_LINK clipboard/deep link routing, SEND_DM delivery, ranking, top-3 reaction aggregation, comment likes/boosts, 24h highlights backend |
| C Search | Social people/posts/events SQL search; no dating identity field or nickname lookup | All tab, photos, cursor search, local recents UX, search index/ranking |
| D Events | Tier3 host write gate, review state, area text not exact pin, list/RSVP UI and API | Composer UI, venue polygon validation, Meet Safe/trusted contacts, delayed check-ins, invitations/social proof integration |
| E Nearby | PostGIS coarse geography, encrypted raw fix, expiry, symmetric friend/block/risk gates, k>=5 widening/quiet, scoped blips, approximate buckets, synthetic radial UI | Redis GEO hot index, S2/geohash, attested spoof model, actual radar push stream, k-anonymity proof for all band edge cases, 500-query adversarial trilateration harness, distance-cache audit |
| F Chats | Social-friend DMs (stricter than 3-message stranger allowance), match rooms, authorized REST, replay-safe client_id, normal history, transactional live-DB Whisper delete, metadata-only WS invalidation, capture-alert API, normal bodies encrypted at rest (005) | Voice/media, WebRTC/PiP/group calling, typing/read ticks, MQTT, Kafka partitioning, media purge/evidence escrow, offline ordered queue, guaranteed capture detection; bodies are encrypted at rest under a server-held key, **not E2EE** |
| G Notifications | Private in-app inbox, mark read, report/capture notices | Follow graph UX, FCM/APNs, safety response follow-ups, category mute settings |
| H Profiles | Separate social/dating schema and editors, nickname-only dating DTO, privacy switches, sessions, basic export/delete | Media cover/grid/reorder, literal preview mode UX, full DSR legal retention and cross-store erasure, independent database/analytics roles |
| I Dating-only | Card list and like/pass, reciprocal match rooms, basic Safety Center/ghost/block/report | Swipe gesture stack, haptic match modal, verified Tier1–3 flows, emergency/trusted contacts, Nexus+ billing/receipts/entitlements, who-liked-me/boost/history, AI icebreakers |

Nexus+ is allowed by the product spec but NOT implemented. Marketplace, coins, seller tooling and item trading remain out of scope. Archived original HTML is documentation only.

## Release-blocking engineering gaps

1. **Coverage gate not met.** Measured Go API coverage is recorded in evidence, below source DoD >=80%; CLI coverage is not claimed. Mobile widget coverage is also below 80%. Passing tests do not waive this gate.
2. **Native delivery unverified.** No Android SDK build, iOS Xcode build, store signing, real phone, 2GB/Android8 profiling, keyboard/device shaping, capture test matrix, AA audit or dynamic-type audit was performed. Screenshots in evidence come from a Flutter widget test, not a real phone.
3. **Security certification absent.** No independent pentest, 500-query trilateration evaluation, red team, bot/scam validation, key rotation, cert pinning, WAF/CDN policy, authenticated device attestation or KMS integration. Shared passphrase pgcrypto is not KMS envelope encryption.
4. **Data isolation partially enforced (2026-11-05 P0-1).** Migration `002_privileges.sql` moves ownership to a NOLOGIN `nexus_owner`, and the bootstrap role no longer serves application traffic: `nexus_app` holds DML-only grants (no DDL, no TRUNCATE, no role creation), `nexus_moderator` can only update social content status and write `safety.audit`, `nexus_janitor` can only delete retention rows, and `nexus_readonly` sees aggregate-safe columns only. Operational-role boundaries are regression-tested in `TestPrivilegeBoundaries` (CI integration lane). Remaining gap: the API is a monolith whose cross-schema joins force broad DML on `nexus_app`, so social↔dating separation inside the app tier is still enforced at the query layer plus privacy tests, not physically per-domain. Per-domain app roles are deferred to service extraction. Dating embeddings, analytics isolation, and geographic residency controls still need infrastructure/verification.
5. **Exact-location write exception.** Raw GPS necessarily exists on the collecting device and in the authenticated POST ingress. The 'no exact coords client-side' source wording is interpreted as no exact coordinates in **responses about people**, maps, logs, caches or analytics. Encrypted raw fixes and coarse PostGIS points still need an independently reviewed retention/access policy. Coarse points themselves are sensitive data.
6. **Whisper scope is narrow and honest.** Per-recipient semantics (migration 003): a read marks the message consumed for that reader via `chat.whisper_reads`; the shared row is physically deleted only after every non-sender room member has read it, so one reader can no longer wipe a group room for others (regression-tested in `group_whisper_per_recipient`). Normal (NORMAL-mode) bodies are now encrypted at rest with pgcrypto under the shared `DATA_KEY` (migration 005, P0-3), but this is server-side encryption, **not E2EE**: the API decrypts to serve history and export, there is no key-version column, and rotation does not re-encrypt historical rows. WAL, PITR backups, DB admin snapshots, OS keyboards/caches, screenshots and other cameras are NOT proven wiped. Evidence capture-before-wipe and media destruction are absent.
7. **Capture signals are best effort.** Android FLAG_SECURE applies globally; it can prevent ordinary capture but cannot detect every attempt, compromised device, or second camera. iOS screenshot notification cannot undo a screenshot. Native bridging source is uncompiled here. Never display a promise of guaranteed protection.
8. **Operations not provisioned.** No active observability dashboards/page routing, moderation staffing, safety SLA dispatch, multi-AZ DB/Redis, Kafka, S3, DR restore rehearsal, real PITR, Argo canary execution or 48h soak. Local HTTP/Compose are NOT a secure public deployment.
9. **Abuse handling needs human controls.** HIGH report holds can be abused; provider-based risk scores, authorized named reviewer workflow, appeal and audit retention policy are required. Current report details are text only; no protected media escrow.
10. **Capacity claims unverified.** No 15K WebSocket/300K DAU exercise, 2x city peak, power/network loss, P95 dashboard or monthly error-budget validation. k6 script is a staging harness only. WS currently sends periodic invalidation hints, not ordered message payloads or radar fanout.
11. **API completeness.** Exactly-once create-post/share/report semantics, durable outbox consumers, replay ordering across disconnects and centralized multi-instance auth/abuse limits need further work. Reactions, swipes and messages have their specific idempotent protections; a general Idempotency-Key system is not implemented.
12. **Compliance/business gates.** Data map, deletion/backup policy, parental/age policy, store labels, local law/PDPA/residency, hosting contracts, terms/privacy policy, subscriptions and operational emergency escalation are not supplied or approved.

## Source ambiguities resolved conservatively

- Foundation §8 calls phone OTP 'Tier0', whereas §3.6/Build Guide §6 calls Tier0 unverified/read-only. Use `phone_verified` as an independent hard gate; tiers 0–3 describe additional verification. Local OTP verification does not self-award selfie/ID tiers.
- Foundation suggests 50m/100m bands; Build Guide adopts 500m/1/3/5/10km. Baseline accepts 500m and above only, with k>=5; no fine-grained precise pins.
- Friend exclusion applies symmetrically to **all dating read and match access paths**. Normal social friends can still see authorized social posts/comments. Hiding an identity on Dating does not sever the social relationship; product owners should confirm this distinction.
- Social unsolicited-DM policy in the source permits three messages; this baseline disallows non-friend social DMs entirely. This is a stricter temporary safety restriction, not full feature parity.
- Event venue is an area label only here. Area text cannot itself guarantee it contains no address; moderation and schema-constrained area identifiers are still necessary.

## Go-live sign-off table (every entry currently unapproved)

| Owner | Required evidence |
|---|---|
| Mobile | Signed native builds, full screen/device/a11y/Myanmar matrix, capture disclosures |
| Platform | Attestation, KMS, least privilege, TLS deployment, perf/DR/soak results |
| Dating | All recall-path/race/cache privacy tests, adversarial location audit |
| Social | Media/review/follow/share/ranking and full UX DoD |
| Safety Ops | Named review tools, abuse handling, staffed HIGH <=30min response |
| QA/Release | >=80% module coverage, Maestro end-to-end, rollback/Canary rehearsal |
| Legal/Store | Local policies, labels, age/verification, DSR and store approval |

The original S1–S20 plan remains the correct completion plan. This handoff is an implementation starting point plus measured verification, not a claim that all twenty sprints occurred.
