package api

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

func (s *Server) otpRequest(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Phone string `json:"phone"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if !phonePattern.MatchString(b.Phone) {
		return fail(400, "invalid_e164_phone")
	}
	hash := keyed(s.Cfg.OTPSecret, b.Phone)
	ok, err := s.limit(r.Context(), "otp:"+hash, 1, time.Minute)
	if err != nil {
		return err
	}
	if !ok {
		w.Header().Set("Retry-After", "60")
		return fail(429, "otp_cooldown")
	}
	ok, err = s.limit(r.Context(), "otp-hour:"+hash, 5, time.Hour)
	if err != nil {
		return err
	}
	if !ok {
		return fail(429, "otp_hourly_limit")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return err
	}
	code := n.String()
	code = strings.Repeat("0", 6-len(code)) + code
	var id string
	err = s.DB.QueryRow(r.Context(), `INSERT INTO identity.otp(phone_hash,phone_cipher,code_hash,expires_at) VALUES($1,pgp_sym_encrypt($2,$3),$4,now()+interval '5 minutes') RETURNING id`, hash, b.Phone, s.Cfg.DataKey, keyed(s.Cfg.OTPSecret, hash+":"+code)).Scan(&id)
	if err != nil {
		return err
	}
	if s.Cfg.Env != "development" && s.Cfg.Env != "test" {
		// SMS webhook adapter contract: {phone,code,ttl_seconds}; provider must be private,
		// authenticated, no request-body logging and map this schema to the real SMS vendor.
		raw, _ := json.Marshal(map[string]any{"phone": b.Phone, "code": code, "ttl_seconds": 300})
		req, err := http.NewRequestWithContext(r.Context(), "POST", s.Cfg.SMSURL, bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+s.Cfg.SMSToken)
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.Do(req)
		if err != nil {
			return fail(503, "sms_unavailable")
		}
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fail(503, "sms_unavailable")
		}
	}
	out := map[string]any{"challenge_id": id, "retry_after": 60, "expires_in": 300}
	if s.Cfg.Env == "development" || s.Cfg.Env == "test" {
		out["development_code"] = code
	}
	write(w, 202, out)
	return nil
}
func adult(dob time.Time, now time.Time) bool {
	return !dob.After(now.AddDate(-18, 0, 0)) && !dob.Before(now.AddDate(-120, 0, 0))
}
func (s *Server) otpVerify(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Challenge string `json:"challenge_id"`
		Code      string `json:"code"`
		DOB       string `json:"dob"`
		Device    string `json:"device"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if !validID(b.Challenge) || len(b.Code) != 6 || !textOK(b.Device, 80) {
		return fail(400, "invalid_challenge")
	}
	dob, err := time.Parse("2006-01-02", b.DOB)
	if err != nil || !adult(dob, time.Now().UTC()) {
		return fail(400, "age_must_be_18_or_over")
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var ph, ch string
	var cipher []byte
	var attempts int
	var expires time.Time
	var consumed bool
	err = tx.QueryRow(r.Context(), `SELECT phone_hash,phone_cipher,code_hash,attempts,expires_at,consumed FROM identity.otp WHERE id=$1 FOR UPDATE`, b.Challenge).Scan(&ph, &cipher, &ch, &attempts, &expires, &consumed)
	if err == pgx.ErrNoRows {
		return fail(401, "invalid_otp")
	}
	if err != nil {
		return err
	}
	if consumed || attempts >= 5 || time.Now().After(expires) {
		return fail(401, "invalid_otp")
	}
	if !hmacEqual(ch, keyed(s.Cfg.OTPSecret, ph+":"+b.Code)) {
		if _, err = tx.Exec(r.Context(), "UPDATE identity.otp SET attempts=attempts+1 WHERE id=$1", b.Challenge); err != nil {
			return err
		}
		if err = tx.Commit(r.Context()); err != nil {
			return err
		}
		return fail(401, "invalid_otp")
	}
	var user string
	// Existing DOB cannot be silently changed by a later OTP login.
	err = tx.QueryRow(r.Context(), `INSERT INTO identity.accounts(phone_hash,phone_cipher,dob,phone_verified) VALUES($1,$2,$3,true) ON CONFLICT(phone_hash) DO UPDATE SET phone_verified=true RETURNING id`, ph, cipher, dob).Scan(&user)
	if err != nil {
		return err
	}
	var pending bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM identity.deletion_requests WHERE user_id=$1)`, user).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return fail(403, "account_pending_deletion")
	}
	if _, err = tx.Exec(r.Context(), "UPDATE identity.otp SET consumed=true WHERE phone_hash=$1", ph); err != nil {
		return err
	}
	refresh := randomToken()
	var sid string
	err = tx.QueryRow(r.Context(), `INSERT INTO identity.sessions(user_id,refresh_hash,device_label,expires_at) VALUES($1,$2,$3,now()+interval '30 days') RETURNING id`, user, digest(refresh), b.Device).Scan(&sid)
	if err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	access, err := s.tokens(user, sid)
	if err != nil {
		return err
	}
	write(w, 200, map[string]any{"access_token": access, "refresh_token": refresh, "user_id": user, "expires_in": 900})
	return nil
}
func hmacEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var x byte
	for i := range a {
		x |= a[i] ^ b[i]
	}
	return x == 0
}
func (s *Server) refresh(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Refresh string `json:"refresh_token"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if len(b.Refresh) != 64 {
		return fail(401, "invalid_refresh")
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var id, user string
	err = tx.QueryRow(r.Context(), `SELECT id,user_id FROM identity.sessions WHERE refresh_hash=$1 AND expires_at>now() FOR UPDATE`, digest(b.Refresh)).Scan(&id, &user)
	if err == pgx.ErrNoRows {
		return fail(401, "invalid_refresh")
	}
	if err != nil {
		return err
	}
	next := randomToken()
	if _, err = tx.Exec(r.Context(), `UPDATE identity.sessions SET refresh_hash=$1 WHERE id=$2`, digest(next), id); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	access, err := s.tokens(user, id)
	if err != nil {
		return err
	}
	write(w, 200, map[string]any{"access_token": access, "refresh_token": next, "expires_in": 900})
	return nil
}
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.DB.Query(r.Context(), `SELECT id,device_label,expires_at FROM identity.sessions WHERE user_id=$1 AND expires_at>now() ORDER BY created_at DESC`, who(r).ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, d string
		var e time.Time
		if err := rows.Scan(&id, &d, &e); err != nil {
			return err
		}
		out = append(out, map[string]any{"id": id, "device": d, "expires_at": e, "current": id == who(r).Session})
	}
	if err = rows.Err(); err != nil {
		return err
	}
	write(w, 200, map[string]any{"items": out})
	return nil
}
func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) error {
	id, err := requiredID(r)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(r.Context(), "DELETE FROM identity.sessions WHERE id=$1 AND user_id=$2", id, who(r).ID)
	if err != nil {
		return err
	}
	write(w, 204, nil)
	return nil
}
func (s *Server) getProfile(w http.ResponseWriter, r *http.Request) error {
	var name, town, bio, avatar string
	var tier int
	var ghost bool
	err := s.DB.QueryRow(r.Context(), `SELECT p.real_name,p.township,p.bio,p.avatar,a.tier,a.ghost FROM social.user_profiles p JOIN identity.accounts a ON a.id=p.user_id WHERE p.user_id=$1`, who(r).ID).Scan(&name, &town, &bio, &avatar, &tier, &ghost)
	if err == pgx.ErrNoRows {
		return fail(404, "profile_not_created")
	}
	if err != nil {
		return err
	}
	write(w, 200, map[string]any{"real_name": name, "township": town, "bio": bio, "avatar": avatar, "tier": tier, "ghost": ghost})
	return nil
}
func validTown(t string) bool {
	return contains([]string{"kamayut", "bahan", "chanayethazan", "bangkok", "chiang-mai"}, t)
}
func (s *Server) putProfile(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Name string `json:"real_name"`
		Town string `json:"township"`
		Bio  string `json:"bio"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if !textOK(b.Name, 80) || !validTown(b.Town) || len([]rune(b.Bio)) > 500 {
		return fail(400, "invalid_profile")
	}
	_, err := s.DB.Exec(r.Context(), `INSERT INTO social.user_profiles(user_id,real_name,township,bio) VALUES($1,$2,$3,$4) ON CONFLICT(user_id) DO UPDATE SET real_name=excluded.real_name,township=excluded.township,bio=excluded.bio`, who(r).ID, strings.TrimSpace(b.Name), b.Town, b.Bio)
	if err != nil {
		return err
	}
	return s.getProfile(w, r)
}
func (s *Server) putDating(w http.ResponseWriter, r *http.Request) error {
	if err := s.mayWrite(r.Context(), who(r).ID); err != nil {
		return err
	}
	var b struct {
		Nickname    string   `json:"nickname"`
		Bio         string   `json:"bio"`
		Photos      []string `json:"photos"`
		Prompts     []string `json:"prompts"`
		Interests   []string `json:"interests"`
		Gender      string   `json:"gender"`
		Preferences []string `json:"preferences"`
		Min         int      `json:"age_min"`
		Max         int      `json:"age_max"`
		Hide        bool     `json:"hide_from_social_friends"`
		Radar       bool     `json:"radar_opt_in"`
		Radius      int      `json:"radius_km"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if !textOK(b.Nickname, 40) || len([]rune(b.Bio)) > 500 || len(b.Photos) > 0 || len(b.Prompts) > 3 || len(b.Interests) > 12 || !contains([]string{"1", "3", "5", "10"}, itoa(b.Radius)) || b.Min < 18 || b.Max > 120 || b.Max < b.Min || !textOK(b.Gender, 40) {
		return fail(400, "invalid_dating_profile_or_media_not_configured")
	}
	for _, x := range append(append(append([]string{}, b.Prompts...), b.Interests...), b.Preferences...) {
		if !textOK(x, 200) {
			return fail(400, "invalid_profile_field")
		}
	}
	var real string
	err := s.DB.QueryRow(r.Context(), "SELECT real_name FROM social.user_profiles WHERE user_id=$1", who(r).ID).Scan(&real)
	if err == pgx.ErrNoRows {
		return fail(409, "social_profile_required")
	}
	if err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(real), strings.TrimSpace(b.Nickname)) {
		return fail(400, "choose_nickname_not_real_name")
	}
	if b.Prompts == nil {
		b.Prompts = []string{}
	}
	if b.Interests == nil {
		b.Interests = []string{}
	}
	if b.Preferences == nil {
		b.Preferences = []string{}
	}
	_, err = s.DB.Exec(r.Context(), `INSERT INTO dating.dating_profiles(user_id,nickname,bio,prompts,interests,gender,preferences,age_min,age_max,hide_from_social_friends,radius_km,radar_until) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,CASE WHEN $12 THEN now()+interval '15 minutes' ELSE NULL END) ON CONFLICT(user_id) DO UPDATE SET nickname=excluded.nickname,bio=excluded.bio,prompts=excluded.prompts,interests=excluded.interests,gender=excluded.gender,preferences=excluded.preferences,age_min=excluded.age_min,age_max=excluded.age_max,hide_from_social_friends=excluded.hide_from_social_friends,radius_km=excluded.radius_km,radar_until=excluded.radar_until`, who(r).ID, b.Nickname, b.Bio, b.Prompts, b.Interests, b.Gender, b.Preferences, b.Min, b.Max, b.Hide, b.Radius, b.Radar)
	if err != nil {
		return err
	}
	return s.preview(w, r)
}
func itoa(n int) string {
	if n == 1 {
		return "1"
	}
	if n == 3 {
		return "3"
	}
	if n == 5 {
		return "5"
	}
	if n == 10 {
		return "10"
	}
	return ""
}
func (s *Server) preview(w http.ResponseWriter, r *http.Request) error {
	var id, nick, bio, town, gender string
	var photos, prompts, interests, prefs []string
	var age, tier, radius, min, max int
	var hide, radar bool
	err := s.DB.QueryRow(r.Context(), `SELECT d.profile_id,d.nickname,d.bio,d.photos,d.prompts,d.interests,EXTRACT(YEAR FROM age(a.dob))::int,a.tier,p.township,d.hide_from_social_friends,COALESCE(d.radar_until>now(),false),d.radius_km,d.gender,d.preferences,d.age_min,d.age_max FROM dating.dating_profiles d JOIN identity.accounts a ON a.id=d.user_id JOIN social.user_profiles p ON p.user_id=d.user_id WHERE d.user_id=$1`, who(r).ID).Scan(&id, &nick, &bio, &photos, &prompts, &interests, &age, &tier, &town, &hide, &radar, &radius, &gender, &prefs, &min, &max)
	if err == pgx.ErrNoRows {
		return fail(404, "dating_profile_not_created")
	}
	if err != nil {
		return err
	}
	write(w, 200, map[string]any{"profile_id": id, "nickname": nick, "bio": bio, "photos": photos, "prompts": prompts, "interests": interests, "age": age, "tier": tier, "township": town, "hide_from_social_friends": hide, "radar_opt_in": radar, "radius_km": radius, "gender": gender, "preferences": prefs, "age_min": min, "age_max": max})
	return nil
}
