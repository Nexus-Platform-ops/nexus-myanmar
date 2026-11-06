package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"nexus/internal/moderation"
	"nexus/internal/retention"
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
func TestEnvConfigDefaultsAndOverrides(t *testing.T) {
	for _, k := range []string{"APP_ENV", "LISTEN_ADDR", "REDIS_URL", "ALLOWED_ORIGIN"} {
		t.Setenv(k, "")
	}
	c := EnvConfig()
	if c.Env != "development" || c.Addr != ":8080" || c.RedisURL != "redis://localhost:6379/0" || c.Origin != "http://localhost:3000" {
		t.Fatalf("defaults: %+v", c)
	}
	t.Setenv("APP_ENV", "staging")
	t.Setenv("LISTEN_ADDR", ":9090")
	t.Setenv("REDIS_URL", "redis://example:6379/1")
	t.Setenv("ALLOWED_ORIGIN", "https://app.example")
	t.Setenv("DATABASE_URL", "postgres://db/nexus")
	t.Setenv("JWT_SECRET", strings.Repeat("j", 40))
	t.Setenv("DATA_KEY", strings.Repeat("d", 40))
	t.Setenv("OTP_SECRET", strings.Repeat("o", 40))
	t.Setenv("SMS_URL", "https://sms.example")
	t.Setenv("SMS_TOKEN", "token")
	c = EnvConfig()
	if c.Env != "staging" || c.Addr != ":9090" || c.DatabaseURL != "postgres://db/nexus" || c.SMSURL != "https://sms.example" {
		t.Fatalf("overrides: %+v", c)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid staging config rejected: %v", err)
	}
}
func TestConfigurationValidationBranches(t *testing.T) {
	base := func() Config {
		return Config{Env: "test", DatabaseURL: "postgres://localhost/nexus_test", JWTSecret: strings.Repeat("a", 32), DataKey: strings.Repeat("b", 32), OTPSecret: strings.Repeat("c", 32)}
	}
	for name, mutate := range map[string]func(*Config){
		"invalid_env":    func(c *Config) { c.Env = "qa" },
		"short_secret":   func(c *Config) { c.OTPSecret = "short" },
		"missing_db":     func(c *Config) { c.DatabaseURL = "" },
		"staging_no_sms": func(c *Config) { c.Env = "staging" },
		"insecure_sms":   func(c *Config) { c.Env = "staging"; c.SMSURL = "http://sms.example"; c.SMSToken = "t" },
		"dev_secret_used": func(c *Config) {
			c.Env = "staging"
			c.SMSURL = "https://sms.example"
			c.SMSToken = "t"
			c.JWTSecret = strings.Repeat("development", 5)
		},
	} {
		c := base()
		mutate(&c)
		if c.Validate() == nil {
			t.Fatalf("%s accepted", name)
		}
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

func TestNewRejectsInvalidConfig(t *testing.T) {
	if _, err := New(context.Background(), Config{}); err == nil {
		t.Fatal("invalid config accepted")
	}
	cfg := Config{Env: "test", DatabaseURL: "postgres://localhost/nexus_test", RedisURL: "://bad", JWTSecret: strings.Repeat("a", 32), DataKey: strings.Repeat("b", 32), OTPSecret: strings.Repeat("c", 32)}
	if _, err := New(context.Background(), cfg); err == nil {
		t.Fatal("invalid redis url accepted")
	}
}
func TestPurgeFailsWithoutDatabase(t *testing.T) {
	p, err := pgxpool.New(context.Background(), "postgres://nexus:pw@127.0.0.1:1/nexus_test?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err = retention.PurgeDue(context.Background(), p); err == nil {
		t.Fatal("purge succeeded without database")
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
	return f.reqHeaders(i, method, path, body, nil)
}

func (f *fixture) reqHeaders(i int, method, path string, body any, headers map[string]string) (int, DataMap) {
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
	for k, v := range headers {
		r.Header.Set(k, v)
	}
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

func (f *fixture) bootExec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.boot.Exec(context.Background(), q, args...); err != nil {
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
		f.expect(-1, "GET", "/v1/ws?ticket=short", nil, 401)
		f.expect(-1, "GET", "/v1/ws?ticket="+strings.Repeat("a", 64), nil, 401)
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
	run("websocket_ordered_replay", func(t *testing.T) {
		f.truncate("private.outbox")
		var base int64
		if err := f.boot.QueryRow(context.Background(), `SELECT COALESCE(MAX(id),0) FROM private.outbox`).Scan(&base); err != nil {
			t.Fatal(err)
		}
		for n := 1; n <= 3; n++ {
			f.exec("INSERT INTO private.outbox(topic,user_id,payload) VALUES('test.event',$1,jsonb_build_object('n',$2::int))", f.users[0], n)
		}
		f.exec("INSERT INTO private.outbox(topic,user_id,payload) VALUES('test.event',$1,jsonb_build_object('n',99))", f.users[1])
		dial := func(i int, cursor *int64) *websocket.Conn {
			d := f.expect(i, "POST", "/v1/realtime/ticket", DataMap{}, 201)
			url := "ws" + strings.TrimPrefix(f.ts.URL, "http") + "/v1/ws?ticket=" + d["ticket"].(string)
			if cursor != nil {
				url += "&cursor=" + strconv.FormatInt(*cursor, 10)
			}
			c, _, err := websocket.DefaultDialer.Dial(url, nil)
			if err != nil {
				t.Fatal(err)
			}
			_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
			return c
		}
		type frame struct {
			Type    string         `json:"type"`
			ID      int64          `json:"id"`
			Topic   string         `json:"topic"`
			Cursor  int64          `json:"cursor"`
			Payload map[string]any `json:"payload"`
		}
		read := func(c *websocket.Conn) frame {
			var m frame
			if err := c.ReadJSON(&m); err != nil {
				t.Fatal(err)
			}
			return m
		}
		expectEvent := func(c *websocket.Conn, n int) int64 {
			m := read(c)
			if m.Type != "event" || m.Topic != "test.event" {
				t.Fatalf("unexpected frame %+v", m)
			}
			if m.Payload["n"].(float64) != float64(n) {
				t.Fatalf("out of order: got %v want %d", m.Payload["n"], n)
			}
			return m.ID
		}

		c := dial(0, &base)
		first := expectEvent(c, 1)
		expectEvent(c, 2)
		c.Close()

		// Resume from the first event: no duplicate, no gap.
		c = dial(0, &first)
		later := expectEvent(c, 2)
		expectEvent(c, 3)

		// A pruned range is announced, not silently skipped.
		f.exec("DELETE FROM private.outbox WHERE id=$1", first)
		c.Close()
		missed := first - 1
		c = dial(0, &missed)
		if m := read(c); m.Type != "replay_truncated" || m.Cursor != missed {
			t.Fatalf("missing truncation notice: %+v", m)
		}
		last := expectEvent(c, 2)
		expectEvent(c, 3)
		if last != later {
			t.Fatal("cursor drifted")
		}

		// Live events arrive on the poll tick after the replay drains.
		f.exec("INSERT INTO private.outbox(topic,user_id,payload) VALUES('test.event',$1,jsonb_build_object('n',4))", f.users[0])
		expectEvent(c, 4)
		c.Close()

		// An unparseable cursor is rejected before the upgrade.
		d := f.expect(0, "POST", "/v1/realtime/ticket", DataMap{}, 201)
		_, res, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(f.ts.URL, "http")+"/v1/ws?ticket="+d["ticket"].(string)+"&cursor=abc", nil)
		if err == nil || res.StatusCode != 400 {
			t.Fatal("invalid cursor accepted")
		}
		f.truncate("private.outbox")
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
	run("report_daily_cap", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			f.expect(5, "POST", "/v1/safety/report", DataMap{"target_id": f.users[6], "category": "OTHER", "details": "daily cap probe"}, 202)
		}
		f.expect(5, "POST", "/v1/safety/report", DataMap{"target_id": f.users[6], "category": "OTHER", "details": "daily cap probe"}, 429)
		f.exec("DELETE FROM safety.reports WHERE reporter=$1", f.users[5])
		f.exec("DELETE FROM safety.blocks WHERE user_id=$1", f.users[5])
		f.exec("DELETE FROM social.notifications WHERE user_id=$1", f.users[5])
		f.bootExec("DELETE FROM safety.brigade_flags WHERE target_id=$1", f.users[6])
	})
	run("coordinated_report_flag", func(t *testing.T) {
		// reporter 7 already reported target 8 in report_high_lock; four more
		// distinct reporters cross the five-reporter threshold.
		for i := 0; i < 4; i++ {
			f.expect(i, "POST", "/v1/safety/report", DataMap{"target_id": f.users[8], "category": "OTHER", "details": "coordinated probe"}, 202)
		}
		var reporters int
		if err := f.boot.QueryRow(context.Background(), `SELECT reporters FROM safety.brigade_flags WHERE target_id=$1`, f.users[8]).Scan(&reporters); err != nil {
			t.Fatal(err)
		}
		if reporters < 5 {
			t.Fatalf("brigade flag reporters=%d", reporters)
		}
		f.exec("DELETE FROM safety.reports WHERE target_id=$1", f.users[8])
		f.exec("DELETE FROM safety.blocks WHERE target_id=$1", f.users[8])
		f.exec("DELETE FROM social.notifications WHERE user_id IN($1,$2,$3,$4,$5)", f.users[7], f.users[0], f.users[1], f.users[2], f.users[3])
		f.bootExec("DELETE FROM safety.brigade_flags WHERE target_id=$1", f.users[8])
	})
	run("appeal_workflow", func(t *testing.T) {
		postID := uuid.NewString()
		f.exec(`INSERT INTO social.posts(id,author,body,township,audience,status) VALUES($1,$2,'appeal fixture','kamayut','PUBLIC','REMOVED')`, postID, f.users[0])
		var decision, live int64
		if err := f.boot.QueryRow(context.Background(), `INSERT INTO safety.audit(action,resource_id) VALUES('MODERATE_post_REMOVED',$1) RETURNING id`, postID).Scan(&decision); err != nil {
			t.Fatal(err)
		}
		out := f.expect(0, "POST", "/v1/safety/appeals", DataMap{"decision_id": decision, "reason": "This removal was mistaken"}, 201)
		if out["status"] != "OPEN" {
			t.Fatal(out)
		}
		f.expect(0, "POST", "/v1/safety/appeals", DataMap{"decision_id": decision, "reason": "This removal was mistaken"}, 409)
		f.expect(1, "POST", "/v1/safety/appeals", DataMap{"decision_id": decision, "reason": "This removal was mistaken"}, 404)
		f.expect(0, "POST", "/v1/safety/appeals", DataMap{"decision_id": decision, "reason": "short"}, 400)
		list := f.expect(0, "GET", "/v1/safety/appeals", nil, 200)
		if len(list["items"].([]any)) != 1 {
			t.Fatal(list)
		}
		if err := f.boot.QueryRow(context.Background(), `INSERT INTO safety.audit(action,resource_id) VALUES('MODERATE_post_LIVE',$1) RETURNING id`, postID).Scan(&live); err != nil {
			t.Fatal(err)
		}
		f.expect(0, "POST", "/v1/safety/appeals", DataMap{"decision_id": live, "reason": "This restoration was mistaken"}, 404)
		f.bootExec("DELETE FROM safety.appeals WHERE decision_id=$1", decision)
		f.exec("DELETE FROM safety.audit WHERE id IN($1,$2)", decision, live)
		f.exec("DELETE FROM social.posts WHERE id=$1", postID)
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
	run("whisper_time_expiry", func(t *testing.T) {
		ctx := context.Background()
		msg := f.expect(5, "POST", "/v1/chats/"+room+"/messages", DataMap{"text": "ttl secret", "mode": "WHISPER", "client_id": uuid.NewString()}, 201)
		if n := len(f.expect(6, "GET", "/v1/chats/"+room+"/messages", nil, 200)["items"].([]any)); n != 1 {
			t.Fatal("whisper not delivered before expiry")
		}
		f.exec("UPDATE chat.messages SET whisper_expires_at=now()-interval '1 second' WHERE id=$1", msg["id"])
		if n := len(f.expect(6, "GET", "/v1/chats/"+room+"/messages", nil, 200)["items"].([]any)); n != 0 {
			t.Fatal("expired whisper still readable")
		}
		f.exec(`WITH expired AS (DELETE FROM chat.messages WHERE mode='WHISPER' AND whisper_expires_at<=now() RETURNING id,room_id,seq) INSERT INTO chat.tombstones(message_id,room_id,seq) SELECT id,room_id,seq FROM expired ON CONFLICT DO NOTHING`)
		var swept, tomb bool
		if err := f.boot.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM chat.messages WHERE id=$1),EXISTS(SELECT 1 FROM chat.tombstones WHERE message_id=$1)`, msg["id"]).Scan(&swept, &tomb); err != nil {
			t.Fatal(err)
		}
		if !swept || !tomb {
			t.Fatalf("janitor sweep swept=%v tombstone=%v", swept, tomb)
		}
		f.exec("DELETE FROM chat.tombstones WHERE message_id=$1", msg["id"])
	})
	run("idempotency_key_replay", func(t *testing.T) {
		ctx := context.Background()
		key := uuid.NewString()
		body := DataMap{"body": "Idempotent post", "audience": "PUBLIC", "township": "kamayut"}
		st1, first := f.reqHeaders(0, "POST", "/v1/posts", body, map[string]string{"Idempotency-Key": key})
		st2, second := f.reqHeaders(0, "POST", "/v1/posts", body, map[string]string{"Idempotency-Key": key})
		if st1 != 202 || st2 != 202 || first["id"] != second["id"] {
			t.Fatalf("replay mismatch: %d %d %v %v", st1, st2, first, second)
		}
		var posts int
		if err := f.boot.QueryRow(ctx, `SELECT count(*) FROM social.posts WHERE body='Idempotent post'`).Scan(&posts); err != nil || posts != 1 {
			t.Fatalf("duplicate post rows=%d err=%v", posts, err)
		}
		if st, _ := f.reqHeaders(0, "POST", "/v1/posts", DataMap{"body": "Different body", "audience": "PUBLIC", "township": "kamayut"}, map[string]string{"Idempotency-Key": key}); st != 409 {
			t.Fatalf("key reuse with different body: %d", st)
		}
		if st, _ := f.reqHeaders(0, "POST", "/v1/posts", body, map[string]string{"Idempotency-Key": "not-a-uuid"}); st != 400 {
			t.Fatalf("invalid idempotency key: %d", st)
		}
		rkey := uuid.NewString()
		report := DataMap{"target_id": f.users[4], "category": "OTHER", "details": "idempotent report"}
		rs1, r1 := f.reqHeaders(0, "POST", "/v1/safety/report", report, map[string]string{"Idempotency-Key": rkey})
		rs2, r2 := f.reqHeaders(0, "POST", "/v1/safety/report", report, map[string]string{"Idempotency-Key": rkey})
		if rs1 != 202 || rs2 != 202 || r1["case_id"] != r2["case_id"] {
			t.Fatalf("report replay mismatch: %d %d %v %v", rs1, rs2, r1, r2)
		}
		var reports int
		if err := f.boot.QueryRow(ctx, `SELECT count(*) FROM safety.reports WHERE reporter=$1 AND target_id=$2`, f.users[0], f.users[4]).Scan(&reports); err != nil || reports != 1 {
			t.Fatalf("duplicate report rows=%d err=%v", reports, err)
		}
		f.exec("DELETE FROM social.posts WHERE body='Idempotent post'")
		f.exec("DELETE FROM safety.reports WHERE reporter=$1", f.users[0])
		f.exec("DELETE FROM safety.blocks WHERE user_id=$1", f.users[0])
		f.exec("DELETE FROM social.notifications WHERE user_id=$1 AND category='SAFETY'", f.users[0])
		f.exec("DELETE FROM identity.idempotency WHERE user_id=$1", f.users[0])
	})
	run("friend_graph_api", func(t *testing.T) {
		ctx := context.Background()
		f.expect(1, "POST", "/v1/friends", DataMap{"user_id": f.users[1]}, 400)
		f.expect(1, "POST", "/v1/friends", DataMap{"user_id": "bad"}, 400)
		out := f.expect(1, "POST", "/v1/friends", DataMap{"user_id": f.users[2]}, 201)
		if out["user_id"] != f.users[2] {
			t.Fatal(out)
		}
		f.expect(1, "POST", "/v1/friends", DataMap{"user_id": f.users[2]}, 201)
		list := f.expect(1, "GET", "/v1/friends", nil, 200)
		if n := len(list["items"].([]any)); n != 1 {
			t.Fatalf("friend list rows=%d", n)
		}
		var rows int
		if err := f.boot.QueryRow(ctx, `SELECT count(*) FROM social.friendships WHERE (user_id=$1 AND friend_id=$2) OR (user_id=$2 AND friend_id=$1)`, f.users[1], f.users[2]).Scan(&rows); err != nil || rows != 2 {
			t.Fatalf("symmetric rows=%d err=%v", rows, err)
		}
		notice := f.expect(2, "GET", "/v1/notifications?category=FOLLOW", nil, 200)
		if len(notice["items"].([]any)) != 1 {
			t.Fatalf("friend notification duplicated: %v", notice)
		}
		f.exec(`INSERT INTO safety.blocks(user_id,target_id) VALUES($1,$2)`, f.users[2], f.users[1])
		f.expect(1, "POST", "/v1/friends", DataMap{"user_id": f.users[2]}, 403)
		f.exec("DELETE FROM safety.blocks WHERE user_id=$1 AND target_id=$2", f.users[2], f.users[1])
		f.exec("UPDATE identity.accounts SET ghost=true WHERE id=$1", f.users[3])
		f.expect(1, "POST", "/v1/friends", DataMap{"user_id": f.users[3]}, 404)
		f.exec("UPDATE identity.accounts SET ghost=false WHERE id=$1", f.users[3])
		f.expect(1, "DELETE", "/v1/friends/"+f.users[2], nil, 204)
		f.expect(1, "DELETE", "/v1/friends/"+f.users[2], nil, 404)
		if err := f.boot.QueryRow(ctx, `SELECT count(*) FROM social.friendships WHERE (user_id=$1 AND friend_id=$2) OR (user_id=$2 AND friend_id=$1)`, f.users[1], f.users[2]).Scan(&rows); err != nil || rows != 0 {
			t.Fatalf("friendship rows after delete=%d err=%v", rows, err)
		}
		f.exec("DELETE FROM social.notifications WHERE user_id=$1 AND kind='FRIEND_ADDED'", f.users[2])
	})
	run("notification_preferences", func(t *testing.T) {
		d := f.expect(4, "GET", "/v1/notifications/preferences", nil, 200)
		if d["activity_muted"] != false || d["safety_locked"] != true {
			t.Fatal(d)
		}
		f.expect(4, "PUT", "/v1/notifications/preferences", DataMap{"activity_muted": true, "follow_muted": true}, 200)
		f.expect(4, "PUT", "/v1/notifications/preferences", DataMap{"safety_muted": true}, 400)
		f.exec(`INSERT INTO social.notifications(user_id,category,kind) VALUES($1,'ACTIVITY','PROBE'),($1,'SAFETY','PROBE')`, f.users[4])
		if n := len(f.expect(4, "GET", "/v1/notifications?category=ACTIVITY", nil, 200)["items"].([]any)); n != 0 {
			t.Fatalf("muted activity delivered: %d", n)
		}
		if n := len(f.expect(4, "GET", "/v1/notifications?category=SAFETY", nil, 200)["items"].([]any)); n < 1 {
			t.Fatal("safety notice suppressed by mute")
		}
		f.expect(4, "PUT", "/v1/notifications/preferences", DataMap{"activity_muted": false, "follow_muted": false}, 200)
		if n := len(f.expect(4, "GET", "/v1/notifications?category=ACTIVITY", nil, 200)["items"].([]any)); n != 1 {
			t.Fatalf("unmuted activity missing: %d", n)
		}
		f.exec("DELETE FROM social.notifications WHERE user_id=$1 AND kind='PROBE'", f.users[4])
		f.exec("DELETE FROM social.notification_prefs WHERE user_id=$1", f.users[4])
	})
	run("meet_safe_trusted_contacts", func(t *testing.T) {
		f.expect(0, "POST", "/v1/safety/trusted-contacts", DataMap{"user_id": f.users[0], "label": "Me"}, 400)
		f.expect(0, "POST", "/v1/safety/trusted-contacts", DataMap{"user_id": "nope", "label": "X"}, 400)
		f.expect(0, "POST", "/v1/safety/trusted-contacts", DataMap{"user_id": f.users[1], "label": "   "}, 400)
		f.exec("UPDATE identity.accounts SET ghost=true WHERE id=$1", f.users[8])
		f.expect(0, "POST", "/v1/safety/trusted-contacts", DataMap{"user_id": f.users[8], "label": "Ghost"}, 404)
		f.exec("UPDATE identity.accounts SET ghost=false WHERE id=$1", f.users[8])
		f.expect(0, "POST", "/v1/safety/trusted-contacts", DataMap{"user_id": f.users[1], "label": "Sister"}, 201)
		f.expect(0, "POST", "/v1/safety/trusted-contacts", DataMap{"user_id": f.users[1], "label": "Elder sister"}, 201)
		f.expect(0, "POST", "/v1/safety/trusted-contacts", DataMap{"user_id": uuid.NewString(), "label": "Missing"}, 404)
		items := f.expect(0, "GET", "/v1/safety/trusted-contacts", nil, 200)["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["label"] != "Elder sister" || items[0].(map[string]any)["real_name"] != "Social Person 1" {
			t.Fatalf("trusted contact upsert failed: %v", items)
		}
		f.expect(0, "POST", "/v1/safety/block", DataMap{"target_id": f.users[1]}, 200)
		if n := len(f.expect(0, "GET", "/v1/safety/trusted-contacts", nil, 200)["items"].([]any)); n != 0 {
			t.Fatal("blocked contact still listed")
		}
		f.expect(0, "DELETE", "/v1/safety/blocks/"+f.users[1], nil, 204)
		f.expect(0, "DELETE", "/v1/safety/trusted-contacts/"+f.users[1], nil, 204)
		f.expect(0, "DELETE", "/v1/safety/trusted-contacts/"+f.users[1], nil, 404)
	})
	run("meet_safe_checkin_flow", func(t *testing.T) {
		f.expect(0, "POST", "/v1/safety/trusted-contacts", DataMap{"user_id": f.users[2], "label": "Friend"}, 201)
		f.expect(0, "POST", "/v1/safety/checkins", DataMap{"place_label": "Kandawgyi", "minutes": 1}, 400)
		f.expect(0, "POST", "/v1/safety/checkins", DataMap{"place_label": " ", "minutes": 60}, 400)
		f.expect(0, "POST", "/v1/safety/checkins", DataMap{"place_label": "Kandawgyi", "minutes": 60, "event_id": uuid.NewString()}, 400)
		f.expect(0, "POST", "/v1/safety/checkins", DataMap{"place_label": "Kandawgyi", "minutes": 60, "event_id": "bad"}, 400)
		d := f.expect(0, "POST", "/v1/safety/checkins", DataMap{"place_label": "Kandawgyi", "minutes": 60}, 201)
		id := d["id"].(string)
		if d["status"] != "PENDING" {
			t.Fatal(d)
		}
		for i := 0; i < 4; i++ {
			f.expect(0, "POST", "/v1/safety/checkins", DataMap{"place_label": "Open check-in", "minutes": 60}, 201)
		}
		f.expect(0, "POST", "/v1/safety/checkins", DataMap{"place_label": "One too many", "minutes": 60}, 409)
		f.expect(0, "POST", "/v1/safety/checkins/"+uuid.NewString()+"/confirm", DataMap{}, 404)
		f.exec("DELETE FROM safety.checkins WHERE user_id=$1 AND place_label='Open check-in'", f.users[0])
		if n := len(f.expect(2, "GET", "/v1/safety/checkins?role=contact", nil, 200)["items"].([]any)); n != 1 {
			t.Fatalf("contact cannot see check-in: %d", n)
		}
		if n := len(f.expect(3, "GET", "/v1/safety/checkins?role=contact", nil, 200)["items"].([]any)); n != 0 {
			t.Fatal("non-contact saw check-in")
		}
		if n := len(f.expect(2, "GET", "/v1/notifications?category=SAFETY", nil, 200)["items"].([]any)); n < 1 {
			t.Fatal("trusted contact not notified")
		}
		f.expect(3, "POST", "/v1/safety/checkins/"+id+"/confirm", DataMap{}, 404)
		f.expect(0, "POST", "/v1/safety/checkins/"+id+"/confirm", DataMap{}, 200)
		f.expect(0, "POST", "/v1/safety/checkins/"+id+"/confirm", DataMap{}, 409)
		if n := len(f.expect(2, "GET", "/v1/safety/checkins?role=contact", nil, 200)["items"].([]any)); n != 1 {
			t.Fatal("resolved check-in hidden from contact")
		}
		// An overdue check-in stays PENDING in storage and is reported as OVERDUE.
		d = f.expect(0, "POST", "/v1/safety/checkins", DataMap{"place_label": "Sule", "minutes": 30}, 201)
		late := d["id"].(string)
		f.exec("UPDATE safety.checkins SET due_at=now()-interval '1 minute' WHERE id=$1", late)
		items := f.expect(0, "GET", "/v1/safety/checkins", nil, 200)["items"].([]any)
		var status string
		for _, v := range items {
			if m := v.(map[string]any); m["id"] == late {
				status = m["status"].(string)
			}
		}
		if status != "OVERDUE" {
			t.Fatalf("overdue not derived: %q", status)
		}
		f.expect(0, "POST", "/v1/safety/checkins/"+late+"/cancel", DataMap{}, 200)
		f.expect(0, "GET", "/v1/safety/checkins?role=bogus", nil, 400)
		f.exec("DELETE FROM safety.checkins WHERE user_id=$1", f.users[0])
		f.exec("DELETE FROM safety.trusted_contacts WHERE user_id=$1", f.users[0])
		f.exec("DELETE FROM social.notifications WHERE user_id=$1 AND kind LIKE 'CHECKIN_%'", f.users[2])
	})
	run("account_deletion_grace_and_purge", func(t *testing.T) {
		ctx := context.Background()
		f.exec("INSERT INTO identity.sessions(id,user_id,refresh_hash,device_label,expires_at) VALUES($1,$2,$3,'other',now()+interval '1 day')", uuid.NewString(), f.users[6], randomToken())
		code := "654321"
		ph := "fixture6"
		var challenge string
		if err := f.boot.QueryRow(ctx, `INSERT INTO identity.otp(phone_hash,phone_cipher,code_hash,expires_at) VALUES($1,pgp_sym_encrypt('private',$2),$3,now()+interval '5 minutes') RETURNING id`, ph, f.s.Cfg.DataKey, keyed(f.s.Cfg.OTPSecret, ph+":"+code)).Scan(&challenge); err != nil {
			t.Fatal(err)
		}
		f.expect(6, "DELETE", "/v1/account", DataMap{"confirm": "yes"}, 400)
		d := f.expect(6, "DELETE", "/v1/account", DataMap{"confirm": "DELETE MY ACCOUNT"}, 202)
		if d["purge_after"] == nil || d["requested_at"] == nil {
			t.Fatalf("missing grace window: %v", d)
		}
		var sessions int
		if err := f.boot.QueryRow(ctx, "SELECT count(*) FROM identity.sessions WHERE user_id=$1", f.users[6]).Scan(&sessions); err != nil {
			t.Fatal(err)
		}
		if sessions != 1 {
			t.Fatalf("other sessions survived: %d", sessions)
		}
		if d = f.expect(6, "GET", "/v1/account/deletion", nil, 200); d["pending"] != true {
			t.Fatalf("deletion not pending: %v", d)
		}
		f.expect(6, "POST", "/v1/posts", DataMap{"body": "still here", "audience": "PUBLIC", "township": "kamayut"}, 403)
		// A pending deletion cannot be re-entered by OTP login on a new device.
		f.expect(-1, "POST", "/v1/auth/otp/verify", DataMap{"challenge_id": challenge, "code": code, "dob": "1997-01-01", "device": "probe"}, 403)
		f.expect(6, "POST", "/v1/account/deletion/cancel", DataMap{}, 204)
		if d = f.expect(6, "GET", "/v1/account/deletion", nil, 200); d["pending"] != false {
			t.Fatalf("deletion still pending: %v", d)
		}
		f.expect(6, "POST", "/v1/account/deletion/cancel", DataMap{}, 404)
		f.expect(6, "POST", "/v1/posts", DataMap{"body": "back again", "audience": "PUBLIC", "township": "kamayut"}, 202)

		// Fan-out erases the account, sessions, non-FK rows and the orphaned room.
		probe, rid := uuid.NewString(), uuid.NewString()
		f.exec(`INSERT INTO identity.accounts(id,phone_hash,phone_cipher,dob,phone_verified) VALUES($1,'purge_probe',pgp_sym_encrypt('private',$2),'1997-01-01',true)`, probe, f.s.Cfg.DataKey)
		f.exec("INSERT INTO identity.sessions(id,user_id,refresh_hash,device_label,expires_at) VALUES($1,$2,$3,'probe',now()+interval '1 day')", uuid.NewString(), probe, randomToken())
		f.exec("INSERT INTO identity.otp(phone_hash,phone_cipher,code_hash,expires_at) VALUES('purge_probe',pgp_sym_encrypt('x',$1),'x',now()+interval '5 minutes')", f.s.Cfg.DataKey)
		f.exec("INSERT INTO chat.rooms(id,kind) VALUES($1,'SOCIAL')", rid)
		f.exec("INSERT INTO chat.members(room_id,user_id) VALUES($1,$2)", rid, probe)
		f.exec("INSERT INTO private.outbox(topic,user_id,payload) VALUES('probe',$1,'{}'::jsonb)", probe)
		f.exec("INSERT INTO identity.deletion_requests(user_id,requested_at,purge_after) VALUES($1,now()-interval '31 days',now()-interval '1 second')", probe)
		n, err := retention.PurgeDue(ctx, f.boot)
		if err != nil || n != 1 {
			t.Fatalf("purge: %d %v", n, err)
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{"SELECT count(*) FROM identity.accounts WHERE id=$1", []any{probe}},
			{"SELECT count(*) FROM identity.sessions WHERE user_id=$1", []any{probe}},
			{"SELECT count(*) FROM identity.otp WHERE phone_hash='purge_probe'", nil},
			{"SELECT count(*) FROM chat.rooms WHERE id=$1", []any{rid}},
			{"SELECT count(*) FROM chat.members WHERE user_id=$1", []any{probe}},
			{"SELECT count(*) FROM private.outbox WHERE user_id=$1", []any{probe}},
			{"SELECT count(*) FROM identity.deletion_requests WHERE user_id=$1", []any{probe}},
		} {
			var left int
			if err := f.boot.QueryRow(ctx, q.sql, q.args...).Scan(&left); err != nil {
				t.Fatal(err)
			}
			if left != 0 {
				t.Fatalf("purge left %d rows for %s", left, q.sql)
			}
		}
	})
	run("search_pagination_and_recents", func(t *testing.T) {
		for i := 0; i < 22; i++ {
			f.exec(`INSERT INTO social.posts(author,body,township,audience,status) VALUES($1,'CursorFixture','kamayut','PUBLIC','LIVE')`, f.users[0])
		}
		d := f.expect(1, "GET", "/v1/search?type=posts&q=CursorFixture", nil, 200)
		if len(d["items"].([]any)) != 20 || d["next_cursor"] == nil {
			t.Fatalf("search first page: %v", d)
		}
		next := d["next_cursor"].(string)
		d = f.expect(1, "GET", "/v1/search?type=posts&q=CursorFixture&cursor="+next, nil, 200)
		if len(d["items"].([]any)) != 2 || d["next_cursor"] != nil {
			t.Fatalf("search second page: %v", d)
		}
		f.expect(1, "GET", "/v1/search?type=people&q=Social&cursor=bad", nil, 400)
		recent := f.expect(1, "GET", "/v1/search/recent", nil, 200)
		if len(recent["items"].([]any)) < 1 {
			t.Fatal("search not recorded")
		}
		f.expect(1, "DELETE", "/v1/search/recent", nil, 204)
		if n := len(f.expect(1, "GET", "/v1/search/recent", nil, 200)["items"].([]any)); n != 0 {
			t.Fatal("recent searches not cleared")
		}
		f.exec("DELETE FROM social.posts WHERE body='CursorFixture'")
		var extra []string
		for i := 0; i < 12; i++ {
			id := uuid.NewString()
			f.exec(`INSERT INTO identity.accounts(id,phone_hash,phone_cipher,dob,phone_verified,tier) VALUES($1,$2,pgp_sym_encrypt('private',$3),'1997-01-01',true,1)`, id, "pagesearch"+uuid.NewString(), cfg.DataKey)
			f.exec(`INSERT INTO social.user_profiles(user_id,real_name,township,bio) VALUES($1,$2,'kamayut','Pagination bio')`, id, fmt.Sprintf("Social Person P%d", i))
			extra = append(extra, id)
		}
		d = f.expect(1, "GET", "/v1/search?type=people&q=Social%20Person", nil, 200)
		if len(d["items"].([]any)) != 20 || d["next_cursor"] == nil {
			t.Fatalf("people first page: %d %v", len(d["items"].([]any)), d["next_cursor"])
		}
		d = f.expect(1, "GET", "/v1/search?type=people&q=Social%20Person&cursor="+d["next_cursor"].(string), nil, 200)
		if len(d["items"].([]any)) != 1 || d["next_cursor"] != nil {
			t.Fatalf("people second page: %v", d)
		}
		for _, id := range extra {
			f.exec("DELETE FROM identity.accounts WHERE id=$1", id)
		}
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
			{"POST", "/v1/chats/bad/messages", DataMap{"text": "x", "mode": "NORMAL", "client_id": uuid.NewString()}, 400},
			{"POST", "/v1/chats/" + room + "/messages", DataMap{"text": "x", "mode": "BAD", "client_id": uuid.NewString()}, 400},
			{"POST", "/v1/chats/" + room + "/read", DataMap{"through_seq": 0}, 400},
			{"GET", "/v1/chats/bad/messages", nil, 400},
			{"POST", "/v1/events/bad/rsvp", DataMap{"status": "ATTEND"}, 400},
			{"POST", "/v1/groups/bad/join", DataMap{}, 400},
			{"GET", "/v1/events?township=kamayut", nil, 200}, {"GET", "/v1/notifications?category=invalid", nil, 400},
			{"GET", "/v1/posts/bad/comments", nil, 400}, {"GET", "/v1/radar/blips/bad", nil, 404},
			{"GET", "/v1/lookaround?band=3km&township=kamayut", nil, 200},
			{"POST", "/v1/auth/refresh", DataMap{"refresh_token": "short"}, 401},
			{"POST", "/v1/auth/refresh", DataMap{"refresh_token": strings.Repeat("x", 64)}, 401},
			{"DELETE", "/v1/auth/sessions/bad", nil, 400},
			{"POST", "/v1/auth/otp/verify", DataMap{"challenge_id": uuid.NewString(), "code": "123456", "dob": "1990-01-01", "device": "probe"}, 401},
			{"POST", "/v1/actions/swipe", DataMap{"profile_id": uuid.NewString(), "intent": "LIKE"}, 404},
			{"POST", "/v1/events/" + uuid.NewString() + "/rsvp", DataMap{"status": "ATTEND"}, 404},
			{"POST", "/v1/groups/" + uuid.NewString() + "/join", DataMap{}, 200},
			{"POST", "/v1/safety/capture-alert", DataMap{"room_id": uuid.NewString(), "kind": "RECORDING"}, 404},
		}
		// Ghost mode already reset, but its radar timer remains disabled by design.
		for _, v := range cases {
			f.expect(5, v.method, v.path, v.b, v.status)
		}
	})
	run("safety_reports_blocks_and_notifications", func(t *testing.T) {
		f.exec("DELETE FROM safety.reports WHERE reporter=$1", f.users[2])
		out := f.expect(2, "POST", "/v1/safety/report", DataMap{"target_id": f.users[3], "category": "SCAM", "details": "listing fixture"}, 202)
		if out["severity"] != "MEDIUM" || out["response_due_minutes"] != float64(120) {
			t.Fatalf("report defaults: %v", out)
		}
		if n := len(f.expect(2, "GET", "/v1/safety/reports", nil, 200)["items"].([]any)); n != 1 {
			t.Fatalf("report listing: %d", n)
		}
		if n := len(f.expect(2, "GET", "/v1/safety/blocks", nil, 200)["items"].([]any)); n != 1 {
			t.Fatalf("report must hide the pair: %d", n)
		}
		f.expect(2, "POST", "/v1/safety/report", DataMap{"profile_id": f.profiles[4], "category": "HARASSMENT", "details": "by profile"}, 202)
		f.expect(2, "POST", "/v1/safety/report", DataMap{"target_id": f.users[2], "category": "SCAM", "details": "self"}, 400)
		f.expect(2, "POST", "/v1/safety/report", DataMap{"target_id": uuid.NewString(), "category": "SCAM", "details": "unknown"}, 404)
		f.expect(2, "POST", "/v1/safety/report", DataMap{"profile_id": "not-a-uuid", "category": "SCAM", "details": "bad"}, 400)
		f.expect(2, "POST", "/v1/safety/report", DataMap{"profile_id": uuid.NewString(), "category": "SCAM", "details": "absent"}, 404)
		f.expect(2, "POST", "/v1/safety/report", DataMap{"target_id": f.users[3], "category": "NOPE", "details": "bad"}, 400)
		f.expect(2, "POST", "/v1/safety/report", DataMap{"target_id": f.users[3], "category": "OTHER", "details": "  "}, 400)

		if b := f.expect(2, "POST", "/v1/safety/block", DataMap{"target_id": f.users[5]}, 200); b["blocked"] != true {
			t.Fatalf("first block: %v", b)
		}
		if b := f.expect(2, "POST", "/v1/safety/block", DataMap{"target_id": f.users[5]}, 200); b["blocked"] != false {
			t.Fatalf("duplicate block: %v", b)
		}
		f.expect(2, "POST", "/v1/safety/block", DataMap{"target_id": f.users[2]}, 400)
		f.expect(2, "POST", "/v1/safety/block", DataMap{"profile_id": "bad"}, 400)
		f.expect(2, "POST", "/v1/safety/block", DataMap{"profile_id": uuid.NewString()}, 404)
		f.expect(2, "POST", "/v1/safety/block", DataMap{"profile_id": f.profiles[7]}, 200)
		f.expect(2, "DELETE", "/v1/safety/blocks/"+f.users[5], nil, 204)
		f.expect(2, "DELETE", "/v1/safety/blocks/bad", nil, 400)

		d := f.expect(2, "GET", "/v1/notifications?category=SAFETY", nil, 200)
		list := d["items"].([]any)
		if len(list) < 1 {
			t.Fatalf("report notification missing: %v", d)
		}
		nid := list[0].(map[string]any)["id"].(string)
		f.expect(2, "POST", "/v1/notifications/"+nid+"/read", DataMap{}, 204)
		var read bool
		if err := f.boot.QueryRow(context.Background(), `SELECT read_at IS NOT NULL FROM social.notifications WHERE id=$1`, nid).Scan(&read); err != nil || !read {
			t.Fatalf("read_at not set: %v", err)
		}
		f.expect(2, "POST", "/v1/notifications/"+uuid.NewString()+"/read", DataMap{}, 204)
		f.expect(2, "GET", "/v1/notifications?category=FOLLOW", nil, 200)
		f.expect(2, "GET", "/v1/notifications?category=ACTIVITY", nil, 200)

		f.exec("DELETE FROM safety.reports WHERE reporter=$1", f.users[2])
		f.exec("DELETE FROM safety.blocks WHERE user_id=$1 OR target_id=$1", f.users[2])
		f.exec("DELETE FROM social.notifications WHERE user_id=$1", f.users[2])
	})
	run("comments_listing", func(t *testing.T) {
		d := f.expect(2, "GET", "/v1/posts/"+post+"/comments", nil, 200)
		items := d["items"].([]any)
		if len(items) != 2 || items[0].(map[string]any)["body"] != "Root" {
			t.Fatalf("comments listing: %v", d)
		}
	})
	run("chat_history_paging", func(t *testing.T) {
		rid := uuid.NewString()
		f.exec("INSERT INTO chat.rooms(id,kind) VALUES($1,'SOCIAL')", rid)
		f.exec("INSERT INTO chat.members(room_id,user_id) VALUES($1,$2),($1,$3)", rid, f.users[0], f.users[1])
		for n := 1; n <= 51; n++ {
			f.exec("INSERT INTO chat.messages(room_id,sender,body_cipher,mode,client_id) VALUES($1,$2,pgp_sym_encrypt($3,$4),'NORMAL',$5)", rid, f.users[0], fmt.Sprintf("page %d", n), cfg.DataKey, uuid.NewString())
		}
		d := f.expect(0, "GET", "/v1/chats/"+rid+"/messages", nil, 200)
		items := d["items"].([]any)
		if len(items) != 50 || d["next_before_seq"] == nil {
			t.Fatalf("first page: %d %v", len(items), d["next_before_seq"])
		}
		oldest := int64(items[0].(map[string]any)["seq"].(float64))
		if newest := int64(items[49].(map[string]any)["seq"].(float64)); newest <= oldest {
			t.Fatalf("ordering: %v", items)
		}
		d = f.expect(0, "GET", "/v1/chats/"+rid+"/messages?before_seq="+strconv.FormatInt(oldest, 10), nil, 200)
		if len(d["items"].([]any)) != 1 || d["next_before_seq"] != nil {
			t.Fatalf("tail page: %v", d)
		}
		f.expect(0, "GET", "/v1/chats/"+rid+"/messages?before_seq=0", nil, 400)
		f.expect(0, "GET", "/v1/chats/"+rid+"/messages?before_seq=nope", nil, 400)
	})
	run("cors_and_security_headers", func(t *testing.T) {
		get := func(origin, method string) *http.Response {
			r, _ := http.NewRequest(method, f.ts.URL+"/health/live", nil)
			if origin != "" {
				r.Header.Set("Origin", origin)
			}
			res, err := http.DefaultClient.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			return res
		}
		res := get("http://localhost:3000", "GET")
		if res.StatusCode != 200 || res.Header.Get("Access-Control-Allow-Origin") != "http://localhost:3000" || res.Header.Get("X-Content-Type-Options") != "nosniff" || res.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("cors headers: %d %v", res.StatusCode, res.Header)
		}
		if res = get("http://evil.example", "GET"); res.StatusCode != 403 {
			t.Fatalf("origin not denied: %d", res.StatusCode)
		}
		if res = get("http://localhost:3000", "OPTIONS"); res.StatusCode != 204 || res.Header.Get("Access-Control-Allow-Methods") == "" {
			t.Fatalf("preflight: %d %v", res.StatusCode, res.Header)
		}
	})
	run("staging_sms_adapter_fail_closed", func(t *testing.T) {
		// A staging server must call the HTTPS SMS adapter; an unreachable or
		// untrusted adapter fails closed instead of silently issuing a code.
		sms := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
		defer sms.Close()
		cfg2 := cfg
		cfg2.Env = "staging"
		cfg2.SMSURL = sms.URL
		cfg2.SMSToken = "probe-token"
		s2 := &Server{DB: s.DB, Redis: s.Redis, Cfg: cfg2, mux: http.NewServeMux()}
		s2.routes()
		ts2 := httptest.NewServer(s2.Handler())
		defer ts2.Close()
		phone := "+959111111111"
		r, _ := http.NewRequest("POST", ts2.URL+"/v1/auth/otp/request", strings.NewReader(`{"phone":"`+phone+`"}`))
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 503 {
			t.Fatalf("SMS failure must fail closed: %d", res.StatusCode)
		}
		f.exec("DELETE FROM identity.otp WHERE phone_hash=$1", keyed(cfg.OTPSecret, phone))
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
	run("account_delete_grace", func(t *testing.T) {
		f.expect(8, "DELETE", "/v1/account", DataMap{"confirm": "no"}, 400)
		d := f.expect(8, "DELETE", "/v1/account", DataMap{"confirm": "DELETE MY ACCOUNT"}, 202)
		if d["purge_after"] == nil {
			t.Fatalf("missing grace window: %v", d)
		}
		var ghost bool
		if err := f.boot.QueryRow(context.Background(), "SELECT ghost FROM identity.accounts WHERE id=$1", f.users[8]).Scan(&ghost); err != nil || !ghost {
			t.Fatalf("pending account not hidden: %v", err)
		}
		f.expect(8, "GET", "/v1/profile", nil, 200)
		f.expect(8, "POST", "/v1/account/deletion/cancel", DataMap{}, 204)
		f.expect(8, "GET", "/v1/account/deletion", nil, 200)
	})
	run("moderation_named_reviewer_and_appeals", func(t *testing.T) {
		ctx := context.Background()
		modDSN := os.Getenv("TEST_MODERATOR_DATABASE_URL")
		if modDSN == "" {
			modDSN = "postgres://nexus_moderator:nexus_mod_ci_only_pw1@localhost:5432/nexus_test?sslmode=disable"
		}
		if !strings.Contains(modDSN, "/nexus_test?") {
			t.Fatal("refusing moderator fixtures outside nexus_test")
		}
		mod, err := pgxpool.New(ctx, modDSN)
		if err != nil {
			t.Fatal(err)
		}
		defer mod.Close()
		suffix := uuid.NewString()[:8]
		nameA, nameB := "rev-a-"+suffix, "rev-b-"+suffix
		password := "reviewer-pass-123"
		postID := uuid.NewString()
		appeal := uuid.NewString()
		f.bootExec(`INSERT INTO social.posts(id,author,body,township,audience,status) VALUES($1,$2,'moderation fixture','kamayut','PUBLIC','UNDER_REVIEW')`, postID, f.users[0])
		idA, err := moderation.CreateReviewer(ctx, f.boot, nameA, "MODERATOR", password)
		if err != nil {
			t.Fatal(err)
		}
		idB, err := moderation.CreateReviewer(ctx, f.boot, nameB, "SENIOR", password)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			f.bootExec(`DELETE FROM safety.audit WHERE actor=$1 OR actor=$2 OR resource_id=$1 OR resource_id=$2 OR resource_id=$3 OR resource_id=$4`, idA, idB, postID, appeal)
			f.bootExec(`DELETE FROM safety.reviewers WHERE reviewer_id=$1 OR reviewer_id=$2`, idA, idB)
			f.bootExec(`DELETE FROM social.posts WHERE id=$1`, postID)
		}()
		var audited int
		if err = f.boot.QueryRow(ctx, `SELECT count(*) FROM safety.audit WHERE action='REVIEWER_ADDED' AND resource_id=$1`, idA).Scan(&audited); err != nil || audited != 1 {
			t.Fatalf("reviewer creation not audited: n=%d err=%v", audited, err)
		}
		if _, err = moderation.Authenticate(ctx, mod, nameA, "wrong-password-123"); !errors.Is(err, moderation.ErrInvalidCredentials) {
			t.Fatalf("wrong password: want ErrInvalidCredentials, got %v", err)
		}
		if err = moderation.SetReviewerActive(ctx, f.boot, nameA, false); err != nil {
			t.Fatal(err)
		}
		if _, err = moderation.Authenticate(ctx, mod, nameA, password); !errors.Is(err, moderation.ErrInvalidCredentials) {
			t.Fatalf("inactive reviewer: want ErrInvalidCredentials, got %v", err)
		}
		if err = moderation.SetReviewerActive(ctx, f.boot, nameA, true); err != nil {
			t.Fatal(err)
		}
		rA, err := moderation.Authenticate(ctx, mod, nameA, password)
		if err != nil || rA.Role != "MODERATOR" || rA.ID != idA {
			t.Fatalf("authenticate: %+v %v", rA, err)
		}
		decision, err := moderation.Decide(ctx, mod, rA, "post", postID, "REMOVED")
		if err != nil {
			t.Fatal(err)
		}
		var status, actor string
		if err = f.boot.QueryRow(ctx, `SELECT p.status,au.actor::text FROM social.posts p JOIN safety.audit au ON au.id=$2 WHERE p.id=$1`, postID, decision).Scan(&status, &actor); err != nil {
			t.Fatal(err)
		}
		if status != "REMOVED" || actor != idA {
			t.Fatalf("decision not attributed: status=%s actor=%s", status, actor)
		}
		if err = f.boot.QueryRow(ctx, `INSERT INTO safety.appeals(user_id,decision_id,reason) VALUES($1,$2,'This removal was mistaken') RETURNING appeal_id`, f.users[0], decision).Scan(&appeal); err != nil {
			t.Fatal(err)
		}
		if err = moderation.ReviewAppeal(ctx, mod, rA, appeal, "UPHELD"); !errors.Is(err, moderation.ErrSelfReview) {
			t.Fatalf("self review: want ErrSelfReview, got %v", err)
		}
		rB, err := moderation.Authenticate(ctx, mod, nameB, password)
		if err != nil {
			t.Fatal(err)
		}
		if err = moderation.ReviewAppeal(ctx, mod, rB, appeal, "OVERTURNED"); err != nil {
			t.Fatal(err)
		}
		var appealStatus, reviewedBy string
		if err = f.boot.QueryRow(ctx, `SELECT a.status,a.reviewed_by::text,p.status FROM safety.appeals a JOIN social.posts p ON p.id=$2 WHERE a.appeal_id=$1`, appeal, postID).Scan(&appealStatus, &reviewedBy, &status); err != nil {
			t.Fatal(err)
		}
		if appealStatus != "OVERTURNED" || reviewedBy != idB || status != "LIVE" {
			t.Fatalf("overturn: appeal=%s reviewer=%s post=%s", appealStatus, reviewedBy, status)
		}
		if err = moderation.ReviewAppeal(ctx, mod, rB, appeal, "UPHELD"); !errors.Is(err, moderation.ErrAppealNotOpen) {
			t.Fatalf("closed appeal: want ErrAppealNotOpen, got %v", err)
		}
		if _, err = f.boot.Exec(ctx, `UPDATE safety.reviewers SET decisions_count=$2 WHERE reviewer_id=$1`, idA, moderation.MaxDecisionsPerReviewer); err != nil {
			t.Fatal(err)
		}
		if _, err = moderation.Decide(ctx, mod, rA, "post", postID, "LIVE"); !errors.Is(err, moderation.ErrRotationRequired) {
			t.Fatalf("rotation cap: want ErrRotationRequired, got %v", err)
		}
		if _, err = f.boot.Exec(ctx, `UPDATE safety.reviewers SET decisions_count=0 WHERE reviewer_id=$1`, idA); err != nil {
			t.Fatal(err)
		}
		if _, err = moderation.Decide(ctx, mod, rA, "post", postID, "LIVE"); err != nil {
			t.Fatalf("decision after rotation reset: %v", err)
		}
		if _, err = moderation.Authenticate(ctx, mod, "", ""); !errors.Is(err, moderation.ErrInvalidCredentials) {
			t.Fatalf("empty credentials: want ErrInvalidCredentials, got %v", err)
		}
		if _, err = moderation.Authenticate(ctx, mod, "nobody-"+suffix, password); !errors.Is(err, moderation.ErrInvalidCredentials) {
			t.Fatalf("unknown reviewer: want ErrInvalidCredentials, got %v", err)
		}
		for _, bad := range []struct{ name, role, pw string }{{"ab", "MODERATOR", password}, {nameA, "ADMIN", password}, {nameA, "MODERATOR", "short"}} {
			if _, err = moderation.CreateReviewer(ctx, f.boot, bad.name, bad.role, bad.pw); !errors.Is(err, moderation.ErrInvalidInput) {
				t.Fatalf("bad reviewer %+v: want ErrInvalidInput, got %v", bad, err)
			}
		}
		if _, err = moderation.CreateReviewer(ctx, f.boot, nameA, "MODERATOR", password); !errors.Is(err, moderation.ErrExists) {
			t.Fatalf("duplicate reviewer: want ErrExists, got %v", err)
		}
		if err = moderation.SetReviewerActive(ctx, f.boot, "", true); !errors.Is(err, moderation.ErrInvalidInput) {
			t.Fatalf("empty reviewer id: want ErrInvalidInput, got %v", err)
		}
		if err = moderation.SetReviewerActive(ctx, f.boot, "no-such-"+suffix, true); !errors.Is(err, moderation.ErrResourceNotFound) {
			t.Fatalf("unknown reviewer id: want ErrResourceNotFound, got %v", err)
		}
		for _, bad := range []struct{ kind, resource, decision string }{{"group", postID, "LIVE"}, {"post", postID, "DELETED"}, {"post", "not-a-uuid", "LIVE"}} {
			if _, err = moderation.Decide(ctx, mod, rA, bad.kind, bad.resource, bad.decision); !errors.Is(err, moderation.ErrInvalidInput) {
				t.Fatalf("bad decision %+v: want ErrInvalidInput, got %v", bad, err)
			}
		}
		if _, err = moderation.Decide(ctx, mod, rA, "post", uuid.NewString(), "LIVE"); !errors.Is(err, moderation.ErrResourceNotFound) {
			t.Fatalf("missing resource: want ErrResourceNotFound, got %v", err)
		}
		if err = moderation.ReviewAppeal(ctx, mod, rB, "not-a-uuid", "UPHELD"); !errors.Is(err, moderation.ErrInvalidInput) {
			t.Fatalf("bad appeal id: want ErrInvalidInput, got %v", err)
		}
		if err = moderation.ReviewAppeal(ctx, mod, rB, uuid.NewString(), "OVERTURNED"); !errors.Is(err, moderation.ErrAppealNotFound) {
			t.Fatalf("missing appeal: want ErrAppealNotFound, got %v", err)
		}
		if err = moderation.SetReviewerActive(ctx, f.boot, nameB, false); err != nil {
			t.Fatal(err)
		}
		if _, err = moderation.Decide(ctx, mod, rB, "post", postID, "REMOVED"); !errors.Is(err, moderation.ErrInvalidCredentials) {
			t.Fatalf("disabled reviewer decision: want ErrInvalidCredentials, got %v", err)
		}
		if err = moderation.SetReviewerActive(ctx, f.boot, nameB, true); err != nil {
			t.Fatal(err)
		}
	})
}
