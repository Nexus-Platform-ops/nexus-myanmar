# Nexus — Location-first, Safety-first, AI-enhanced Dating + City Social Community Hybrid Platform

## စာတမ်းအနှစ်ချုပ် (Executive Summary)

Nexus (မူရင်း BeeTalk) ကို **တစ်ခုတည်းသော Location Engine** ပေါ်မှာ **Dating** နှင့် **City Social Community** ကို တစ်ပြိုင်နက်တည်ဆောက်ထားတဲ့ Hybrid Platform အဖြစ် ဒီဇိုင်းဆွဲထားပါတယ်။ Local Marketplace layer ကို ဖယ်ရှားထားပြီး၊ platform ရဲ့ ရည်မှန်းချက်က commerce မဟုတ်ဘဲ **city-level social connection** သာ ဖြစ်ပါတယ်။ ဗားရှင်းလည်တွင် **BeeTalk-Inspired Dating System** (Look Around / Radar Scan / Whisper Mode Chat + သီးသန့် Dating Profile) နှင့် **Facebook-Style Social Post Architecture** (Reactions / Nested Comments / Shares / Newsfeed Optimization) တို့ကို Developer တည်ဆောက်နိုင်ရန် schema/UI/logic အထိ ထည့်သွင်းပြင်ဆင်ထားပါသည်။ အဓိက ဆုံးဖြတ်ချက် ၄ ချက်—

| ဆုံးဖြတ်ချက် | အကျိုးဆက် |
| --- | --- |
| Location ကို Feature မဟုတ်ဘဲ **Platform Layer** အဖြစ် သတ်မှတ်ခြင်း | Dating နှင့် Community အားလုံးက တစ်ခုတည်း Geo Index ကို共用 → data consistency မြင့်၊ development cost နည်း |
| Safety ကို UI ထဲမဟုတ်ဘဲ **Architecture ထဲ** ထည့်ခြင်း (verification, anti-trilateration, risk scoring) | TrustScore က Matching Algorithm ၏ input ဖြစ်လာ → safety = ranking signal |
| AI ကို Recommendation သာမက **Moderation + Fraud + Compatibility + Newsfeed ranking** အတွက်လည်း သုံးခြင်း | Moderate scale (1M DAU) မှာ human review cost ကို 60–70% လျှော့ချနိုင် |
| **Profile Data Isolation** — Social (`user_profiles`) နှင့် Dating (`dating_profiles`) table ချင်း သီးသန့်ခွဲခြင်း | အမည်ရင်းနှင့် Dating nickname မရောထွေး; "Hide from friends" guardrail ကို backend query ဖြင့် အာမခံ |

> Naming: BeeTalk → **Nexus** သို့ ပြောင်းလဲပြီး။ "Chitchat (ချစ်-ချတ်)" ကို Dating/Chat module ၏ sub-brand idea အဖြစ် §15 တွင် မှတ်စုထားသည်။

---

## 1. Product Concept — Layer Model (Hybrid)

| Layer | အဓိက ရည်ရွယ်ချက် | Core Deliverable |
| --- | --- | --- |
| **Location Layer** | Dating နှင့် Community နှစ်ခုလုံးအတွက် common base | Geohash/S2 index, Redis GEO, PostGIS, Privacy Radius, Look Around, Radar Scan |
| **Dating Layer** | Nearby user များကို match, chat, meet — BeeTalk legacy features ပါဝင် | MatchScore engine, Swipe/Discover, Separate Dating Profile, Radar, Whisper Mode Chat |
| **Community Layer** | မြို့/မြို့နယ်အလိုက် City Social Community — Facebook-style posts | City Boards, Social Post Feed (Reactions/Comments/Shares), Interest Communities, Events |
| **AI Layer** | Match, ranking, fraud detection, personalization, newsfeed optimization | Two-tower ranking, feed ranking, LLM assist, Risk model |
| **Safety Layer** | Verification, moderation, privacy, anti-scam, identity guardrails | TrustScore, Anti-trilateration, Friend-exclusion logic, Review pipeline |

**Platform flywheel (social-only)** — Location → Community posts & events → People interact → Network effect per township/city → ပိုမြင့်တဲ့ nearby match density (Radar/Look Around) → Dating retention တက် → ယုံကြည်မှုရှိတဲ့ social graph ပိုထူ။ Dating-only app များ၏ churn ပြဿနာကို City Social Community က ဖြေရှင်းပေးတာက Nexus ရဲ့ structural advantage ဖြစ်ပါတယ်။

---

## 2. Location Engine — Platform ၏ အခြေခံအုတ်မြစ်

### 2.1 Location Data Model

