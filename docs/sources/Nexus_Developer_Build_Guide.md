# Nexus — Developer Build Guide (Production-Ready Implementation လမ်းညွှန်)

> **ဤစာတမ်း**: "Nexus Platform Design" document (§ အမှတ်များ ဤ guide ထဲ ရည်ညွှန်း) ကို confirmed foundation အဖြစ် ယူပြီး，Reference UI Kit (social app template screenshots ၁၃ ု + `myanmar_superapp_unified_ecosystem.html` mockup) ကို visual reference အဖြစ် အသုံးပြုကာ，Developer team က Phase 0 မှ production launch အထိ တည်ဆောက်နိုင်ရန် screen-by-screen spec, design system, engineering process, QA gates အားလုံး ပါဝင်သော build playbook ဖြစ်သည်။
> **Scope guard**: Local Marketplace / Nexus Coins / commerce flows များသည် reference mockup ထဲ ပါဝင်သော်လည်း **out of scope** (§11) — ပြန်မထည့်ရ။

---

## 0. ဖတ်ရှုရမည့် အစဉ်

1. §1 Reference inventory → ဘာကို adopt/adapt/reject လုပ်မည်
2. §2 Design System → tokens, components
3. §3 Client architecture + navigation mapping
4. §4 Module-by-module screen specs (အဓိက အပိုင်း)
5. §5 Cross-cutting implementation rules
6. §6 Safety guardrails in code
7. §7 Engineering process (squads, sprints, CI/CD, DoD)
8. §8 Production readiness + launch checklist
9. §9 QA acceptance matrix

---

## 1. Reference UI Kit Inventory & Disposition

Reference template (wordmark "Suitify") မှ screen ၃၂ ခုကို Nexus နှင့် mapping။ Brand အားလုံး **Nexus** ဖြင့် အစားထိုးရမည် (§15 design doc)။

| # | Reference Screen | Disposition | Nexus ပြောင်းလဲချက် အဓိက |
| --- | --- | --- | --- |
| 1 | Splash (logo) | Adopt | Nexus wordmark + safety-first tagline |
| 2–4 | Onboarding ×3 ("Find Friends…" / "Meet Awesome People…" / "Attend Events…") | Adopt, re-copy | Slide 1=Dating+Radar, 2=City Community, 3=Events & Meet Safe; Burmese copy |
| 5 | Sign In (email+password) | **Adapt** | Phone-OTP ကို primary; email သည် recovery option သာ (design doc §12 Phase 0) |
| 6 | Sign Up (name/email/pw) | **Adapt** | Real name → `user_profiles` သာ; dating nickname ကို §4.9 တွင် သီးသန့်ယူ |
| 7 | Enter Phone Number (+country) | Adopt | +95/+66 default detection per city launch |
| 8 | OTP Code | Adopt | 4-digit box design OK; anti-abuse rate-limit client copy |
| 9 | Forgot Password | Adopt | Email/phone dual recovery |
| 10 | Change Password | Adopt | — |
| 11 | Home Feed (stories row + post cards + bookmark) | **Adapt** | Stories row → **Township Highlights**; post card = design doc §4.2 layout (reaction stats bar + hover/long-press reactions) |
| 12 | Create Post (audience/Gallery/Attachment/Location/Add someone) | Adopt | Location = township label သာ (§2.4); moderation pre-hook (§4.1) |
| 13 | Post Comments (nested, "Show 14 more replies") | Adopt | = §4.4 thread hybrid အတိုင်း |
| 14 | Notifications — Follow Request | Adopt | Social graph သာ; dating matches မပါ |
| 15 | Notifications — Activity | Adopt | + Safety notices category (§4.7) |
| 16 | Messages — Chats list (+create room, online avatars row) | **Adapt** | Online avatar row = Radar opt-in matches; whisper badge on threads |
| 17 | Messages — Calls list (missed/in/out) | Adopt | — |
| 18 | Chat thread (voice, typing, read ✓✓) | **Adapt** | + **Whisper mode toggle & self-destruct chip** (§4.6), screenshot alert (§8) |
| 19 | Search Conversation (recent) | Adopt | — |
| 20 | Global Search tabs All/People/Post/Photos/Events | **Adapt** | People tab = **social profiles သာ** — dating profiles ရှာဖွေခွင့် မရှိ (§6 guardrail) |
| 21–24 | Search results (People/Post/Photos/Events) | Adopt | Events distance = bucket + km (§2.4), not "mil" |
| 25 | Events feed (Attend / Not going / share) | **Adapt** | Township filter chip (Kamayut/Bahan…); host verification badge Tier3 |
| 26 | Event detail (hero/map/attendees) | **Adapt** | Map = venue area သာ; delayed check-in publish (§2.4); Meet Safe entry (§8) |
| 27 | **People Near You — Maps** (radius rings, avatar pins) | **Adapt heavily** | = Look Around + Radar (§2.5): avatar pins → **anonymous blips**, exact pins → jittered, radius rings → privacy buckets |
| 28 | People Near You — Profile tab | **Adapt** | = Look Around list view (dating cards, nickname+age+badges+distance bucket) |
| 29 | Nearby Filter (Show me on map / Range slider / ordering) | **Adapt** | "Show me on map" = `radar_opt_in`; Range in km buckets; k-anonymity note (§2.5) |
| 30 | Create Chat (multi-select) | Adopt | Group DM; friend-exclusion n/a (social side) |
| 31 | Video Call full-screen (PiP, controls) | **Adapt** | + in-call Report & Safety button; whisper rooms media-purge rule |
| 32 | Profile (cover/stats/photos/videos/edit) | **Adapt** | + **Social ↔ Dating profile segmented switch** (§4.9) |

