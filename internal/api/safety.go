package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *Server) notifications(w http.ResponseWriter, r *http.Request) error {
	cat := r.URL.Query().Get("category")
	if cat != "" && !contains([]string{"ACTIVITY", "SAFETY", "FOLLOW"}, cat) {
		return fail(400, "invalid_category")
	}
	rows, err := s.DB.Query(r.Context(), `SELECT json_build_object('id',id,'category',category,'kind',kind,'resource_id',resource_id,'read_at',read_at,'created_at',created_at) FROM social.notifications WHERE user_id=$1 AND ($2='' OR category=$2) ORDER BY created_at DESC,id DESC LIMIT 100`, who(r).ID, cat)
	if err != nil {
		return err
	}
	out, err := jsonRows(rows)
	if err != nil {
		return err
	}
	write(w, 200, map[string]any{"items": out})
	return nil
}
func (s *Server) readNotification(w http.ResponseWriter, r *http.Request) error {
	id, err := requiredID(r)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(r.Context(), "UPDATE social.notifications SET read_at=COALESCE(read_at,now()) WHERE id=$1 AND user_id=$2", id, who(r).ID)
	if err != nil {
		return err
	}
	write(w, 204, nil)
	return nil
}
func (s *Server) report(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Target   string `json:"target_id"`
		Profile  string `json:"profile_id"`
		Category string `json:"category"`
		Details  string `json:"details"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if !contains([]string{"SCAM", "HARASSMENT", "UNDERAGE", "VIOLENCE", "OTHER"}, b.Category) || !textOK(b.Details, 2000) {
		return fail(400, "invalid_report")
	}
	if b.Profile != "" {
		if !validID(b.Profile) {
			return fail(400, "invalid_profile")
		}
		err := s.DB.QueryRow(r.Context(), `SELECT user_id FROM dating.dating_profiles WHERE profile_id=$1`, b.Profile).Scan(&b.Target)
		if err == pgx.ErrNoRows {
			return fail(404, "profile_unavailable")
		}
		if err != nil {
			return err
		}
	}
	if !validID(b.Target) || b.Target == who(r).ID {
		return fail(400, "invalid_target")
	}
	day, err := s.limit(r.Context(), "report-day:"+who(r).ID+":"+time.Now().UTC().Format("2006-01-02"), 5, 24*time.Hour)
	if err != nil {
		return err
	}
	if !day {
		return fail(429, "report_daily_limit")
	}
	ok, err := s.limit(r.Context(), "report:"+who(r).ID, 10, time.Hour)
	if err != nil {
		return err
	}
	if !ok {
		return fail(429, "report_limit")
	}
	severity := "MEDIUM"
	minutes := 120
	if b.Category == "UNDERAGE" || b.Category == "VIOLENCE" {
		severity = "HIGH"
		minutes = 30
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var id string
	var exists bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM identity.accounts WHERE id=$1)`, b.Target).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fail(404, "target_unavailable")
	}
	err = tx.QueryRow(r.Context(), `INSERT INTO safety.reports(reporter,target_id,category,severity,details,due_at) VALUES($1,$2,$3,$4,$5,now()+$6*interval '1 minute') RETURNING id`, who(r).ID, b.Target, b.Category, severity, b.Details, minutes).Scan(&id)
	if err != nil {
		return err
	}
	// Five distinct reporters within 24h is recorded for human review; volume
	// alone never changes a decision.
	flag, err := tx.Exec(r.Context(), `INSERT INTO safety.brigade_flags(target_id,reporters,reason)
SELECT $1,count(DISTINCT reporter),'COORDINATED_REPORTING_24H' FROM safety.reports
WHERE target_id=$1 AND created_at>now()-interval '24 hours'
HAVING count(DISTINCT reporter)>=5
ON CONFLICT (target_id) DO UPDATE SET reporters=EXCLUDED.reporters,flagged_at=now()
WHERE safety.brigade_flags.reporters < EXCLUDED.reporters`, b.Target)
	if err != nil {
		return err
	}
	if flag.RowsAffected() > 0 {
		if _, err = tx.Exec(r.Context(), `INSERT INTO safety.audit(action,resource_id) VALUES('BRIGADE_SUSPECTED',$1)`, b.Target); err != nil {
			return err
		}
	}
	// A report always hides the pair; HIGH holds all exposure pending human review.
	if _, err = tx.Exec(r.Context(), `INSERT INTO safety.blocks(user_id,target_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, who(r).ID, b.Target); err != nil {
		return err
	}
	if severity == "HIGH" {
		if _, err = tx.Exec(r.Context(), `UPDATE identity.accounts SET restricted=true WHERE id=$1`, b.Target); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO social.notifications(user_id,category,kind,resource_id) VALUES($1,'SAFETY','REPORT_RECEIVED',$2)`, who(r).ID, id); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	write(w, 202, map[string]any{"case_id": id, "severity": severity, "response_due_minutes": minutes})
	return nil
}
func (s *Server) reports(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.DB.Query(r.Context(), `SELECT json_build_object('case_id',id,'category',category,'severity',severity,'status',status,'due_at',due_at) FROM safety.reports WHERE reporter=$1 ORDER BY created_at DESC LIMIT 100`, who(r).ID)
	if err != nil {
		return err
	}
	out, err := jsonRows(rows)
	if err != nil {
		return err
	}
	write(w, 200, map[string]any{"items": out})
	return nil
}