```text
UserLocation
  user_id            UUID, PK
  lat / lng          NUMERIC(10,7) — encrypted at rest (pgcrypto / KMS envelope)
  geohash            TEXT, indexed        # precision 6 ≈ 1.2km, 8 ≈ 19m
  s2_cell_id         BIGINT, indexed      # hierarchical, multi-resolution
  city_id            FK -> City
  district_id        FK -> Township       # Kamayut / Bahan / Chanayethazan ...
  privacy_radius     ENUM {EXACT, 500m, 1km, 5km, CITY_ONLY, INVISIBLE}
  coarse_grid_key    TEXT — server-side obfuscated value actually exposed
  radar_opt_in       BOOL — "Show me on Radar & Look Around" switch (dating_profiles)
  last_fix_at        TIMESTAMPTZ
  last_active_at     TIMESTAMPTZ
  is_online          BOOL (Redis authoritative, DB projection)
  device_ip_geo      STRING (fallback + fraud signal)
  location_source    ENUM {GPS, WIFI, CELL, IP, MANUAL}
  accuracy_m         INT
  spoof_risk_score   FLOAT
```

### 2.2 Indexing Strategy

| Index | သုံးတဲ့နေရာ | ဘာကြောင့် |
| --- | --- | --- |
| **Redis GEO** (Sorted Set + GeoAdd) | Online nearby query (<50ms), **Radar Scan** | In-memory radius query အလွန်မြန် — စက္ကန့်ပိုင်း refresh; TTL ဖြင့် auto-expire |
| **GeoHash prefix** (precision 5–7) | Community township/city scoping | String prefix → hotspot aggregation, cache key အဖြစ် direct သုံးနိုင် |
| **S2 CellID** (level 12–16) | Fine-grained matching, anti-spoof | Hierarchical parent-child → precision level switch |
| **PostGIS GiST** (`geography` + `ST_DWithin`) | **Look Around** meter-band queries, admin heatmap, history | Extension မှာ real-time coordinate radius ဆွဲပြနိုင် (Mongo `2dsphere` alternative) |

### 2.3 Location Update Flow (End-to-End)

```text
1. App collects GPS/WiFi/Cell fix
2. Client-side filter: ignore if accuracy > 100m; throttle to min 30s interval
3. POST /v1/location (TLS, signed request, nonce)
4. Location Service:
   a. computes geohash + S2 cellid
   b. writes Redis GEO (key: geo:online:{city_id})   → realtime (Radar source)
   c. writes PostGIS encrypted record (async via Kafka) → durable (Look Around source)
   d. publishes location.updated event
5. Match Service consumes event → invalidates candidate cache, re-ranks feed
6. Reader/Feed/Community Services push updated nearby lists (WebSocket)
```

### 2.4 Privacy Radius & Anti-Trilateration (Safety-critical)

Exact coordinate **ဘယ်တော့မှ client သို့ မပို့ပါ**။

- Distance ကို server တွက်ပြီး **bucket** သာ ပြ: `<500m` `1km` `3km` `5km` `10km+`
- Bucket boundary တွင် **jitter** ထည့် — distance value ကို time အလိုက် ±10% ပြောင်းလဲပြီး return လုပ်။ Trilateration attack ကို တားဆီး။
- Distance query **rate-limit**: user B မှ user A distance ကို တစ်နာရီလျှင် 5 ကြိမ်သာ တောင်းခွင့်။
- "Show me on map" default OFF; ON = per-session, auto-expire (15 min)။
- Check-in များ **delayed publish** (≥10 min) ဖြင့် broadcast — live location leak ရှောင်။

### 2.5 BeeTalk Legacy Location Features — Nexus Implementation

| Legacy Feature | Nexus Implementation | Privacy reconciliation |
| --- | --- | --- |
| **Look Around (LBS)** — GPS ဖြင့် မီတာ ၅၀/၁၀၀ မှစ၍ မြို့နယ်အလိုက် nearby dating profiles | PostGIS `ST_DWithin(geog, point, meters)` + township filter UI (Kamayut/Bahan/...); band selector 50m/100m/500m/1km... | Client သို့ profile cards + distance bucket သာ; band ≤100m တွင် distance ကို "အနီးအနား (<100m)" စာသားတည်း ပြ၊ numeric မပြ။ k-anonymity ≥ 5 profiles/band rule — density မလုံလောက်လျှင် band ကို auto-widen |
| **Radar Scan** — radar sweep animation ဖြင့် nearby online users ကို စက္ကန့်ပိုင်း ပြ | Redis GEO `GEORADIUS` via WebSocket channel `radar:{city_id}` (server push, no polling), client sweep animation 2.5s cycle | Result တွင် `radar_opt_in = true` user များသာ ပါ; tap မတိုင်မီ anonymous blip + distance ring သာ (nickname/avatar မပြ) |
| **Whisper Mode Chat** — match ဖြစ်ပြီး စာပို့လျှင် ဖတ်ပြီးသည်နှင့် auto-delete | Chat Service `mode = WHISPER`: read receipt ကို Kafka consumer ဖြင့် detect → server-side content wipe (DB row delete + media purge) + client local wipe | Whisper chat တွင် screenshot alert (§8) အထူး active — self-destruct expectation ကို အသုံးပြုသူ သိရှိစေရန် first-use explainer |

---

## 3. Dating System — Separate Profile Architecture & Matching