**Nexus-only screens (reference မပါ — design doc အရ တည်ဆောက်ရမည်)**: Discover swipe card stack (§5.3), Radar sweep animation, Dating Profile editor (§3.2 ASCII layout အတိုင်း), Verification flow (Tier0–3), Safety Center, Match chat icebreaker prompts, City Board / Interest Groups, Nexus+ paywall, Whisper settings。

---

## 2. Design System (Tokens & Components)

### 2.1 Color / Type / Layout tokens

```text
color.primary      #4C5FD5 (reference indigo)   color.onPrimary   #FFFFFF
color.ink          #1E2A56 (headings)           color.text        #3A4468
color.bg           #F5F7FB                      color.surface     #FFFFFF
color.accent       #35C4E8 (map/radar rings)    color.danger      #E5484D
color.verified     #2E9E5B / #1E874B / #0F6B3A (Tier1/2/3 badge intensity)
radius.card 16 · radius.chip 999 · nav.height 64 · fab 56 circular
spacing scale 4/8/12/16/24/32 · elevation: card 2dp, nav 8dp, sheet 16dp
```

- **Typography**: Latin = Inter/SF fallback; **Myanmar = Noto Sans Myanmar (Android) / Myanmar Sangam MN (iOS)**; body 15/line 1.6, stacked-consonant line-height regression test ပါရမည် (§9)。
- Dark mode: Phase 2 target; token-based flip only。
- Motion: 200–300ms ease-out; radar sweep 2.5s GPU loop; reduced-motion setting honored。

### 2.2 Shared components (design tokens → code)

`PostCard` (§4.2 layout), `ReactionBar` (optimistic, top-3 emoji stack), `Avatar` (+online dot, tier badge slot), `ProfileCard` (dating: nickname/age/badges/distance-bucket), `Blip` (map anonymous dot), `TownshipChip`, `DistanceBucketLabel`, `ToggleRow` (privacy switches), `RangeSlider` (km buckets), `SafetySheet` (long-press any user → block/report/menu ≤3 taps), `EmptyState` (Radar-quiet → community nudge §5.3)。

---

## 3. Client Architecture & Navigation

### 3.1 Stack

