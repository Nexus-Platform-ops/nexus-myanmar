package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAdultBoundary(t *testing.T) {
	now := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	for _, v := range []struct {
		s    string
		want bool
	}{{"2008-11-05", true}, {"2008-11-06", false}, {"2010-01-01", false}, {"1890-01-01", false}, {"1999-05-01", true}} {
		d, _ := time.Parse("2006-01-02", v.s)
		if adult(d, now) != v.want {
			t.Errorf("age gate %s", v.s)
		}
	}
}
func TestDistanceBuckets(t *testing.T) {
	for _, v := range []struct {
		n float64
		s string
	}{{0, "<500m"}, {499, "<500m"}, {500, "1km"}, {999, "1km"}, {1000, "3km"}, {2999, "3km"}, {3000, "5km"}, {5000, "10km+"}} {
		if bucket(v.n, 0) != v.s {
			t.Errorf("bucket %v", v.n)
		}
	}
}
func TestCursorRoundTrip(t *testing.T) {
	id := uuid.NewString()
	now := time.Now().UTC()
	c, e := parseCursor(cursor(now, id))
	if e != nil || c.ID != id || !c.Time.Equal(now) {
		t.Fatal("cursor round trip")
	}
	for _, s := range []string{"!", "eyJpZCI6IngiLCJ0aW1lIjoiIn0"} {
		if _, e = parseCursor(s); e == nil {
			t.Fatal("invalid cursor accepted")
		}
	}
}
func TestConfigurationFailClosed(t *testing.T) {
	c := Config{Env: "test", DatabaseURL: "postgres://localhost/nexus_test", JWTSecret: strings.Repeat("a", 32), DataKey: strings.Repeat("b", 32), OTPSecret: strings.Repeat("c", 32)}
	if c.Validate() != nil {
		t.Fatal("valid test config rejected")
	}
	c.Env = "production"
	if c.Validate() == nil {
		t.Fatal("unintegrated production accepted")
	}
	c.Env = "test"
	c.DataKey = c.JWTSecret
	if c.Validate() == nil {
		t.Fatal("reused secrets")
	}
}
func TestPhoneValidation(t *testing.T) {
	for _, v := range []string{"+959123456789", "+66812345678"} {
		if !phonePattern.MatchString(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"091234", "+0123456789", "' OR 1=1", "+95\n"} {
		if phonePattern.MatchString(v) {
			t.Fatal(v)
		}
	}
}
func TestDecodeStrict(t *testing.T) {
	for _, body := range []string{`{"x":1}`, `{} {}`, `null trailing`} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		w := httptest.NewRecorder()
		var b struct {
			Value string `json:"value"`
		}
		if decode(w, r, &b) == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

type fixture struct {
	s                       *Server
	ts                      *httptest.Server
	users, profiles, tokens []string
	t                       *testing.T
	boot                    *pgxpool.Pool
}

func (f *fixture) truncate(tables string) {
	f.t.Helper()
	if _, err := f.boot.Exec(context.Background(), "TRUNCATE "+tables); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) req(i int, method, path string, body any) (int, DataMap) {
	f.t.Helper()
	var reader io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			f.t.Fatal(e)
		}
		reader = bytes.NewReader(b)
	}
	r, e := http.NewRequest(method, f.ts.URL+path, reader)
	if e != nil {
		f.t.Fatal(e)
	}
	if i >= 0 {
		r.Header.Set("Authorization", "Bearer "+f.tokens[i])
	}
	r.Header.Set("Content-Type", "application/json")
	res, e := http.DefaultClient.Do(r)
	if e != nil {
		f.t.Fatal(e)
	}
	defer res.Body.Close()
	out := DataMap{}
	raw, _ := io.ReadAll(res.Body)
	if len(raw) > 0 {
		if e = json.Unmarshal(raw, &out); e != nil {
			f.t.Fatalf("non-json response %d: %s", res.StatusCode, raw)
		}
	}
	return res.StatusCode, out
}

type DataMap map[string]any