### 3.1 Dual-Profile Data Isolation (Technical Directive #1)

Main Social Account နှင့် Dating Profile ကို **table ချင်း သီးသန့်ခွဲ** — real name နှင့် dating nickname ရောထွေးမှု ရှောင်။

```text
user_profiles (Social)                    dating_profiles (Dating — separate schema)
  user_id PK                               profile_id PK
  real_name                                dating_nickname        # e.g. "Cherry"
  avatar, bio, friends_list_ref            user_id FK  (1:1, UNIQUE)
  township, joined_communities             photos[] (≤6, ordered) # drag & drop reorder
  privacy: profile_visibility              age, gender_identity, gender_preference
                                           township + distance_radius_km    # slider, e.g. Within 10 km
                                           short_bio, dating_prompts[]      # icebreakers
                                           interest_tags[] / lifestyle_badges
                                           radar_opt_in          # "Show me on Radar & Look Around"
                                           hide_from_social_friends BOOL
                                           verification_tier, status
```

- `dating_profiles` သည် separate storage/partition ဖြင့် social analytics pipeline မှ opt-out။
- Dating UI မှာ `user_profiles.real_name` ကို ဘယ်နေရာမှ မrender — nickname only။

### 3.2 Separate Dating Profile Layout (Settings/Editor UI)

```text
+------------------------------------------------------------------------------------+
|                         SEPARATE DATING PROFILE INTERFACE                          |
+------------------------------------------------------------------------------------+
| [Header] < Back | Dating Profile Settings | Preview Mode                           |
+------------------------------------------------------------------------------------+
| [Photo Grid Slider] Up to 6 Photos (Drag & Drop Reorder)                           |
+------------------------------------------------------------------------------------+
| [Basic Dating Info]                                                                |
| - Display Name (Nick Name) | Age | Gender Identity & Preference                    |
| - Location / Township | Distance Radius Slider (e.g., Within 10 km)               |
+------------------------------------------------------------------------------------+
| [Dating Bio & Prompts]                                                             |
| - Short Bio: "ခရီးသွားရတာ ကြိုက်တယ်..."                                              |
| - Dating Prompts / Icebreakers (e.g., "My ideal weekend is...")                    |
+------------------------------------------------------------------------------------+
| [Interests & Lifestyle Badges] (Multi-select Tags)                                 |
| [ Gaming ] [ Coffee Lover ] [ Movies ] [ Night Life ] [ Music ]                    |
+------------------------------------------------------------------------------------+
| [Privacy & Discovery Switch]                                                       |
| - [ Switch ] Show me on Radar & Look Around                                        |
| - [ Switch ] Hide my Dating Profile from Main Social Friends                       |
+------------------------------------------------------------------------------------+
```

### 3.3 Identity Guardrail — "Hide from Friends" Exclusion Logic (Technical Directive #4)

`hide_from_social_friends = true` ဖွင့်ထားလျှင် Backend ရှိ candidate generation **အားလုံး**တွင် exclusion query run လုပ်ရမည် — Dating Radar/feed ထဲ user ၏ social friend list ရှိသူများ မပေါ်ရ။

```sql
-- candidate pool (PostgreSQL)
SELECT dp.* FROM dating_profiles dp
JOIN user_locations ul ON ul.user_id = dp.user_id
WHERE ST_DWithin(ul.geog, :me, :radius_m)
  AND dp.radar_opt_in = TRUE
  AND NOT EXISTS (                       -- friend exclusion guardrail
    SELECT 1 FROM friendships f
    WHERE f.user_id = :viewer_id
      AND f.friend_id = dp.user_id
  );
```

WebSocket Radar push တွင်လည်း same filter ကို fanout မတိုင်မီ apply; cache key တွင် viewer hash ပါဝင်ရ (`radar:{city}:{viewer_hash}`)။ Guardrail ကို integration test suite ဖြင့် per-release verify — leak = **release blocker**။

### 3.4 Reader Module (3 types)

| Reader | Content | Signal Source |
| --- | --- | --- |
| **Discovery Reader** | Nearby users (Look Around), Radar blips, new users, trending profiles | Location + freshness + popularity |
| **Interest Reader** | Shared tags, communities, events, social posts | Tag overlap, community membership |
| **Match Reader** | Mutual-interest, chat-ready profiles (incl. Whisper option) | Mutual like, compatibility score, activity |

### 3.5 MatchScore Formula

```text
MatchScore = w1·LocationScore + w2·PreferenceScore + w3·InterestScore
           + w4·ActivityScore + w5·SafetyScore + w6·AICompatibilityScore
```

| Component | Signals |
| --- | --- |
| LocationScore | Distance bucket, township match, radius band stability |
| PreferenceScore | Age, gender identity/preference, language, relationship goal |
| InterestScore | Lifestyle badges/tags, communities, events, shared venues |
| ActivityScore | Online status, Radar activity, reply rate, last active |
| SafetyScore | Verification tier, report history, spoof score (hard-gate §3.6) |
| AICompatibilityScore | Prompt/bio embedding, chat style, values similarity |

