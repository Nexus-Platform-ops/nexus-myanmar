package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Config struct{ Env, Addr, DatabaseURL, RedisURL, JWTSecret, DataKey, OTPSecret, SMSURL, SMSToken, Origin string }

func EnvConfig() Config {
	return Config{env("APP_ENV", "development"), env("LISTEN_ADDR", ":8080"), os.Getenv("DATABASE_URL"), env("REDIS_URL", "redis://localhost:6379/0"), os.Getenv("JWT_SECRET"), os.Getenv("DATA_KEY"), os.Getenv("OTP_SECRET"), os.Getenv("SMS_URL"), os.Getenv("SMS_TOKEN"), env("ALLOWED_ORIGIN", "http://localhost:3000")}
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func (c Config) Validate() error {
	if c.Env != "development" && c.Env != "test" && c.Env != "staging" && c.Env != "production" {
		return errors.New("invalid APP_ENV")
	}
	for _, s := range []string{c.JWTSecret, c.DataKey, c.OTPSecret} {
		if len(s) < 32 {
			return errors.New("three independent secrets of at least 32 bytes required")
		}
	}
	if c.JWTSecret == c.DataKey || c.JWTSecret == c.OTPSecret || c.DataKey == c.OTPSecret {
		return errors.New("secrets must be independent")
	}
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL required")
	}
	if c.Env != "development" && c.Env != "test" {
		if c.SMSURL == "" || c.SMSToken == "" || !strings.HasPrefix(c.SMSURL, "https://") {
			return errors.New("HTTPS SMS adapter required outside development")
		}
		for _, s := range []string{c.JWTSecret, c.DataKey, c.OTPSecret} {
			if strings.Contains(s, "development") {
				return errors.New("development secret forbidden")
			}
		}
	}
	// Deployment is deliberately fail-closed: unimplemented device attestation and
	// KMS integrations must not be bypassed by setting a ceremonial environment flag.
	if c.Env == "production" {
		return errors.New("production release blocked: device attestation, KMS, independent pentest and device gates outstanding; see docs/READINESS.md")
	}
	return nil
}

type Server struct {
	DB    *pgxpool.Pool
	Redis *redis.Client
	Cfg   Config
	mux   *http.ServeMux
}
type principal struct{ ID, Session string }
type contextKey struct{}
type apiError struct {
	Status int
	Code   string
}

func (e apiError) Error() string         { return e.Code }
func fail(status int, code string) error { return apiError{status, code} }

type handler func(http.ResponseWriter, *http.Request) error