Flutter monorepo app + native plugins (CoreLocation/FusedLocation, camera verification, FLAG_SECURE/App Attest, WebRTC, FCM/APNs)。State: **Riverpod** (auth/session, feed cache, radar socket, chat rooms)。Offline queue for swipes/reactions (§6.4 design doc)。BFF = GraphQL gateway; REST for mutations per §6.2 contracts。

### 3.2 Navigation mapping (reference nav → Nexus IA)

Reference bottom nav = home · search · (+FAB) · location · profile။ Nexus final IA (design doc §5.2) နှင့် ပေါင်းစပ်:

```text
Bottom: [Discover] [Community] [ (+) Create ] [Chats] [Me]
  · Community home = reference feed layout (stories row → Township Highlights; top-right bell + send → Notifications / Chats quick entry)
  · Global Search = Community tab top search bar (reference tab design adopted)
  · People Near You / Look Around / Radar = Discover အတွင်း mode toggle (reference map screens adopted there)
  · (+) FAB = Create Post / Create Event / Create Group (audience selector ပါ)
Rationale: map က home မဟုတ် (design doc §5.1 principle 1) — reference ရဲ့ location-pin tab ကို Discover mode အဖြစ် downgrade။
```

---

## 4. Module-by-module Build Specs

Each screen: **Reference → Nexus spec → API → State/edge → DoD**。

### Module A — Onboarding & Auth (ref 1–10)

| Item | Spec |
| --- | --- |
| Flow | Splash → 3 onboarding slides → Phone → OTP → **Social basics (real name, township, avatar)** → **Dating profile setup (nickname required, photos ≤6, prompts, badges)** → permissions (location "while using", notifications, camera) → privacy switches (radius, `radar_opt_in`, `hide_from_social_friends`) → first 10 suggestions |
| API | `POST /v1/auth/otp/request`, `POST /v1/auth/otp/verify` → JWT+refresh; `PUT /v1/profile`, `PUT /v1/dating-profile` |
| Edge | OTP resend cooldown 60s; multi-device session list; account merge via phone only |
| Safety | Age gate ≥18 (dob); device attestation at first login; rate-limit per IP/device |
| DoD | Myanmar keyboard renders on phone/OTP fields; re-entry flow <90s median; dating nickname never equals real name suggestion copy |

### Module B — Community Feed & Posts (ref 11–13)

| Item | Spec |
| --- | --- |
| Home | Township Highlights row (24h, not stories copy) + ranked feed (§4.5 FeedScore); pull-to-refresh delta sync; skeleton <300ms |
| PostCard | = §4.2 ASCII exactly: author+time+audience icon+⋯ menu; text w/ hashtags & tags; media grid/reel; stats bar (top-3 reaction emoji + counts | comments | shares); Like(long-press=picker)/Comment/Share; bookmark |
| Create Post | ref 12 adopted: audience {Public, Community, Friends}; Gallery(≤9 multi-grid)/Attachment/Location(township chip only)/Add someone(tag); 202 → "under review" ghost state until moderation pass |
| Comments | ref 13: 2-level thread, "Show N more replies", comment like, sticker/photo input, author-reply boost ordering |
| API | `GET /v1/feed`, `POST /v1/posts`, `POST /v1/posts/{id}/reactions` (idempotent, optimistic rollback), `/comments`, `/shares {RESHARE|SEND_DM|COPY_LINK}` |
| DoD | reaction double-tap = LOVE optimistic <16ms render; failed reaction rollback tested; share→DM creates chat deep-link from community post (Dating/Community bridge §4.4) |

### Module C — Search (ref 19–24)

Tabs All/People/Post/Photos/Events adopted. Rules: People = `user_profiles` only — **dating profiles excluded by query guard (§3.3 pattern)**; Events sorted by township relevance + distance bucket; recent searches client-local (privacy); "Photos From Friends/Public" sections = community albums. API: `GET /v1/search?q&type&cursor`. DoD: no dating nickname/handle resolvable via search (pen-test case).