### 3.6 SafetyScore Hard Gate

```text
if trust_tier < VERIFIED_PHONE  → discovery feed ပါခွင့်မရ (community read-only သာ)
if spoof_risk_score > 0.8       → nearby/Radar/match pool မှ ချက်ချင်း ဖယ်, review queue
if open_reports_severity >= HIGH→ chat disabled, profile hidden pending review
```

Risky user သည် match pool ထဲ ဝင်ရန်တောင် မဝင်နိုင် — "Safety-first" ကို architecture ဖြင့် အမှန်တကယ် ကျေပွန်စေသည်။

### 3.7 Three-stage Ranking Pipeline

```text
Stage 1 — Candidate Generation (<20ms)
   Redis GEO radius → ~500 ; ES tag query → ~300 ; friend-exclusion filter (§3.3) ; union+dedupe
Stage 2 — Heavy Scoring (<30ms)
   Two-tower (user × dating_profile tower) → dot product ; rule blend (formula §3.5)
Stage 3 — Re-rank & Business Rules (<15ms)
   Diversity (township cap, community cap), freshness boost, safety re-check
```

---

## 4. Facebook-Style Social Post System (Community Layer)

### 4.1 Post Data Model

```text
social_posts
  post_id PK, author_user_id FK(user_profiles), township_id FK
  body_text, hashtags[], mentioned_user_ids[], tagged_user_ids[]   # photo tagging
  media[] (images grid | reel video, CDN urls), media_s2_cell
  audience ENUM {PUBLIC, COMMUNITY, FRIENDS}
  geo_context    # township label သာ ပြ — exact coord မပါ (§2.4 ကျင့်)
  status {LIVE, UNDER_REVIEW, REMOVED}
post_reactions   post_id, user_id, reaction ENUM, ts   # UNIQUE(post_id, user_id)
post_comments    comment_id, post_id, parent_comment_id (nested), user_id, body, media[], ts
post_shares      share_id, post_id, user_id, mode {RESHARE, SEND_DM, COPY_LINK}, ts
```

### 4.2 Post Card UI (per spec)

```text
+------------------------------------------------------------------------------------+
|                          FACEBOOK-STYLE SOCIAL POST CARD                           |
+------------------------------------------------------------------------------------+
| [Author Header]                                                                    |
| [Avatar] Profile Name · 2 hrs ago · [ Public / Friends Icon ]      [ ... Menu ]    |
+------------------------------------------------------------------------------------+
| [Post Text Area]                                                                   |
| "ဒီနေ့ မြို့နယ် အသစ်မှာ ဖွင့်တဲ့ ဆိုင်လေး ရောက်ခဲ့တယ်..." (Supports Hashtags & Tags)      |
+------------------------------------------------------------------------------------+
| [Media Viewport] Multi-Image Grid Layout / Reel Video Viewport                     |
+------------------------------------------------------------------------------------+
| [Social Engagement Stats Bar]                                                      |
| (👍❤️😂 450 Reactions)                              | 32 Comments | 12 Shares |
+------------------------------------------------------------------------------------+
| [Interactive Action Buttons]                                                       |
| [ 👍 Like (Hover for Reactions) ]  | [ 💬 Comment ]  | [ ↗️ Share ]                 |
+------------------------------------------------------------------------------------+
| [Nested Comment Section]                                                           |
| - User 1: "နေရာလေး လိပ်စာလေး ပြောပြပေးပါဦး"                                          |
|   └─ Reply Button | Like | 1 hr ago                                                |
| - [ Input Field: Write a comment... ] [ Sticker ] [ Photo Input ]                  |
+------------------------------------------------------------------------------------+
```

### 4.3 Reaction Logic (Technical Directive #3)

- Enum: `reaction_type ENUM {LIKE, LOVE, HAHA, WOW, SAD, ANGRY}`; user per post တစ်ခုတည်း (re-select = replace)။
- **Optimistic UI Rendering**: tap → client က ချက်ချင်း +1/selected state render → `POST /v1/posts/{id}/reactions {type}`; server idempotent by `(post_id, user_id)`; failure → rollback + snackbar။
- Counter aggregation: Redis `HINCRBY post:{id}:reactions {type}` + periodic flush to Postgres; stats bar emoji top-3 by count။
- Reaction events → Kafka `post.reaction.created` → newsfeed ranking feature။

### 4.4 Comments & Shares

- Nested comments: thread hybrid (`parent_comment_id`; deep replies collapse "View more replies")။ Ordering: author-reply boost + comment reactions + time decay။ Reply / Like / sticker / photo input supported; same moderation hook as posts။
- Share modes: **RESHARE** (quote optional, own feed or community board), **SEND_DM** (chat ထဲ မျှဝေ — Dating chat နှင့် Community bridge: community post ကို match chat ထဲ share လုပ်၍ conversation starter ဖြစ်စေ), **COPY_LINK** (public posts သာ)။

### 4.5 Newsfeed Optimization (AI)