func (s *Server) wrap(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			var e apiError
			if errors.As(err, &e) {
				write(w, e.Status, map[string]any{"error": e.Code})
			} else {
				slog.Error("request_failed")
				write(w, 500, map[string]any{"error": "internal_error"})
			}
		}
	}
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	var raw json.RawMessage
	if err := d.Decode(&raw); err != nil {
		return fail(400, "invalid_json")
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' || d.Decode(&struct{}{}) != io.EOF {
		return fail(400, "one_json_object_required")
	}
	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	if err := strict.Decode(v); err != nil {
		return fail(400, "invalid_json")
	}
	return nil
}
func who(r *http.Request) principal { p, _ := r.Context().Value(contextKey{}).(principal); return p }
func validID(s string) bool         { _, err := uuid.Parse(s); return err == nil }
func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func keyed(key, s string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}
func New(ctx context.Context, c Config) (*Server, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	db, err := pgxpool.New(ctx, c.DatabaseURL)
	if err != nil {
		return nil, err
	}
	o, err := redis.ParseURL(c.RedisURL)
	if err != nil {
		db.Close()
		return nil, err
	}
	s := &Server{DB: db, Redis: redis.NewClient(o), Cfg: c, mux: http.NewServeMux()}
	s.routes()
	return s, nil
}
func (s *Server) Close()                          { s.DB.Close(); _ = s.Redis.Close() }
func (s *Server) route(pattern string, h handler) { s.mux.HandleFunc(pattern, s.wrap(h)) }
func (s *Server) protected(pattern string, h handler) {
	s.mux.HandleFunc(pattern, s.wrap(func(w http.ResponseWriter, r *http.Request) error {
		p, err := s.authenticate(r)
		if err != nil {
			return err
		}
		return h(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, p)))
	}))
}
func (s *Server) routes() {
	s.route("GET /health/live", func(w http.ResponseWriter, r *http.Request) error {
		write(w, 200, map[string]any{"status": "alive"})
		return nil
	})
	s.route("GET /health/ready", func(w http.ResponseWriter, r *http.Request) error {
		ctx, c := context.WithTimeout(r.Context(), time.Second)
		defer c()
		if s.DB.Ping(ctx) != nil || s.Redis.Ping(ctx).Err() != nil {
			return fail(503, "dependency_unavailable")
		}
		var v int
		if s.DB.QueryRow(ctx, "SELECT version FROM identity.schema_version").Scan(&v) != nil || v < 8 {
			return fail(503, "schema_mismatch")
		}
		write(w, 200, map[string]any{"status": "ready", "release": "development-baseline"})
		return nil
	})
	s.route("POST /v1/auth/otp/request", s.otpRequest)
	s.route("POST /v1/auth/otp/verify", s.otpVerify)
	s.route("POST /v1/auth/refresh", s.refresh)
	s.protected("GET /v1/auth/sessions", s.sessions)
	s.protected("DELETE /v1/auth/sessions/{id}", s.revokeSession)
	s.protected("GET /v1/profile", s.getProfile)
	s.protected("PUT /v1/profile", s.putProfile)
	s.protected("GET /v1/dating-profile/preview", s.preview)
	s.protected("PUT /v1/dating-profile", s.putDating)
	s.protected("POST /v1/location", s.location)
	s.protected("POST /v1/location:patch", s.location)
	s.protected("GET /v1/discover", s.discover)
	s.protected("GET /v1/lookaround", s.lookaround)
	s.protected("GET /v1/radar", s.radar)
	s.protected("GET /v1/radar/blips/{id}", s.blip)
	s.protected("POST /v1/actions/swipe", s.swipe)
	s.protected("GET /v1/feed", s.feed)
	s.protectedIdempotent("POST /v1/posts", s.createPost)
	s.protected("POST /v1/posts/{id}/reactions", s.react)
	s.protected("POST /v1/posts/{id}/bookmark", s.bookmark)
	s.protected("GET /v1/posts/{id}/comments", s.comments)
	s.protected("POST /v1/posts/{id}/comments", s.createComment)
	s.protectedIdempotent("POST /v1/posts/{id}/shares", s.share)
	s.protected("GET /v1/search", s.search)
	s.protected("GET /v1/search/recent", s.recentSearches)
	s.protected("DELETE /v1/search/recent", s.clearRecentSearches)
	s.protected("GET /v1/friends", s.friends)
	s.protected("POST /v1/friends", s.addFriend)
	s.protected("DELETE /v1/friends/{id}", s.removeFriend)
	s.protected("GET /v1/events", s.events)
	s.protected("POST /v1/events", s.createEvent)
	s.protected("POST /v1/events/{id}/rsvp", s.rsvp)
	s.protected("GET /v1/groups", s.groups)
	s.protected("POST /v1/groups", s.createGroup)
	s.protected("POST /v1/groups/{id}/join", s.joinGroup)
	s.protected("GET /v1/chats", s.rooms)
	s.protected("POST /v1/chats", s.createRoom)
	s.protected("GET /v1/chats/{id}/messages", s.messages)
	s.protected("POST /v1/chats/{id}/messages", s.send)
	s.protected("POST /v1/chats/{id}/read", s.readMessages)
	s.protected("GET /v1/notifications", s.notifications)
	s.protected("GET /v1/notifications/preferences", s.notificationPrefs)
	s.protected("PUT /v1/notifications/preferences", s.putNotificationPrefs)
	s.protected("POST /v1/notifications/{id}/read", s.readNotification)
	s.protectedIdempotent("POST /v1/safety/report", s.report)
	s.protected("GET /v1/safety/reports", s.reports)
	s.protected("POST /v1/safety/appeals", s.createAppeal)
	s.protected("GET /v1/safety/appeals", s.appeals)
	s.protected("POST /v1/safety/block", s.block)
	s.protected("GET /v1/safety/blocks", s.blocks)
	s.protected("DELETE /v1/safety/blocks/{id}", s.unblock)
	s.protected("POST /v1/safety/ghost", s.ghost)
	s.protected("POST /v1/safety/capture-alert", s.capture)
	s.protected("GET /v1/account/export", s.export)
	s.protected("DELETE /v1/account", s.deleteAccount)
	s.protected("POST /v1/realtime/ticket", s.ticket)
	s.route("GET /v1/ws", s.websocket)
	for _, p := range []string{"POST /v1/calls", "POST /v1/media/upload", "POST /v1/verification", "POST /v1/nexus-plus"} {
		s.protected(p, func(w http.ResponseWriter, r *http.Request) error { return fail(503, "integration_not_configured") })
	}
	s.route("/", func(w http.ResponseWriter, r *http.Request) error { return fail(404, "not_found") })
}
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		if o := r.Header.Get("Origin"); o != "" {
			if o != s.Cfg.Origin {
				write(w, 403, map[string]any{"error": "origin_denied"})
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", o)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,Idempotency-Key")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		defer func() {
			if recover() != nil {
				slog.Error("request_panic")
				write(w, 500, map[string]any{"error": "internal_error"})
			}
		}()
		// Raw IP, path parameters, queries, bodies, tokens and PII are never logged.
		if !strings.HasPrefix(r.URL.Path, "/health/") {
			host, _, _ := net.SplitHostPort(r.RemoteAddr)
			ok, err := s.limit(r.Context(), "ip:"+keyed(s.Cfg.OTPSecret, host), 300, time.Minute)
			if err != nil {
				write(w, 503, map[string]any{"error": "rate_limit_unavailable"})
				return
			}
			if !ok {
				write(w, 429, map[string]any{"error": "rate_limited"})
				return
			}
		}
		s.mux.ServeHTTP(w, r)
	})
}
func (s *Server) limit(ctx context.Context, key string, n int, ttl time.Duration) (bool, error) {
	v, err := s.Redis.Eval(ctx, `local v=redis.call('INCR',KEYS[1]);if v==1 then redis.call('PEXPIRE',KEYS[1],ARGV[1]) end;return v`, []string{"limit:" + key}, ttl.Milliseconds()).Int64()
	return v <= int64(n), err
}
func (s *Server) authenticate(r *http.Request) (principal, error) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return principal{}, fail(401, "authentication_required")
	}
	c := &jwt.RegisteredClaims{}
	t, err := jwt.ParseWithClaims(strings.TrimPrefix(h, "Bearer "), c, func(t *jwt.Token) (any, error) { return []byte(s.Cfg.JWTSecret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("nexus"), jwt.WithAudience("nexus-app"), jwt.WithExpirationRequired())
	if err != nil || !t.Valid || !validID(c.Subject) || !validID(c.ID) {
		return principal{}, fail(401, "invalid_session")
	}
	var ok bool
	err = s.DB.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM identity.sessions WHERE id=$1 AND user_id=$2 AND expires_at>now())", c.ID, c.Subject).Scan(&ok)
	if err != nil {
		return principal{}, err
	}
	if !ok {
		return principal{}, fail(401, "session_revoked")
	}
	return principal{c.Subject, c.ID}, nil
}
func (s *Server) tokens(user, session string) (string, error) {
	now := time.Now()
	c := jwt.RegisteredClaims{Issuer: "nexus", Subject: user, Audience: jwt.ClaimStrings{"nexus-app"}, ID: session, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute))}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(s.Cfg.JWTSecret))
}
func (s *Server) mayWrite(ctx context.Context, user string) error {
	var verified, restricted, ghost bool
	err := s.DB.QueryRow(ctx, "SELECT phone_verified,restricted,ghost FROM identity.accounts WHERE id=$1", user).Scan(&verified, &restricted, &ghost)
	if err != nil {
		return err
	}
	if !verified || restricted || ghost {
		return fail(403, "account_read_only")
	}
	return nil
}
func (s *Server) blocked(ctx context.Context, a, b string) (bool, error) {
	var v bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM safety.blocks WHERE (user_id=$1 AND target_id=$2) OR (user_id=$2 AND target_id=$1))`, a, b).Scan(&v)
	return v, err
}
func requiredID(r *http.Request) (string, error) {
	id := r.PathValue("id")
	if !validID(id) {
		return "", fail(400, "invalid_id")
	}
	return id, nil
}
func textOK(t string, max int) bool {
	return len([]rune(strings.TrimSpace(t))) > 0 && len([]rune(t)) <= max
}
func contains(a []string, b string) bool {
	for _, v := range a {
		if v == b {
			return true
		}
	}
	return false
}
func closeRows(err error) error {
	if err != nil {
		return fmt.Errorf("database operation: %w", err)
	}
	return nil
}