### Module D — Events (ref 25–26)

Cards: hero, title, date/time, description, attendee avatars + "40+ attending", Attend/Not going/share. Nexus adds: township filter chips; host Tier3 badge; venue map = area polygon only; Attend → social proof + optional "X people nearby joined" (§5.3); post-event check-in prompt (Meet Safe §8); share to chat. API: `GET /v1/events?township`, `POST /v1/events/{id}/rsvp`. DoD: host without Tier3 cannot publish venue-precise event.

### Module E — People Near You: Look Around & Radar (ref 27–29) — **core differentiator**

| Item | Spec |
| --- | --- |
| Maps mode | Reference concentric-ring map adopted **with privacy transforms**: self at center; others = anonymous blips (no avatar until tap; jittered position per §2.4); rings = privacy buckets not exact radii; range slider in km buckets (500m/1/3/5/10km); "Show me on map" toggle = `radar_opt_in` (default OFF, per-session auto-expire 15min) |
| Radar sweep | Discover mode: 2.5s GPU sweep; results streamed via WS `radar:{city_id}`; friend-exclusion applied server-side pre-fanout (§3.3); tap blip → ProfileCard mini (nickname, age, badges, distance bucket) → like = swipe action |
| Profile mode (ref 28) | = Look Around list: PostGIS band query (`GET /v1/lookaround?band=500m&township=kamayut`), k-anonymity ≥5/band else auto-widen (§2.5); cards sortable Last-activity (ref 29 "Order by Last Access") |
| Empty state | "Radar quiet" + township community feed nudge (§5.3) |
| Edge | accuracy>100m fixes dropped; spoof>0.8 → invisible (§3.6); offline → cached last list w/ staleness label |
| DoD | packet capture shows zero exact coords; trilateration harness (500 queries) yields ±bucket only; friend-exclusion CI green |

### Module F — Chats, Whisper, Calls (ref 16–19, 30–31)

| Item | Spec |
| --- | --- |
| Chats list | ref 16: online avatars row = matched users with `is_online`; whisper threads show hourglass chip + countdown; community DMs vs match chats segmented |
| Thread | ref 18 adopted (bubbles, voice w/ waveform+download, typing "…is typing", ✓✓ receipts); **Whisper composer toggle**: mode NORMAL|WHISPER; on first whisper use → explainer sheet (self-destruct + screenshot alert honest disclosure §5.5); read → server wipe → client bubble replaced by "message expired" ghost; media purge same |
| Calls | ref 31: 1:1 video/voice, PiP, mute/end/cam; group rooms (create room ref 16); **in-call Report button**; call minutes not stored for whisper matches |
| Create Chat | ref 30 multi-select; guard: cannot DM non-match users beyond 3 messages without mutual interest (anti-spam §7) |
| API/realtime | WS + MQTT fallback, partition `room_id`; `chat.message.read` → whisper wipe consumer (§6.3 design doc); screenshot events → `POST /v1/safety/capture-alert` |
| DoD | wipe latency p95 <2s after read receipt; screenshot alert appears both sides; offline queued send preserves order |

### Module G — Notifications (ref 14–15)

Follow Request (Confirm/Delete) + Activity (followed/shared/liked/commented + Follow Back) adopted; add **Safety notices** tab (report status, action taken, 24h follow-up §5.5); deep-link to source. API: `GET /v1/notifications?category`, FCM/APNs + in-app inbox mirror. DoD: per-category mute settings; no PII in push payload (case_id only).

### Module H — Profile & Dual Profile (ref 32)

Social profile = ref 32 layout (cover, avatar, handle, bio tags, Posts/Photos/Followers/Following stats, photos/videos grids, EDIT/⚙). Nexus adds: segmented switch **Social ↔ Dating** — dating side renders §3.2 editor (6-photo drag&drop reorder, prompts, badges, radius slider, both privacy toggles, Preview Mode as other users will see: nickname only, real name never shown §3.1); verification tier card + upgrade CTA; Nexus+ row; data export/delete (§8)。API `PUT /v1/dating-profile`, `GET /v1/dating-profile/preview`。DoD: preview screenshot diff test vs §3.2 layout; real-name leak scan on dating view (CI)。