```text
FeedScore = f(affinity(author, viewer), engagement_velocity, recency,
              township_relevance, community_membership, reaction_mix_weight)
```

Recall via graph + geo + community channels → rank (§3.7 pattern) → business rules (township diversity quota, creator diversity cap, safety re-check per slot)။ Reaction mix weight: LOVE/WOW signals > LIKE; SAD/ANGRY သည် ranking ထဲ negative-empathy signal အဖြစ်သာ သုံး (down-rank မဟုတ်) — engagement-bait suppression။

---

## 5. UI/UX Strategy — Borrowing Best Patterns

### 5.1 Design Principles

1. **Map is a mode, not the home screen.** Tinder/Bumble-style card feed က default Discover home; Look Around/Radar map က opt-in mode။
2. **One intent per screen.** Discover / Community / Chats / Me — bottom nav 4 ခု; Radar/Look Around သည် Discover အတွင်း mode toggle; social posts သည် Community feed။
3. **Safety visible at all times.** Verified badge, trust level, location-sharing status, Whisper indicator ကို top chip/inline အဖြစ် အမြဲပြ။
4. **Progressive disclosure of location.** Onboarding မှာ "township only" default; precision တက်သည်နှင့် functional unlock (nearby list → Radar → sub-km Look Around)။
5. **Low-bandwidth first.** Image placeholder + blur-up, lazy list, data-saver mode; reel autoplay OFF on 3G။

### 5.2 Information Architecture

```text
Bottom navigation
├── Discover   (Swipe cards | Look Around band selector | Radar sweep | Map mode)
├── Community  (Township Social Feed §4 | City Board | Interest groups | Events)
├── Chats      (Matches, Whisper conversations, Community DMs, System/Safety notices)
└── Me         (Social Profile | Dating Profile §3.2 | Privacy toggles | Verification | Nexus+ | Safety Center)
```

### 5.3 Key Screen Flows

**Onboarding:** Phone OTP → language → social profile basics → **separate dating profile setup (nickname mandatory)** → photo verify → location permission rationale ("While using app") → privacy radius + Radar opt-in + Hide-from-friends toggles → interest badges → first 10 suggestions။

**Discover:** Card stack (dating profile §3.2 data) → like/pass/"Start city activity" → mutual match → chat: normal or **Whisper mode** toggle with self-destruct explainer; icebreaker prompts generated from `dating_prompts`။

**Radar UX:** Center = self; sweeping ring animation (2.5s GPU-optimized); blips = opted-in online users by distance ring; tap → minimal card (nickname, age, badges, distance bucket) → like from radar = same swipe action။ Empty-state: "Radar quiet" + township community feed nudge → low-density moments က social browsing အဖြစ် convert (churn protection)။

**Community:** Township feed (§4) — post composer with multi-image/reel, hashtag & photo tagging, audience selector (Public/Community/Friends); event listings; "X people nearby joined this event" social proof without identities။

**Safety Center:** One-tap block, report (≤3 taps) + evidence attach, "ghost mode" instant hide, trusted-contact share, emergency contacts, data export/delete။

### 5.4 Mobile Interaction Design

| Domain | Pattern | Anti-pattern Nexus ရှောင်သည် |
| --- | --- | --- |
| Gestures | Swipe = intent; long-press card = safety menu; long-press Like = reaction picker (hover on web) | Pinch-map as primary discovery (location exposure risk) |
| Feedback | Haptic on match; skeleton loaders <300ms; optimistic reactions | Toast spam for likes (dopamine inflation → churn) |
| Motion | 200–300ms ease-out; reduced-motion option; radar sweep GPU-optimized | Continuous auto-refresh map pings |
| Accessibility | Dynamic type, WCAG AA contrast, Burmese line-height ≥1.6 | Image-only content (low-data devices fail) |
| Typography | Myanmar font (Noto Sans Myanmar / Padauk) with stacked consonant rendering | Latin-only type scale |
| Content | Emoji/sticker local packs, voice messages, reels | Text-only chat |

### 5.5 Trust & Persuasion UI

- Verification badges: **Phone ✓ / Selfie ✓✓ / Social links ✓✓✓** tiers by color intensity (visual hierarchy = trust hierarchy)။
- Report flow ≤3 taps; "You will not be shown this person again"; 24h safety follow-up notification။
- Screenshot detection (Android FLAG_SECURE / iOS capture notification) → counterpart alert; **Whisper chats တွင် ပိုမတင်းကျပ်**: alert + safety log entry (§8)။

---

## 6. Backend System Design

### 6.1 Service Topology