var appealOwners = map[string]string{
	"post":    `SELECT EXISTS(SELECT 1 FROM social.posts WHERE id=$1 AND author=$2)`,
	"comment": `SELECT EXISTS(SELECT 1 FROM social.comments WHERE id=$1 AND author=$2)`,
	"event":   `SELECT EXISTS(SELECT 1 FROM social.events WHERE id=$1 AND host=$2)`,
}

func appealable(action string) (string, bool) {
	rest, ok := strings.CutPrefix(action, "MODERATE_")
	if !ok {
		return "", false
	}
	for kind := range appealOwners {
		if d, ok := strings.CutPrefix(rest, kind+"_"); ok && d == "REMOVED" {
			return kind, true
		}
	}
	return "", false
}

func (s *Server) createAppeal(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Decision int64  `json:"decision_id"`
		Reason   string `json:"reason"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if b.Decision <= 0 || !textOK(b.Reason, 2000) || len([]rune(strings.TrimSpace(b.Reason))) < 10 {
		return fail(400, "invalid_appeal")
	}
	var action string
	var resource *string
	err := s.DB.QueryRow(r.Context(), `SELECT action,resource_id FROM safety.audit WHERE id=$1`, b.Decision).Scan(&action, &resource)
	if err == pgx.ErrNoRows {
		return fail(404, "decision_unavailable")
	}
	if err != nil {
		return err
	}
	kind, ok := appealable(action)
	if !ok || resource == nil {
		return fail(404, "decision_unavailable")
	}
	var owner bool
	if err = s.DB.QueryRow(r.Context(), appealOwners[kind], *resource, who(r).ID).Scan(&owner); err != nil {
		return err
	}
	if !owner {
		return fail(404, "decision_unavailable")
	}
	var id string
	err = s.DB.QueryRow(r.Context(), `INSERT INTO safety.appeals(user_id,decision_id,reason) VALUES($1,$2,$3) RETURNING appeal_id`, who(r).ID, b.Decision, strings.TrimSpace(b.Reason)).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fail(409, "appeal_exists")
		}
		return err
	}
	write(w, 201, map[string]any{"appeal_id": id, "status": "OPEN"})
	return nil
}

func (s *Server) appeals(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.DB.Query(r.Context(), `SELECT json_build_object('appeal_id',appeal_id,'decision_id',decision_id,'reason',reason,'status',status,'reviewed_at',reviewed_at,'created_at',created_at) FROM safety.appeals WHERE user_id=$1 ORDER BY created_at DESC,appeal_id LIMIT 100`, who(r).ID)
	if err != nil {
		return err
	}
	out, err := jsonRows(rows)
	if err != nil {
		return err
	}
	write(w, 200, map[string]any{"items": out})
	return nil
}
func (s *Server) block(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Target  string `json:"target_id"`
		Profile string `json:"profile_id"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if b.Profile != "" {
		if !validID(b.Profile) {
			return fail(400, "invalid_profile")
		}
		err := s.DB.QueryRow(r.Context(), `SELECT user_id FROM dating.dating_profiles WHERE profile_id=$1`, b.Profile).Scan(&b.Target)
		if err == pgx.ErrNoRows {
			return fail(404, "profile_unavailable")
		}
		if err != nil {
			return err
		}
	}
	if !validID(b.Target) || b.Target == who(r).ID {
		return fail(400, "invalid_target")
	}
	tag, err := s.DB.Exec(r.Context(), `INSERT INTO safety.blocks(user_id,target_id) SELECT $1,id FROM identity.accounts WHERE id=$2 ON CONFLICT DO NOTHING`, who(r).ID, b.Target)
	if err != nil {
		return err
	}
	write(w, 200, map[string]any{"blocked": tag.RowsAffected() > 0})
	return nil
}
func (s *Server) blocks(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.DB.Query(r.Context(), `SELECT json_build_object('target_id',target_id) FROM safety.blocks WHERE user_id=$1 ORDER BY target_id`, who(r).ID)
	if err != nil {
		return err
	}
	out, err := jsonRows(rows)
	if err != nil {
		return err
	}
	write(w, 200, map[string]any{"items": out})
	return nil
}
func (s *Server) unblock(w http.ResponseWriter, r *http.Request) error {
	id, err := requiredID(r)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(r.Context(), "DELETE FROM safety.blocks WHERE user_id=$1 AND target_id=$2", who(r).ID, id)
	if err != nil {
		return err
	}
	write(w, 204, nil)
	return nil
}
func (s *Server) ghost(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `UPDATE identity.accounts SET ghost=$1 WHERE id=$2`, b.Enabled, who(r).ID); err != nil {
		return err
	}
	if b.Enabled {
		if _, err = tx.Exec(r.Context(), `UPDATE dating.dating_profiles SET radar_until=NULL WHERE user_id=$1`, who(r).ID); err != nil {
			return err
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	write(w, 200, map[string]any{"enabled": b.Enabled})
	return nil
}
func (s *Server) capture(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Room string `json:"room_id"`
		Kind string `json:"kind"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if !validID(b.Room) || !contains([]string{"SCREENSHOT", "RECORDING"}, b.Kind) {
		return fail(400, "invalid_capture_event")
	}
	if err := s.roomAccess(r, b.Room); err != nil {
		return err
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `INSERT INTO safety.audit(actor,action,resource_id) VALUES($1,$2,$3)`, who(r).ID, "CAPTURE_"+b.Kind, b.Room); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO social.notifications(user_id,category,kind,resource_id) SELECT user_id,'SAFETY',$2,$1 FROM chat.members WHERE room_id=$1`, b.Room, "CAPTURE_"+b.Kind); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	write(w, 202, map[string]any{"accepted": true})
	return nil
}
func (s *Server) export(w http.ResponseWriter, r *http.Request) error {
	// Export only the subject's content; excludes other participants' messages and safety investigations.
	specs := map[string]string{
		"account":        `SELECT json_build_object('id',id,'dob',dob,'phone_verified',phone_verified,'tier',tier,'created_at',created_at) FROM identity.accounts WHERE id=$1`,
		"social_profile": `SELECT to_json(p) FROM social.user_profiles p WHERE user_id=$1`,
		"dating_profile": `SELECT to_json(p) FROM dating.dating_profiles p WHERE user_id=$1`,
		"posts":          `SELECT to_json(p) FROM social.posts p WHERE author=$1`,
		"comments":       `SELECT to_json(c) FROM social.comments c WHERE author=$1`,
		"messages":       `SELECT json_build_object('id',id,'room_id',room_id,'text',pgp_sym_decrypt(body_cipher,$2),'created_at',created_at) FROM chat.messages WHERE sender=$1 AND mode='NORMAL'`,
		"reactions":      `SELECT to_json(v) FROM social.reactions v WHERE user_id=$1`,
	}
	out := map[string]any{}
	for name, q := range specs {
		args := []any{who(r).ID}
		if name == "messages" {
			args = append(args, s.Cfg.DataKey)
		}
		rows, err := s.DB.Query(r.Context(), q, args...)
		if err != nil {
			return err
		}
		v, err := jsonRows(rows)
		if err != nil {
			return err
		}
		out[name] = v
	}
	write(w, 200, out)
	return nil
}
func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Confirm string `json:"confirm"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if b.Confirm != "DELETE MY ACCOUNT" {
		return fail(400, "explicit_confirmation_required")
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `DELETE FROM private.outbox WHERE user_id=$1`, who(r).ID); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM chat.rooms WHERE id IN(SELECT room_id FROM chat.members WHERE user_id=$1)`, who(r).ID); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM identity.accounts WHERE id=$1`, who(r).ID); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	write(w, 204, nil)
	return nil
}

var _ = json.Valid