func (f *fixture) expect(i int, method, path string, body any, want int) DataMap {
	f.t.Helper()
	status, out := f.req(i, method, path, body)
	if status != want {
		f.t.Fatalf("%s %s: got %d want %d: %v", method, path, status, want, out)
	}
	return out
}
func (f *fixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.s.DB.Exec(context.Background(), q, args...); err != nil {
		f.t.Fatal(err)
	}
}
func noSecret(t *testing.T, out any) {
	t.Helper()
	b, _ := json.Marshal(out)
	for _, field := range []string{`"lat"`, `"lng"`, `"latitude"`, `"longitude"`, `"real_name"`, `"user_id"`, `"phone"`, `fix_cipher`, `coarse_geog`, `Social Person`} {
		if strings.Contains(string(b), field) {
			t.Fatalf("private field leaked: %s", field)
		}
	}
}
func TestIntegration(t *testing.T) {
	if os.Getenv("NEXUS_INTEGRATION") != "1" {
		t.Skip("requires isolated nexus_test database and Redis DB 15")
	}
	db := os.Getenv("TEST_DATABASE_URL")
	if db == "" {
		db = "postgres://nexus:nexus_local_only@localhost:5432/nexus_test?sslmode=disable"
	}
	if !strings.Contains(db, "/nexus_test?") {
		t.Fatal("refusing destructive fixtures outside nexus_test")
	}
	// Fixtures use the bootstrap role; the server under test must run as the
	// least-privilege app role so integration proves the production grant shape.
	appDB := os.Getenv("TEST_APP_DATABASE_URL")
	if appDB == "" {
		appDB = "postgres://nexus_app:nexus_app_ci_only_pw@localhost:5432/nexus_test?sslmode=disable"
	}
	if !strings.Contains(appDB, "/nexus_test?") {
		t.Fatal("refusing app role outside nexus_test")
	}
	cfg := Config{Env: "test", Addr: ":0", DatabaseURL: appDB, RedisURL: "redis://localhost:6379/15", JWTSecret: strings.Repeat("a", 40), DataKey: strings.Repeat("b", 40), OTPSecret: strings.Repeat("c", 40), Origin: "http://localhost:3000"}
	s, e := New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	boot, e := pgxpool.New(context.Background(), db)
	if e != nil {
		t.Fatal(e)
	}
	defer boot.Close()
	f := &fixture{s: s, t: t, boot: boot}
	f.ts = httptest.NewServer(s.Handler())
	defer f.ts.Close()
	f.truncate("identity.accounts CASCADE")
	f.truncate("identity.otp,private.outbox")
	if e = s.Redis.FlushDB(context.Background()).Err(); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 9; i++ {
		id, pid, sid := uuid.NewString(), uuid.NewString(), uuid.NewString()
		f.exec(`INSERT INTO identity.accounts(id,phone_hash,phone_cipher,dob,phone_verified,tier) VALUES($1,$2,pgp_sym_encrypt('private',$3),'1997-01-01',true,1)`, id, fmt.Sprintf("fixture%d", i), cfg.DataKey)
		f.exec(`INSERT INTO social.user_profiles(user_id,real_name,township,bio) VALUES($1,$2,'kamayut','Social bio')`, id, fmt.Sprintf("Social Person %d", i))
		f.exec(`INSERT INTO dating.dating_profiles(profile_id,user_id,nickname,radar_until) VALUES($1,$2,$3,now()+interval '15 minutes')`, pid, id, fmt.Sprintf("Nickname%d", i))
		f.exec(`INSERT INTO private.user_locations(user_id,fix_cipher,coarse_geog,township,accuracy_m) VALUES($1,pgp_sym_encrypt('[16.82,96.13]',$2),ST_SetSRID(ST_MakePoint(96.13,16.82),4326)::geography,'kamayut',10)`, id, cfg.DataKey)
		f.exec(`INSERT INTO identity.sessions(id,user_id,refresh_hash,device_label,expires_at) VALUES($1,$2,$3,'fixture',now()+interval '1 day')`, sid, id, randomToken())
		token, e := s.tokens(id, sid)
		if e != nil {
			t.Fatal(e)
		}
		f.users = append(f.users, id)
		f.profiles = append(f.profiles, pid)
		f.tokens = append(f.tokens, token)
	}
	run := func(name string, fn func(*testing.T)) { t.Run(name, func(t *testing.T) { f.t = t; fn(t) }); f.t = t }
	run("health", func(t *testing.T) { f.expect(-1, "GET", "/health/ready", nil, 200) })
	run("auth_required", func(t *testing.T) { f.expect(-1, "GET", "/v1/feed", nil, 401) })
	run("tampered_jwt", func(t *testing.T) {
		old := f.tokens[0]
		f.tokens[0] = old + "bad"
		f.expect(0, "GET", "/v1/feed", nil, 401)
		f.tokens[0] = old
	})
	run("social_dating_isolation", func(t *testing.T) {
		out := f.expect(0, "GET", "/v1/dating-profile/preview", nil, 200)
		noSecret(t, out)
		out = f.expect(0, "GET", "/v1/search?q=Nickname&type=people", nil, 200)
		if len(out["items"].([]any)) != 0 {
			t.Fatal("dating nickname searchable")
		}
		out = f.expect(0, "GET", "/v1/search?q=Social&type=people", nil, 200)
		if len(out["items"].([]any)) < 1 {
			t.Fatal("social search failed")
		}
	})
	run("nearby_no_exact_coordinates", func(t *testing.T) {
		out := f.expect(0, "GET", "/v1/lookaround?band=500m", nil, 200)
		noSecret(t, out)
		if len(out["items"].([]any)) != 8 {
			t.Fatal(out)
		}
	})
	run("symmetric_friend_exclusion", func(t *testing.T) {
		f.exec(`INSERT INTO social.friendships(user_id,friend_id) VALUES($1,$2)`, f.users[1], f.users[0])
		out := f.expect(0, "GET", "/v1/discover", nil, 200)
		raw, _ := json.Marshal(out)
		if strings.Contains(string(raw), f.profiles[1]) {
			t.Fatal("friend leak")
		}
		f.exec("DELETE FROM social.friendships")
	})
	run("risk_gate", func(t *testing.T) {
		f.exec("UPDATE identity.accounts SET spoof_risk=.81 WHERE id=$1", f.users[2])
		out := f.expect(0, "GET", "/v1/discover", nil, 200)
		raw, _ := json.Marshal(out)
		if strings.Contains(string(raw), f.profiles[2]) {
			t.Fatal("risk leak")
		}
		f.exec("UPDATE identity.accounts SET spoof_risk=0 WHERE id=$1", f.users[2])
	})
	run("k_anonymity", func(t *testing.T) {
		f.exec("UPDATE dating.dating_profiles SET radar_until=NULL WHERE user_id<>$1", f.users[0])
		out := f.expect(0, "GET", "/v1/lookaround", nil, 200)
		if len(out["items"].([]any)) != 0 {
			t.Fatal("sub k exposure")
		}
		f.exec("UPDATE dating.dating_profiles SET radar_until=now()+interval '15 minutes'")
	})
	run("anonymous_radar_reveal", func(t *testing.T) {
		out := f.expect(3, "GET", "/v1/radar", nil, 200)
		noSecret(t, out)
		raw, _ := json.Marshal(out)
		if strings.Contains(string(raw), "nickname") {
			t.Fatal("radar identity exposed")
		}
		list := out["items"].([]any)
		if len(list) == 0 {
			t.Fatal("no blips")
		}
		id := list[0].(map[string]any)["blip_id"].(string)
		noSecret(t, f.expect(3, "GET", "/v1/radar/blips/"+id, nil, 200))
		f.expect(4, "GET", "/v1/radar/blips/"+id, nil, 404)
	})
	run("raw_location_write_only_and_nonce", func(t *testing.T) {
		b := DataMap{"lat": 16.82, "lng": 96.13, "accuracy": 10, "township": "kamayut", "nonce": uuid.NewString()}
		f.expect(4, "POST", "/v1/location", b, 204)
		f.expect(4, "POST", "/v1/location", b, 409)
		b["accuracy"] = 101
		b["nonce"] = uuid.NewString()
		f.expect(4, "POST", "/v1/location", b, 400)
	})
	run("strict_unknown_fields", func(t *testing.T) {
		f.expect(0, "PUT", "/v1/profile", DataMap{"real_name": "x", "township": "kamayut", "tier": 3}, 400)
		f.expect(0, "GET", "/v1/feed?cursor=bad", nil, 400)
		f.expect(0, "GET", "/v1/lookaround?band=50m", nil, 400)
	})
	var post string
	run("post_review_and_visibility", func(t *testing.T) {
		out := f.expect(0, "POST", "/v1/posts", DataMap{"body": "Public test", "audience": "PUBLIC", "township": "kamayut"}, 202)
		post = out["id"].(string)
		d := f.expect(1, "GET", "/v1/feed", nil, 200)
		if len(d["items"].([]any)) != 0 {
			t.Fatal("unreviewed post leaked")
		}
		f.exec("UPDATE social.posts SET status='LIVE' WHERE id=$1", post)
		d = f.expect(1, "GET", "/v1/feed", nil, 200)
		if len(d["items"].([]any)) != 1 {
			t.Fatal(d)
		}
	})
	run("reaction_replace_idempotent", func(t *testing.T) {
		for _, reaction := range []string{"LIKE", "LIKE", "LOVE"} {
			f.expect(1, "POST", "/v1/posts/"+post+"/reactions", DataMap{"type": reaction}, 200)
		}
		var count int
		var kind string
		s.DB.QueryRow(context.Background(), "SELECT count(*),min(type) FROM social.reactions WHERE post_id=$1", post).Scan(&count, &kind)
		if count != 1 || kind != "LOVE" {
			t.Fatal(count, kind)
		}
		f.expect(1, "POST", "/v1/posts/"+post+"/reactions", DataMap{"type": "bad"}, 400)
	})
	run("friends_post_idor", func(t *testing.T) {
		p := f.expect(0, "POST", "/v1/posts", DataMap{"body": "Private", "audience": "FRIENDS", "township": "kamayut"}, 202)["id"].(string)
		f.exec("UPDATE social.posts SET status='LIVE' WHERE id=$1", p)
		f.expect(1, "POST", "/v1/posts/"+p+"/reactions", DataMap{"type": "LIKE"}, 404)
		f.expect(1, "GET", "/v1/posts/"+p+"/comments", nil, 404)
		f.expect(1, "POST", "/v1/posts/"+p+"/shares", DataMap{"mode": "COPY_LINK"}, 404)
	})
	run("comment_two_levels_moderation", func(t *testing.T) {
		root := f.expect(1, "POST", "/v1/posts/"+post+"/comments", DataMap{"body": "Root"}, 202)["id"].(string)
		f.exec("UPDATE social.comments SET status='LIVE' WHERE id=$1", root)
		reply := f.expect(2, "POST", "/v1/posts/"+post+"/comments", DataMap{"body": "Reply", "parent_comment_id": root}, 202)["id"].(string)
		f.exec("UPDATE social.comments SET status='LIVE' WHERE id=$1", reply)
		f.expect(3, "POST", "/v1/posts/"+post+"/comments", DataMap{"body": "Too deep", "parent_comment_id": reply}, 400)
	})
	run("tier0_read_only", func(t *testing.T) {
		f.exec("UPDATE identity.accounts SET phone_verified=false WHERE id=$1", f.users[7])
		f.expect(7, "POST", "/v1/posts", DataMap{"body": "No", "audience": "PUBLIC", "township": "kamayut"}, 403)
		f.exec("UPDATE identity.accounts SET phone_verified=true WHERE id=$1", f.users[7])
	})
	run("tier3_event_gate", func(t *testing.T) {
		f.expect(1, "POST", "/v1/events", DataMap{}, 403)
		f.exec("UPDATE identity.accounts SET tier=3 WHERE id=$1", f.users[1])
		out := f.expect(1, "POST", "/v1/events", DataMap{"title": "Coffee", "description": "Meet safely", "township": "kamayut", "venue_area": "Kamayut public area", "starts_at": time.Now().Add(time.Hour), "ends_at": time.Now().Add(2 * time.Hour)}, 202)
		f.exec("UPDATE social.events SET status='LIVE' WHERE id=$1", out["id"])
		f.expect(2, "POST", "/v1/events/"+out["id"].(string)+"/rsvp", DataMap{"status": "ATTEND"}, 200)
		noSecret(t, f.expect(2, "GET", "/v1/events", nil, 200))
	})
	var room string
	run("mutual_match_and_whisper", func(t *testing.T) {
		f.expect(5, "POST", "/v1/actions/swipe", DataMap{"profile_id": f.profiles[6], "intent": "LIKE"}, 201)
		out := f.expect(6, "POST", "/v1/actions/swipe", DataMap{"profile_id": f.profiles[5], "intent": "LIKE"}, 201)
		if out["matched"] != true {
			t.Fatal(out)
		}
		room = out["room_id"].(string)
		f.expect(2, "GET", "/v1/chats/"+room+"/messages", nil, 404)
		cid := uuid.NewString()
		msg := f.expect(5, "POST", "/v1/chats/"+room+"/messages", DataMap{"text": "secret text", "mode": "WHISPER", "client_id": cid}, 201)
		f.expect(5, "POST", "/v1/chats/"+room+"/messages", DataMap{"text": "secret text", "mode": "WHISPER", "client_id": cid}, 200)
		start := time.Now()
		wipe := f.expect(6, "POST", "/v1/chats/"+room+"/read", DataMap{"through_seq": msg["seq"]}, 200)
		if wipe["wiped"] != float64(1) || time.Since(start) > 2*time.Second {
			t.Fatal("wipe failed or slow")
		}
		out = f.expect(6, "GET", "/v1/chats/"+room+"/messages", nil, 200)
		if len(out["items"].([]any)) != 0 {
			t.Fatal("whisper remains")
		}
		dup := f.expect(5, "POST", "/v1/chats/"+room+"/messages", DataMap{"text": "secret text", "mode": "WHISPER", "client_id": cid}, 200)
		if dup["expired"] != true {
			t.Fatal("deleted whisper resurrected")
		}
	})
	run("capture_both_sides", func(t *testing.T) {
		f.expect(5, "POST", "/v1/safety/capture-alert", DataMap{"room_id": room, "kind": "SCREENSHOT"}, 202)
		for _, i := range []int{5, 6} {
			out := f.expect(i, "GET", "/v1/notifications?category=SAFETY", nil, 200)
			if len(out["items"].([]any)) < 1 {
				t.Fatal("capture notice missing")
			}
		}
	})
	run("match_reader_rechecks_friend_gate", func(t *testing.T) {
		f.exec("INSERT INTO social.friendships(user_id,friend_id) VALUES($1,$2)", f.users[5], f.users[6])
		f.expect(5, "GET", "/v1/chats/"+room+"/messages", nil, 404)
		f.exec("DELETE FROM social.friendships")
	})
	run("websocket_single_use_ticket", func(t *testing.T) {
		d := f.expect(0, "POST", "/v1/realtime/ticket", DataMap{}, 201)
		url := "ws" + strings.TrimPrefix(f.ts.URL, "http") + "/v1/ws?ticket=" + d["ticket"].(string)
		c, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		_, res, err := websocket.DefaultDialer.Dial(url, nil)
		if err == nil || res.StatusCode != 401 {
			t.Fatal("ticket reused")
		}
	})
	run("concurrent_reaction_unique", func(t *testing.T) {
		var wg sync.WaitGroup
		for n := 0; n < 6; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				f.expect(2, "POST", "/v1/posts/"+post+"/reactions", DataMap{"type": "WOW"}, 200)
			}()
		}
		wg.Wait()
		var count int
		s.DB.QueryRow(context.Background(), "SELECT count(*) FROM social.reactions WHERE post_id=$1 AND user_id=$2", post, f.users[2]).Scan(&count)
		if count != 1 {
			t.Fatal("duplicate reaction")
		}
	})
	run("block_every_dating_read_path", func(t *testing.T) {
		f.expect(0, "POST", "/v1/safety/block", DataMap{"target_id": f.users[1]}, 200)
		out := f.expect(1, "GET", "/v1/discover", nil, 200)
		raw, _ := json.Marshal(out)
		if strings.Contains(string(raw), f.profiles[0]) {
			t.Fatal("block leaked")
		}
		f.expect(0, "DELETE", "/v1/safety/blocks/"+f.users[1], nil, 204)
	})
	run("report_high_lock", func(t *testing.T) {
		out := f.expect(7, "POST", "/v1/safety/report", DataMap{"target_id": f.users[8], "category": "VIOLENCE", "details": "Threat"}, 202)
		if out["response_due_minutes"] != float64(30) {
			t.Fatal(out)
		}
		f.expect(8, "POST", "/v1/posts", DataMap{"body": "No", "audience": "PUBLIC", "township": "kamayut"}, 403)
		// The report hides the pair and HIGH restricts the target; later subtests
		// reuse these users, so restore both here.
		f.exec("DELETE FROM safety.blocks")
		f.exec("UPDATE identity.accounts SET restricted=false WHERE id=$1", f.users[8])
	})
	run("ghost_hides_and_locks", func(t *testing.T) {
		f.expect(6, "POST", "/v1/safety/ghost", DataMap{"enabled": true}, 200)
		f.expect(6, "POST", "/v1/chats/"+room+"/messages", DataMap{"text": "No", "mode": "NORMAL", "client_id": uuid.NewString()}, 403)
		f.expect(6, "POST", "/v1/safety/ghost", DataMap{"enabled": false}, 200)
	})
	run("export_own_only", func(t *testing.T) {
		out := f.expect(0, "GET", "/v1/account/export", nil, 200)
		raw, _ := json.Marshal(out)
		if strings.Contains(string(raw), "Social Person 1") || strings.Contains(string(raw), "phone_cipher") || strings.Contains(string(raw), "coarse_geog") {
			t.Fatal("export leak")
		}
	})
	run("integrations_fail_closed", func(t *testing.T) {
		for _, path := range []string{"/v1/media/upload", "/v1/calls", "/v1/verification", "/v1/nexus-plus"} {
			f.expect(0, "POST", path, DataMap{}, 503)
		}
	})
	run("otp_refresh_rotation_age_gate", func(t *testing.T) {
		ch := f.expect(-1, "POST", "/v1/auth/otp/request", DataMap{"phone": "+959123456789"}, 202)
		b := DataMap{"challenge_id": ch["challenge_id"], "code": ch["development_code"], "dob": "2015-01-01", "device": "test"}
		f.expect(-1, "POST", "/v1/auth/otp/verify", b, 400)
		b["dob"] = "1995-01-01"
		out := f.expect(-1, "POST", "/v1/auth/otp/verify", b, 200)
		f.expect(-1, "POST", "/v1/auth/otp/verify", b, 401)
		refresh := out["refresh_token"].(string)
		f.expect(-1, "POST", "/v1/auth/refresh", DataMap{"refresh_token": refresh}, 200)
		f.expect(-1, "POST", "/v1/auth/refresh", DataMap{"refresh_token": refresh}, 401)
		f.expect(-1, "POST", "/v1/auth/otp/request", DataMap{"phone": "+959123456789"}, 429)
	})

	run("profile_updates_validate_and_isolate", func(t *testing.T) {
		f.expect(0, "PUT", "/v1/profile", DataMap{"real_name": "Social Person 0", "bio": "Updated bio", "township": "kamayut"}, 200)
		b := DataMap{"nickname": "SeparateNick", "bio": "Dating bio", "photos": []string{}, "prompts": []string{"A walk"}, "interests": []string{"Music"}, "gender": "unspecified", "preferences": []string{}, "age_min": 18, "age_max": 99, "hide_from_social_friends": true, "radar_opt_in": true, "radius_km": 5}
		noSecret(t, f.expect(0, "PUT", "/v1/dating-profile", b, 200))
		b["nickname"] = "Social Person 0"
		f.expect(0, "PUT", "/v1/dating-profile", b, 400)
		b["nickname"] = "SafeNick"
		b["photos"] = []string{"https://untrusted.example/photo.jpg"}
		f.expect(0, "PUT", "/v1/dating-profile", b, 400)
	})
	run("groups_create_join_list", func(t *testing.T) {
		d := f.expect(0, "POST", "/v1/groups", DataMap{"name": "Coffee group", "township": "kamayut", "description": "A local interest group"}, 201)
		f.expect(2, "POST", "/v1/groups/"+d["id"].(string)+"/join", DataMap{}, 200)
		d = f.expect(2, "GET", "/v1/groups", nil, 200)
		if len(d["items"].([]any)) != 1 {
			t.Fatal(d)
		}
	})
	run("bookmark_and_share_modes", func(t *testing.T) {
		f.expect(1, "POST", "/v1/posts/"+post+"/bookmark", DataMap{"saved": true}, 200)
		f.expect(1, "POST", "/v1/posts/"+post+"/bookmark", DataMap{"saved": false}, 200)
		f.expect(1, "POST", "/v1/posts/"+post+"/shares", DataMap{"mode": "COPY_LINK"}, 201)
		f.expect(1, "POST", "/v1/posts/"+post+"/shares", DataMap{"mode": "RESHARE"}, 201)
		f.expect(1, "POST", "/v1/posts/"+post+"/shares", DataMap{"mode": "SEND_DM", "room_id": room}, 404)
	})
	run("social_chat_authorization_and_normal_history", func(t *testing.T) {
		f.expect(1, "POST", "/v1/chats", DataMap{"members": []string{f.users[2]}}, 403)
		f.exec("INSERT INTO social.friendships(user_id,friend_id) VALUES($1,$2)", f.users[1], f.users[2])
		d := f.expect(1, "POST", "/v1/chats", DataMap{"members": []string{f.users[2]}}, 201)
		rid := d["id"].(string)
		msg := f.expect(1, "POST", "/v1/chats/"+rid+"/messages", DataMap{"text": "Normal history", "mode": "NORMAL", "client_id": uuid.NewString()}, 201)
		var cipher []byte
		if e := f.boot.QueryRow(context.Background(), "SELECT body_cipher FROM chat.messages WHERE id=$1", msg["id"]).Scan(&cipher); e != nil {
			t.Fatal(e)
		}
		if bytes.Contains(cipher, []byte("Normal history")) {
			t.Fatal("plaintext chat body stored at rest")
		}
		var plain string
		if e := f.boot.QueryRow(context.Background(), "SELECT pgp_sym_decrypt(body_cipher,$2) FROM chat.messages WHERE id=$1", msg["id"], cfg.DataKey).Scan(&plain); e != nil || plain != "Normal history" {
			t.Fatalf("cipher round-trip: %q %v", plain, e)
		}
		d = f.expect(2, "POST", "/v1/chats/"+rid+"/read", DataMap{"through_seq": msg["seq"]}, 200)
		if d["wiped"] != float64(0) {
			t.Fatal("normal message deleted")
		}
		d = f.expect(2, "GET", "/v1/chats/"+rid+"/messages", nil, 200)
		if len(d["items"].([]any)) != 1 {
			t.Fatal(d)
		}
		d = f.expect(2, "GET", "/v1/chats", nil, 200)
		if len(d["items"].([]any)) < 1 {
			t.Fatal(d)
		}
		f.exec("DELETE FROM social.friendships")
	})
	run("group_whisper_per_recipient", func(t *testing.T) {
		// 3-member social room: one reader must never wipe the whisper for others.
		f.exec("INSERT INTO social.friendships(user_id,friend_id) VALUES($1,$2),($1,$3)", f.users[7], f.users[8], f.users[0])
		d := f.expect(7, "POST", "/v1/chats", DataMap{"members": []string{f.users[8], f.users[0]}}, 201)
		rid := d["id"].(string)
		cid := uuid.NewString()
		msg := f.expect(8, "POST", "/v1/chats/"+rid+"/messages", DataMap{"text": "group secret", "mode": "WHISPER", "client_id": cid}, 201)
		// First reader: consumed for them, still live for sender and the third member.
		d = f.expect(0, "POST", "/v1/chats/"+rid+"/read", DataMap{"through_seq": msg["seq"]}, 200)
		if d["marked_read"] != float64(1) || d["wiped"] != float64(0) {
			t.Fatalf("first reader must not trigger physical wipe: %v", d)
		}
		if n := len(f.expect(0, "GET", "/v1/chats/"+rid+"/messages", nil, 200)["items"].([]any)); n != 0 {
			t.Fatal("whisper still visible to first reader")
		}
		if n := len(f.expect(8, "GET", "/v1/chats/"+rid+"/messages", nil, 200)["items"].([]any)); n != 1 {
			t.Fatal("whisper wrongly wiped for sender after first read")
		}
		if n := len(f.expect(7, "GET", "/v1/chats/"+rid+"/messages", nil, 200)["items"].([]any)); n != 1 {
			t.Fatal("whisper wrongly wiped for unread member after first read")
		}
		// Last reader: now every non-sender member read it — physical wipe for all.
		d = f.expect(7, "POST", "/v1/chats/"+rid+"/read", DataMap{"through_seq": msg["seq"]}, 200)
		if d["wiped"] != float64(1) {
			t.Fatalf("physical wipe expected after all readers: %v", d)
		}
		for _, i := range []int{0, 7, 8} {
			if n := len(f.expect(i, "GET", "/v1/chats/"+rid+"/messages", nil, 200)["items"].([]any)); n != 0 {
				t.Fatalf("whisper remains for member %d after full wipe", i)
			}
		}
		dup := f.expect(8, "POST", "/v1/chats/"+rid+"/messages", DataMap{"text": "group secret", "mode": "WHISPER", "client_id": cid}, 200)
		if dup["expired"] != true {
			t.Fatal("fully-read whisper resurrected via dedup")
		}
		f.exec("DELETE FROM social.friendships")
	})
	run("notifications_and_sessions_ownership", func(t *testing.T) {
		d := f.expect(5, "GET", "/v1/notifications", nil, 200)
		list := d["items"].([]any)
		if len(list) > 0 {
			f.expect(5, "POST", "/v1/notifications/"+list[0].(map[string]any)["id"].(string)+"/read", DataMap{}, 204)
		}
		d = f.expect(0, "GET", "/v1/auth/sessions", nil, 200)
		sid := d["items"].([]any)[0].(map[string]any)["id"].(string)
		f.expect(1, "DELETE", "/v1/auth/sessions/"+sid, nil, 204)
		f.expect(0, "GET", "/v1/profile", nil, 200)
	})
	run("search_types_and_pagination", func(t *testing.T) {
		f.expect(1, "GET", "/v1/search?type=posts&q=Public", nil, 200)
		f.expect(2, "GET", "/v1/search?type=events&q=Coffee", nil, 200)
		f.expect(0, "GET", "/v1/search?type=dating&q=Safe", nil, 400)
		f.expect(0, "GET", "/v1/search?type=people&q=x", nil, 400)
		f.expect(0, "GET", "/v1/search?type=people&q=%25%27", nil, 200)
		for i := 0; i < 22; i++ {
			f.exec("INSERT INTO social.posts(author,body,township,audience,status) VALUES($1,'Pagination fixture','kamayut','PUBLIC','LIVE')", f.users[0])
		}
		d := f.expect(1, "GET", "/v1/feed", nil, 200)
		if len(d["items"].([]any)) != 20 || d["next_cursor"] == nil {
			t.Fatal("pagination first page")
		}
		next := d["next_cursor"].(string)
		d = f.expect(1, "GET", "/v1/feed?cursor="+next, nil, 200)
		if len(d["items"].([]any)) != 3 {
			t.Fatal("pagination second page", d)
		}
	})
	run("invalid_inputs_rejected_without_database_errors", func(t *testing.T) {
		cases := []struct {
			method, path string
			b            any
			status       int
		}{
			{"POST", "/v1/posts", DataMap{}, 400}, {"PUT", "/v1/profile", DataMap{}, 400}, {"PUT", "/v1/dating-profile", DataMap{}, 400},
			{"POST", "/v1/groups", DataMap{}, 400}, {"POST", "/v1/chats", DataMap{}, 400}, {"POST", "/v1/safety/report", DataMap{}, 400},
			{"POST", "/v1/safety/block", DataMap{}, 400}, {"POST", "/v1/safety/capture-alert", DataMap{}, 400},
			{"POST", "/v1/actions/swipe", DataMap{}, 400}, {"POST", "/v1/location", DataMap{}, 400},
			{"POST", "/v1/posts/" + post + "/reactions", DataMap{}, 400}, {"POST", "/v1/posts/" + post + "/comments", DataMap{}, 400},
			{"POST", "/v1/posts/" + post + "/shares", DataMap{}, 400}, {"POST", "/v1/chats/" + room + "/messages", DataMap{}, 400},
			{"GET", "/v1/events?township=kamayut", nil, 200}, {"GET", "/v1/notifications?category=invalid", nil, 400},
			{"GET", "/v1/posts/bad/comments", nil, 400}, {"GET", "/v1/radar/blips/bad", nil, 404},
			{"GET", "/v1/lookaround?band=3km&township=kamayut", nil, 200},
		}
		// Ghost mode already reset, but its radar timer remains disabled by design.
		for _, v := range cases {
			f.expect(5, v.method, v.path, v.b, v.status)
		}
	})
	run("otp_attempts_cannot_be_bruteforced", func(t *testing.T) {
		ch := f.expect(-1, "POST", "/v1/auth/otp/request", DataMap{"phone": "+66876543210"}, 202)
		wrong := "000000"
		if ch["development_code"] == wrong {
			wrong = "111111"
		}
		b := DataMap{"challenge_id": ch["challenge_id"], "code": wrong, "dob": "1990-01-01", "device": "test"}
		for i := 0; i < 5; i++ {
			f.expect(-1, "POST", "/v1/auth/otp/verify", b, 401)
		}
		b["code"] = ch["development_code"]
		f.expect(-1, "POST", "/v1/auth/otp/verify", b, 401)
	})
	run("account_delete_revokes", func(t *testing.T) {
		f.expect(8, "DELETE", "/v1/account", DataMap{"confirm": "no"}, 400)
		f.expect(8, "DELETE", "/v1/account", DataMap{"confirm": "DELETE MY ACCOUNT"}, 204)
		f.expect(8, "GET", "/v1/profile", nil, 401)
	})
}