```text
                    ┌─ CDN (edge) ─ Media/Static
Mobile ─ TLS ─► API Gateway (Kong) ─► Auth (JWT+OAuth, OTP)
        │
        ├─ WebSocket Gateway ─► Chat Service ─► Kafka(chat.msg) ─► persistence
        │            ├─ Whisper lifecycle (read-receipt wipe consumer)
        │            └─ Radar channel server (geo fanout push)
        ├─ Location Service ─► Redis GEO / PostGIS (write path async)
        ├─ Match/Dating Service ─► candidate svc + ranking svc (GPU/ANN) + dating_profiles
        ├─ Social Feed Service ─► posts/reactions/comments/shares + feed ranking
        ├─ Reader/Feed Service (GraphQL BFF)
        ├─ Community Service (+ Events sub-module)
        ├─ Media Service (S3/GCS, transcode, moderation hook)
        ├─ Notification Service (FCM/APNs)
        ├─ Safety Service (verification, moderation, risk engine, guardrail audit)
        └─ Admin Service (moderation dashboard, city ops)

Data: PostgreSQL+PostGIS — user_profiles / dating_profiles separate schemas
    · Redis (GEO + Streams + reaction counters) · Elasticsearch/OpenSearch · Kafka
    · ClickHouse (behavior analytics) · Cassandra/Mongo (chat history; Mongo 2dsphere
      optional radar replica) · S3+CDN
AI: Feature store · model registry · offline Spark · online TF Serving/Triton
```

### 6.2 API Contracts (核心 examples)

```text
POST /v1/location:patch          {lat,lng,accuracy,source} → 204 (no echo of stored value)
GET  /v1/radar/subscribe         → WS channel; opt-in users only; friend-filtered
GET  /v1/lookaround?band=500m&township=kamayut → cards + distance_bucket only
POST /v1/actions/swipe           {profile_id, intent} → 201 {matched:bool}
POST /v1/chats/{id}/messages     {text, mode: NORMAL|WHISPER}
POST /v1/posts                   {body, media[], audience, hashtags[], tag_user_ids[]} → 202
POST /v1/posts/{id}/reactions    {type}  → optimistic client, idempotent server
POST /v1/posts/{id}/comments     {parent_comment_id?, body, media?}
POST /v1/posts/{id}/shares       {mode: RESHARE|SEND_DM|COPY_LINK}
GET  /v1/feed?type=community&township_id=... → ranked posts (§4.5)
POST /v1/safety/report           {target_id, category, evidence[]} → 202 case_id
PUT  /v1/dating-profile          {nickname, photos[], prompts[], radar_opt_in,
                                  hide_from_social_friends, radius_km}
```

### 6.3 Event-driven backbone (Kafka topics)

`location.updated`, `user.action.swipe`, `match.created`, `chat.message`, `chat.message.read` → **whisper wipe consumer**, `safety.report.created`, `community.post.published`, `community.event.joined`, `post.reaction.created`, `post.comment.created`, `post.share.created` — AI feature pipeline, analytics, notification fanout အားလုံး event consume သာ; services တွေ direct sync call မလုပ် (decoupling for scale)။

### 6.4 Realtime & Offline

Chat/Radar: WebSocket + MQTT fallback; per-room ordering via Kafka partition key `room_id`; delivery state machine (sent/delivered/read) in Redis Streams → `chat.message.read` event triggers whisper content purge (DB row delete + S3 object expiry)။ History Cassandra/Mongo wide-partition; TTL per privacy setting။

Low-data: GraphQL response compression, delta feed sync, queued write-behind for swipes offline။

### 6.5 Geospatial Querying Directive (Technical Directive #2)

Radar/Look Around realtime queries အတွက် primary = **PostGIS** (`ST_DWithin` + GiST index) — relational schema နှင့် transactional consistency ကာကွယ်; hot path = **Redis GEO** (<50ms radar fanout)။ MongoDB `2dsphere` သည် chat store ရှိ radar workload ရှင်သန်လျှင် allowed replica option — Location Service သည် `geo:online:{city}` + township filter contract ကို single source of truth အဖြစ် ထိန်းရမည်; dual-write via Kafka၊ client-side dual-read path မထားရ။

---

## 7. AI Layer

| Capability | Model | Online/Offline | Purpose |
| --- | --- | --- | --- |
| Two-tower compatibility ranking | Embedding (user × dating_profile) | Online | MatchScore §3.5 |
| Chat-style/values similarity | LLM sentence embedding + scorer | Online | AICompatibilityScore |
| AI icebreaker & coaching | Guardrailed LLM | Online | First-message quality (uses dating_prompts) |
| Fake-account & bot detection | GNN on device/graph + profile dup | Online | Auto-block before first match |
| Spam/romance-scam script detection | Text classifier (Myanmar+English) | Online | Chat/Whisper gating, escalate human |
| Spoof/GPS-fraud | Velocity + wifi/cell cross-check + attestation | Online | SafetyScore input |
| Content moderation (image NII, text) | Vision + text classifiers | Online async | Posts/comments/reels |
| Newsfeed ranking + engagement prediction | LambdaMART / DNN pointwise | Online | FeedScore §4.5 |
| City social demand forecasting | Time-series per S2 cell | Offline | Community/event ops |

**AI governance**: training consent flag + pseudonymization; user delete → feature-store TTL exclusion; moderation decisions appealable — no model-only bans; **dating_profiles embeddings သည် social feed ranking ထဲ မပါဝင်ရ** (data-isolation extension)။