### Module I — Dating-only screens (design doc §3/§5, reference မပါ)

Discover swipe stack (card = ProfileCard + prompts; like/pass/activity intent; haptic on match); match modal → chat choose NORMAL/WHISPER + icebreaker chips from `dating_prompts`; Verification flow (selfie liveness Tier1, social links Tier2, ID Tier3); Safety Center (block list, ghost mode, emergency contacts, trusted contacts, report history); Nexus+ paywall (radius, who-liked-me, boost, radar history §9)。

---

## 5. Cross-cutting Implementation Rules

1. **Optimistic everywhere**: reactions/swipes/follows — local first, idempotent server, rollback + snackbar (§4.3 pattern)。
2. **No exact coordinates client-side** in any payload, log, cache key, or analytics event — lint rule + CI grep gate (§2.4)。
3. Pagination: cursor-based; feed delta sync; images: AVIF/WebP + blur-up, data-saver mode, reel autoplay off ≤3G。
4. WebSocket lifecycle: single multiplexed connection (radar + chat + typing + notifications channels), exponential backoff, server heartbeat 25s。
5. i18n: en/my/zh initial; all strings from resource bundle; Myanmar shaping tests (§9)。
6. Accessibility: dynamic type, AA contrast (verify #4C5FD5 on white = 4.6:1 pass), screen-reader labels on blips ("person nearby, about 1 km")。
7. Device floor: Android 8 / 2GB RAM — radar animation ≤5% frame budget on low-end (Flutter DevTools profile gate)。

---

## 6. Safety Guardrails in Code (release-blocking)

| Guardrail | Implementation | Verification |
| --- | --- | --- |
| Friend-exclusion | §3.3 exclusion in every recall path (radar fanout, discovery, match reader, search People, comment visibility) | CI integration suite per release; leak = blocker |
| SafetyScore hard gate | candidate filter before scoring (§3.6) | chaos test: spoof>0.8 user absent from all lists |
| Tier0 restriction | unverified → community read-only | e2e test |
| Whisper wipe | read-receipt consumer, DB delete + S3 expire | wipe-latency SLO + forensic sample audit |
| Screenshot alert | FLAG_SECURE / iOS capture API → both-side alert | device matrix test |
| Report ≤3 taps | SafetySheet global | UX audit + SLA timer test (HIGH 30min) |
| Age gate | DOB ≥18 at signup, face-estimate secondary | store compliance review |

---

## 7. Engineering Process

**Squads (7)**: Mobile (2 pods), Platform-Backend (location/auth/realtime), Social-Backend (feed/posts/events/search), Dating-Backend (match/radar/whisper), AI/ML, Safety-Ops Tools, QA/Release。Design embedded across pods。

**Repo**: monorepo — `apps/mobile`, `services/{location,match,social,chat,community,safety,notify,media}`, `packages/{geo-contract,design-tokens,proto}`, `infra/`, `e2e/`。Trunk-based + release branches per city rollout。

**Sprint plan (2-week cadence, mapped to design doc §12 Phases)**:

| Sprints | Deliver |
| --- | --- |
| S1–S3 (Phase 0) | design tokens + component kit; auth/OTP; dual-profile schemas; Location v1 (Redis GEO+PostGIS); privacy switches; guardrail CI skeleton |
| S4–S7 (Phase 1) | Discover stack, rule-match, chat WS, **Whisper v1**, report/block, push; Yangon closed beta (Module I + F + A) |
| S8–S12 (Phase 2) | Community feed B, Create Post, Comments, Search C, Events D, **Look Around/Radar E** (ref 27–29 adapted), Notifications G, Profile H |
| S13–S16 (Phase 3) | two-tower + feed ranking, risk engine, scam classifier, icebreaker LLM, moderation dashboards |
| S17–S20 (Phase 4) | multi-city config, Nexus+, promoted events, host verification, scale hardening |

**DoR**: spec §4 row complete + API contract merged + telemetry plan。
**DoD**: unit+widget tests ≥80% module coverage, contract tests pass, guardrail suite green, a11y + Myanmar render check, low-data profile run, staging soak 48h, feature-flag registered。

**CI/CD**: PR → lint/detekt-analog, unit, widget, contract (Pact), guardrail subset, secret-scan, SCA → merge → canary build (internal) → staging e2e (Maestro flows: signup→match→whisper wipe; post→react→rollback; radar privacy) → k6 load (radar fanout 15K WS, feed p95) → prod Argo canary 5%→25→100 with SLO auto-rollback。Kill-switch per AI/moderation feature (§11 design doc)。

---

## 8. Production Readiness & Launch

**SLOs**: rank p95 <80ms · feed p95 <120ms · radar push p95 <400ms · message delivery p95 <1.5s · whisper wipe p95 <2s · safety HIGH response ≤30min — dashboards + paging alerts。

**Capacity** (Y1 300K DAU): Redis GEO per-city shards, WS sticky-hash gateways, reaction counters Redis-primary flush, Kafka 3-AZ; load test at 2× launch city peak。

**Degradation drills** (quarterly): ranking-down→rules, Redis-lag→PostGIS stale, radar→static list, push→in-app queue。DR: PITR 15min RPO / 1h RTO tabletop。

**Security checklist**: TLS1.3 + cert pinning, KMS envelope for lat/lng, Vault, device attestation, R8/obfuscation, no PII in logs (lint), dependency+image scanning, annual pentest + bug bounty, incident runbook (friend-leak, coord-leak, whisper-abuse)。

**Store compliance**: Play Data Safety + App Store privacy labels (location≈coarse, contact data, messages E2E claim only if true), age rating 17+/18+, location permission rationale strings, DSR endpoints live before launch。

**Launch gates (per city)**: township density ≥ threshold for k-anonymity bands · moderators staffed per §8 SLA · guardrail suite green · pentest passed · war-room + rollback rehearsed。

---

## 9. QA Acceptance Matrix (summary)

| Area | Key cases |
| --- | --- |
| Privacy | coord never in any response/log; bucket jitter; rate-limit distance probes |
| Guardrail | friend-exclusion across radar/search/feed/comments; tier restrictions |
| Whisper | wipe timing, screenshot alert, report-with-snapshot path |
| Reactions | optimistic+rollback, enum replace, counter flush |
| Myanmar UI | stacked consonants, line-height 1.6, keyboard on OTP/phone |
| Low-end device | radar FPS, feed scroll jank, 3G reel policy |
| Calls | PiP, reconnect, in-call report |
| Compliance | age gate, DSR export/delete, store labels |

---

## 10. Definition of "Production-Ready" (final checklist)

☑ All §4 modules DoD met ☑ §6 guardrails CI-green + pentest ☑ SLO dashboards + paging live ☑ degradation+DR drills executed ☑ moderation staffing + SLA routing live per city ☑ store compliance + privacy labels approved ☑ analytics + north-star metric pipeline ("Safe Successful Connections/week") reporting ☑ rollback rehearsed ☑ on-call rota + runbooks published。

## 11. Out of Scope (explicit)

Hyper-Local Marketplace, item trading, MMK/Nexus Coins flows, seller tooling — reference mockup ထဲ ပါသော်လည်း design doc အရ **ဖယ်ရှားပြီး**；build ထဲ ပြန်မဝင်စေရ (PR review checklist item)။

---

*Guide version: Nexus v2 companion。Foundation: "Nexus Platform Design" (confirmed)。Visual reference: social UI kit screenshots 1–13 + NexusHub ecosystem mockup。Burmese/English mixed by team convention — copy strings final in i18n bundle (§5.5)。*
