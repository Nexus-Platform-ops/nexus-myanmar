package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// Raw location is accepted only on the authenticated ingestion write path.
// Never copy these request fields to response DTOs, telemetry or cache keys.
type locationInput struct {
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	Accuracy int     `json:"accuracy"`
	Town     string  `json:"township"`
	Nonce    string  `json:"nonce"`
}

func (s *Server) location(w http.ResponseWriter, r *http.Request) error {
	if err := s.mayWrite(r.Context(), who(r).ID); err != nil {
		return err
	}
	var b locationInput
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if math.IsNaN(b.Lat) || math.IsNaN(b.Lng) || b.Lat < -90 || b.Lat > 90 || b.Lng < -180 || b.Lng > 180 || b.Accuracy < 0 || b.Accuracy > 100 || !validTown(b.Town) || !validID(b.Nonce) {
		return fail(400, "invalid_location_fix")
	}
	nonce, err := s.Redis.SetNX(r.Context(), "nonce:"+who(r).ID+":"+b.Nonce, "1", time.Hour).Result()
	if err != nil {
		return err
	}
	if !nonce {
		return fail(409, "replayed_location")
	}
	ok, err := s.limit(r.Context(), "fix:"+who(r).ID, 1, 30*time.Second)
	if err != nil {
		return err
	}
	if !ok {
		return fail(429, "location_throttled")
	}
	raw, _ := json.Marshal([]float64{b.Lat, b.Lng})
	_, err = s.DB.Exec(r.Context(), `INSERT INTO private.user_locations(user_id,fix_cipher,coarse_geog,township,accuracy_m) VALUES($1,pgp_sym_encrypt($2,$3),ST_SnapToGrid(ST_SetSRID(ST_MakePoint($4,$5),4326),0.01)::geography,$6,$7) ON CONFLICT(user_id) DO UPDATE SET fix_cipher=excluded.fix_cipher,coarse_geog=excluded.coarse_geog,township=excluded.township,accuracy_m=excluded.accuracy_m,updated_at=now()`, who(r).ID, string(raw), s.Cfg.DataKey, b.Lng, b.Lat, b.Town, b.Accuracy)
	if err != nil {
		return err
	}
	write(w, 204, nil)
	return nil
}

// Shared gate is deliberately symmetric. Neither side can bypass a friend's hide setting.
// All dating read paths (cards, radar, reveal, swipes and existing match access) use it.
const datingGate = `a.phone_verified AND NOT a.restricted AND NOT a.ghost AND a.spoof_risk<=0.8
 AND me.phone_verified AND NOT me.restricted AND NOT me.ghost AND me.spoof_risk<=0.8
 AND a.id<>me.id AND NOT EXISTS(SELECT 1 FROM safety.blocks b WHERE (b.user_id=me.id AND b.target_id=a.id) OR (b.user_id=a.id AND b.target_id=me.id))
 AND NOT EXISTS(SELECT 1 FROM social.friendships f WHERE ((f.user_id=me.id AND f.friend_id=a.id) OR (f.friend_id=me.id AND f.user_id=a.id)) AND (d.hide_from_social_friends OR md.hide_from_social_friends))
 AND EXTRACT(YEAR FROM age(a.dob)) BETWEEN md.age_min AND md.age_max
 AND EXTRACT(YEAR FROM age(me.dob)) BETWEEN d.age_min AND d.age_max
 AND (cardinality(md.preferences)=0 OR d.gender=ANY(md.preferences))
 AND (cardinality(d.preferences)=0 OR md.gender=ANY(d.preferences))`

type candidate struct {
	ProfileID string   `json:"profile_id"`
	Nickname  string   `json:"nickname"`
	Age       int      `json:"age"`
	Bio       string   `json:"bio"`
	Prompts   []string `json:"prompts"`
	Interests []string `json:"interests"`
	Tier      int      `json:"tier"`
	Town      string   `json:"township"`
	Bucket    string   `json:"distance_bucket"`
	meters    float64
	user      string
}