---

## 8. Safety Layer — The Differentiator

```text
Verification Pyramid          Risk Engine (real-time)         Response Ops
Tier0 phone OTP         ◄──►  velocity / device / geo   ◄──► Tiered review queue
Tier1 selfie liveness         inconsistency (city vs IP)      L1 auto-action
Tier2 social links            behavior anomaly (msg blast)    L2 moderator <2h
Tier3 ID (event hosts,        trust graph (report neighbors)  L3 safety team <30m
      moderators)                 whisper screenshot abuse
```

- **Anti-trilateration** — §2.4; Look Around meter bands via k-anonymity + copy-only sub-100m (§2.5)။
- **Privacy Radius & Radar opt-in** — INVISIBLE = community read သာ; radar off = no nearby exposure။
- **Hide-from-friends guardrail** — §3.3; exclusion ကို recall path အားလုံးတွင် CI-test (Radar push, Discovery, Match Reader, comment visibility)။
- **Whisper Mode safeguards** — server-side wipe + read receipt; screenshot alert; abuse-in-whisper reportable via safety-log snapshot taken before wipe (content not shown to reporter; case evidence retained per policy)။
- **Screenshot alert** + **panic hide** (profile + chat instant lock)။
- **Report → SLA**: HIGH 30 min / MEDIUM 2 h; per-city moderator staffing coupled to community growth။
- **Meet Safe**: event venue check-in (delayed publish) + trusted-contact auto-notify + post-meeting check-in; offline event hosts require Tier3 ID။
- **Data compliance**: GDPR-style lawful basis, PDPA-aware local law mapping, DSR endpoints (export/delete), per-city data residency option။

---

## 9. Business Alignment & Sustainability

| Stakeholder | Value | Lever |
| --- | --- | --- |
| Daters | Safer, denser nearby matches (Radar/Look Around/Whisper) | **Nexus+ subscription**: expanded radius, who-liked-me, boost, radar history |
| Community members | Township belonging, Facebook-grade social loop | Premium community tools (moderator dashboards, larger events) |
| Event hosts | Verified nearby audience | Host verification tier, promoted event placement |
| Platform | Social ↔ dating cross-funnel retention | Subscription attach; LTV via community engagement |
| Trust | Safer ecosystem | Verification upsell, premium safety features |

City launch playbook: **township-level density first** (Kamayut → Bahan → Chanayethazan pattern per city): seed interest communities, launch township feed, then flip dating layer on warm graph — low-density townships rely on Radar-empty-state community nudge (§5.3)။

North-star metric: **Safe Successful Connections per Active User per week** (matches with ≥5 exchanged messages + verified/no open report; community = meaningful post interactions per member)။

---

## 10. Tech Stack Recommendation

| Layer | Choice | Rationale |
| --- | --- | --- |
| Mobile | Flutter + native location/safety plugins (CoreLocation / FusedLocation) | two platforms, Myanmar market; safety-critical bits native |
| API | Go (location/match/chat/radar) + Node/TS BFF (reader/feed) | Go = low-latency geo concurrency; TS = iteration speed |
| Contracts | gRPC, protobuf | schema discipline for event contracts |
| Data | PostgreSQL+PostGIS (separate dating/social schemas), Redis(GEO/Streams/counters), OpenSearch, Kafka, ClickHouse, Cassandra/Mongo(chat), S3+CDN | Directive #2 compliant |
| AI/ML | Python, PyTorch, Faiss/pgvector, feature store, Triton | two-tower + feed ranking serving |
| DevOps | Docker, Kubernetes, Helm, Terraform, GitHub Actions, Argo Rollouts (blue-green/canary) | multi-city rollout |
| Observability | Prometheus+Grafana, Loki/ELK, OpenTelemetry, Sentry; SLO dashboards: rank p95, safety SLA | |
| Security | WAF, rate-limit, bot mgmt, Vault/KMS envelope encryption, device attestation (Play Integrity / App Attest) | |

---

## 11. Production Readiness & Scale

- **Capacity** (Y1: 3M registered / 300K DAU / peak 15K concurrent WS): Redis GEO shards per city; radar fanout ≤1 message/2.5s/user; WS gateway sticky hash; match service HPA p95 80ms SLO; reaction counters Redis-primary with idempotent replay flush။
- **Degradation ladder**: ranking GPU down → rule-based fallback (feature-flagged); Redis GEO lag → PostGIS stale fallback; radar unavailable → static nearby list; push provider down → in-app queue။
- **DR**: multi-AZ ap-southeast-1 primary, PITR 15-min RPO / 1-h RTO, chat data region-pinned for privacy law။
- **Release**: event-schema contract tests; **guardrail regression suite (§3.3 exclusion paths)** per release; shadow-traffic model eval before promotion; kill-switch per AI/moderation feature။

---

## 12. Roadmap

| Phase | Theme | Scope | Exit criteria |
| --- | --- | --- | --- |
| 0 – Foundation | Trust plumbing | Auth, separate `dating_profiles` schema, Location v1 (Redis GEO+PostGIS+privacy radius), phone verification, nearby list | exact coords never leak (pentest pass); friend-exclusion CI pass |
| 1 – Core Dating | Match loop | Swipe/like, rule-match, chat WS, **Whisper Mode v1**, push, report/block | Yangon DAU 10K, match→chat rate ≥25% |
| 2 – City Community | Social density | **Township feed (posts/reactions/comments/shares §4)**, **Look Around bands**, **Radar Scan**, interest groups | 15% users join ≥1 community; community D30 retention +8pp; feed p95 <120ms |
| 3 – Intelligence | AI-enhanced | Two-tower ranking, **newsfeed ranking §4.5**, safety risk engine, scam classifier, icebreaker LLM | rank p95 <80ms; auto-block precision ≥90% |
| 4 – Scale | Multi-city & premium | Mandalay/BKK/CNX, Nexus+ subscription, promoted community events, host verification, admin dashboards | 3 cities live; subscription attach ≥6%; safety NPS ≥55 |

---

## 13. Risks & Mitigations

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Location-privacy incident | Brand fatal (Safety-first positioning) | Pentest per release; k-anonymity bands; bug bounty |
| Friend-graph leak into Dating | Trust fatal (real-world harm) | §3.3 exclusion + CI regression + cache-key audit; incident runbook |
| Whisper false-security expectation (screenshots) | Legal/brand | capture alert + safety log + first-use explainer copy |
| Community–dating brand bleed | Trust dilution | separate tab contexts; dating actions never auto-post to community |
| Reaction/feed gaming (engagement farms) | Feed quality collapse | velocity anomaly detection, reputation-weighted engagement |
| Offline event safety incident | Legal/brand risk | Host Tier3 verification, Meet Safe, delayed check-in publish |
| Regulation (data localization) | Launch block | per-city residency design from Phase 0 |

---

## 14. Technical Directives Summary (Developer Checklist)

| # | Directive | Implemented at |
| --- | --- | --- |
| 1 | DB schema separation — `user_profiles` vs `dating_profiles`; real name နှင့် dating nickname isolation | §3.1, §6.1 |
| 2 | Geospatial querying — PostGIS primary, Redis GEO hot path, Mongo `2dsphere` replica option; Radar & Look Around realtime | §2.5, §6.5 |
| 3 | Facebook-style reactions — enum {LIKE, LOVE, HAHA, WOW, SAD, ANGRY} + optimistic UI + idempotent server | §4.3 |
| 4 | Privacy & identity guardrail — "Hide from Friends" backend exclusion query in all recall paths; Radar/feed suppression | §3.3, §8 |

---

## 15. Naming & Branding Note

- **Nexus** = platform brand — "connection hub" meaning သည် dating + city social community dual positioning နှင့် တိုက်ရိုက်ကိုက်ညီ။
- **Chitchat (ချစ်-ချတ်)** = Chat/Dating module sub-brand candidate (module-level warmth, platform-level neutrality)။
- Store listing, trademark check, domain/handles: final decision Phase 1 မတိုင်မီ။

## 16. Conclusion — Direction Statement

Nexus ၏ architectural identity လေးခု:

1. **Single Location Engine** — Look Around (band-based), Radar (realtime push), community township scope အားလုံး coordinate-less distance abstraction ဖြင့် ချိတ်ဆက်။
2. **Safety as ranking signal + identity guardrail** — hard-gated TrustScore နှင့် backend-enforced friend-exclusion; risky user များ match pool သို့ ဝင်ခွင့်မရှိ။
3. **Dual-profile isolation by design** — social identity နှင့် dating identity သည် တစ်ကိုယ်တည်း person, တစ်စနစ်တည်း platform, သီးသန့် schemas — user control အပြည့်။
4. **AI at four fronts** — compatibility, fraud, moderation, newsfeed ranking — every final action carries a human appeal path。

Location-first သည် density ကို၊ Safety-first သည် trust ကို၊ AI-enhanced သည် quality ကို တာဝန်ယူပြီး — BeeTalk legacy UX (Radar / Look Around / Whisper Chat) နှင့် Facebook-style social loop ပေါင်းစပ်မှ City Social Dating Platform တကယ် ဖြစ်လာမည်။

---

*ဤစာတမ်းသည် "BeeTalk Location Structure System" document ကို foundation အဖြစ် ယူပြီး，Nexus အမည်ဖြင့် Hybrid Platform (Dating + City Social Community) အဖြစ် ဒီဇိုင်းဆွဲကာ Local Marketplace ဆိုင်ရာအား လုံးဝဖယ်ရှားထားသည်။ ဗားရှင်းလည်တွင် BeeTalk-Inspired Dating System (Look Around / Radar Scan / Whisper Mode Chat + Separate Dating Profile) နှင့် Facebook-Style Social Post Architecture၊ Developer Technical Directives ၄ ချက်ကို schema/UI/logic အထိ ပေါင်းစပ်ပြင်ဆင်ထားသည်။*