func bucket(m float64, jitter float64) string {
	m *= 1 + jitter
	if m < 500 {
		return "<500m"
	}
	if m < 1000 {
		return "1km"
	}
	if m < 3000 {
		return "3km"
	}
	if m < 5000 {
		return "5km"
	}
	return "10km+"
}
func (s *Server) candidates(ctx context.Context, user string, radar bool) ([]candidate, error) {
	rows, err := s.DB.Query(ctx, `SELECT d.profile_id,d.nickname,EXTRACT(YEAR FROM age(a.dob))::int,d.bio,d.prompts,d.interests,a.tier,p.township,a.id,ST_Distance(l.coarse_geog,ml.coarse_geog) FROM dating.dating_profiles d JOIN identity.accounts a ON a.id=d.user_id JOIN social.user_profiles p ON p.user_id=a.id JOIN private.user_locations l ON l.user_id=a.id CROSS JOIN identity.accounts me JOIN dating.dating_profiles md ON md.user_id=me.id JOIN private.user_locations ml ON ml.user_id=me.id WHERE me.id=$1 AND `+datingGate+` AND d.radar_until>now() AND l.updated_at>now()-interval '15 minutes' AND ml.updated_at>now()-interval '15 minutes' AND ST_DWithin(l.coarse_geog,ml.coarse_geog,LEAST(md.radius_km,d.radius_km)*1000) ORDER BY ST_Distance(l.coarse_geog,ml.coarse_geog),d.profile_id LIMIT 200`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []candidate{}
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.ProfileID, &c.Nickname, &c.Age, &c.Bio, &c.Prompts, &c.Interests, &c.Tier, &c.Town, &c.user, &c.meters); err != nil {
			return nil, err
		}
		h := keyed(s.Cfg.DataKey, user+":"+c.user+":"+time.Now().UTC().Format("2006-01-02"))
		n, _ := strconv.ParseUint(h[:4], 16, 16)
		c.Bucket = bucket(c.meters, (float64(n)/65535.0-0.5)*0.2)
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Server) canSeeDating(ctx context.Context, user, target string) (bool, error) {
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM dating.dating_profiles d JOIN identity.accounts a ON a.id=d.user_id CROSS JOIN identity.accounts me JOIN dating.dating_profiles md ON md.user_id=me.id WHERE me.id=$1 AND a.id=$2 AND `+datingGate+`)`, user, target).Scan(&ok)
	return ok, err
}
func (s *Server) discover(w http.ResponseWriter, r *http.Request) error { return s.nearby(w, r, false) }
func (s *Server) lookaround(w http.ResponseWriter, r *http.Request) error {
	return s.nearby(w, r, false)
}
func (s *Server) nearby(w http.ResponseWriter, r *http.Request, radar bool) error {
	cs, err := s.candidates(r.Context(), who(r).ID, radar)
	if err != nil {
		return err
	}
	band := 500
	if v := r.URL.Query().Get("band"); v != "" {
		switch v {
		case "500m":
			band = 500
		case "1km":
			band = 1000
		case "3km":
			band = 3000
		case "5km":
			band = 5000
		case "10km":
			band = 10000
		default:
			return fail(400, "invalid_band")
		}
	}
	if town := r.URL.Query().Get("township"); town != "" {
		filtered := []candidate{}
		for _, c := range cs {
			if c.Town == town {
				filtered = append(filtered, c)
			}
		}
		cs = filtered
	}
	filtered := []candidate{}
	for _, b := range []int{500, 1000, 3000, 5000, 10000} {
		if b < band {
			continue
		}
		filtered = []candidate{}
		for _, c := range cs {
			if c.meters <= float64(b) {
				filtered = append(filtered, c)
			}
		}
		band = b
		if len(filtered) >= 5 {
			break
		}
	}
	// No individual exposure below k, including swipe/discover. This favors privacy over density.
	if len(filtered) < 5 {
		write(w, 200, map[string]any{"items": []candidate{}, "effective_band": band, "quiet": true, "next_cursor": nil})
		return nil
	}
	if len(filtered) > 20 {
		filtered = filtered[:20]
	}
	visible := []candidate{}
	for _, c := range filtered {
		ok, err := s.limit(r.Context(), "probe:"+who(r).ID+":"+c.user, 5, time.Hour)
		if err != nil {
			return err
		}
		if ok {
			visible = append(visible, c)
		}
	}
	// Do not expose a sub-k remainder after the probe budget filters.
	if len(visible) < 5 {
		visible = []candidate{}
	}
	write(w, 200, map[string]any{"items": visible, "effective_band": band, "quiet": len(visible) == 0, "next_cursor": nil})
	return nil
}
func (s *Server) radar(w http.ResponseWriter, r *http.Request) error {
	cs, err := s.candidates(r.Context(), who(r).ID, true)
	if err != nil {
		return err
	}
	out := []map[string]any{}
	if len(cs) >= 5 {
		if len(cs) > 20 {
			cs = cs[:20]
		}
		for _, c := range cs {
			token := randomToken()
			if err = s.Redis.Set(r.Context(), "blip:"+who(r).ID+":"+token, c.ProfileID, 15*time.Minute).Err(); err != nil {
				return err
			}
			h := keyed(s.Cfg.DataKey, who(r).ID+":"+c.ProfileID+":"+time.Now().UTC().Format("2006-01-02"))
			n, _ := strconv.ParseUint(h[:4], 16, 16)
			out = append(out, map[string]any{"blip_id": token, "distance_bucket": c.Bucket, "display_angle": int(n) % 360, "display_ring": 1 + int(n)%3})
		}
	}
	write(w, 200, map[string]any{"items": out, "quiet": len(out) == 0, "expires_in": 900})
	return nil
}
func (s *Server) blip(w http.ResponseWriter, r *http.Request) error {
	token := r.PathValue("id")
	if len(token) != 64 {
		return fail(404, "blip_expired")
	}
	id, err := s.Redis.Get(r.Context(), "blip:"+who(r).ID+":"+token).Result()
	if err != nil {
		return fail(404, "blip_expired")
	}
	cs, err := s.candidates(r.Context(), who(r).ID, true)
	if err != nil {
		return err
	}
	if len(cs) < 5 {
		return fail(404, "blip_expired")
	}
	for _, c := range cs {
		if c.ProfileID == id {
			ok, err := s.limit(r.Context(), "probe:"+who(r).ID+":"+c.user, 5, time.Hour)
			if err != nil {
				return err
			}
			if !ok {
				return fail(429, "probe_limit")
			}
			write(w, 200, c)
			return nil
		}
	}
	return fail(404, "blip_expired")
}
func (s *Server) swipe(w http.ResponseWriter, r *http.Request) error {
	if err := s.mayWrite(r.Context(), who(r).ID); err != nil {
		return err
	}
	var b struct {
		Profile string `json:"profile_id"`
		Intent  string `json:"intent"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if !validID(b.Profile) || !contains([]string{"LIKE", "PASS"}, b.Intent) {
		return fail(400, "invalid_swipe")
	}
	cs, err := s.candidates(r.Context(), who(r).ID, false)
	if err != nil {
		return err
	}
	target := ""
	if len(cs) >= 5 {
		for _, c := range cs {
			if c.ProfileID == b.Profile {
				target = c.user
			}
		}
	}
	if target == "" {
		return fail(404, "profile_unavailable")
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	pair := []string{who(r).ID, target}
	sort.Strings(pair)
	key := pair[0] + ":" + pair[1]
	// Serialize reciprocal swipes; without this lock concurrent likes can miss a match.
	if _, err = tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", key); err != nil {
		return err
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO dating.swipes(user_id,target_id,intent) VALUES($1,$2,$3) ON CONFLICT(user_id,target_id) DO UPDATE SET intent=excluded.intent`, who(r).ID, target, b.Intent)
	if err != nil {
		return err
	}
	var matched bool
	err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM dating.swipes WHERE user_id=$1 AND target_id=$2 AND intent='LIKE') AND $3='LIKE'`, target, who(r).ID, b.Intent).Scan(&matched)
	if err != nil {
		return err
	}
	var room string
	if matched {
		err = tx.QueryRow(r.Context(), `INSERT INTO chat.rooms(kind,pair_key) VALUES('MATCH',$1) ON CONFLICT(pair_key) DO UPDATE SET pair_key=excluded.pair_key RETURNING id`, key).Scan(&room)
		if err != nil {
			return err
		}
		for _, u := range pair {
			if _, err = tx.Exec(r.Context(), `INSERT INTO chat.members(room_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, room, u); err != nil {
				return err
			}
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	write(w, 201, map[string]any{"matched": matched, "room_id": room})
	return nil
}

// Stable opaque cursors are validated, never interpolated into SQL.
type pageCursor struct {
	Time time.Time `json:"time"`
	ID   string    `json:"id"`
}

func parseCursor(raw string) (pageCursor, error) {
	if raw == "" {
		return pageCursor{time.Now().Add(time.Second), "ffffffff-ffff-ffff-ffff-ffffffffffff"}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return pageCursor{}, fail(400, "invalid_cursor")
	}
	var c pageCursor
	if json.Unmarshal(b, &c) != nil || !validID(c.ID) || c.Time.IsZero() {
		return c, fail(400, "invalid_cursor")
	}
	return c, nil
}
func cursor(t time.Time, id string) string {
	b, _ := json.Marshal(pageCursor{t, id})
	return base64.RawURLEncoding.EncodeToString(b)
}

var _ = pgx.ErrNoRows
