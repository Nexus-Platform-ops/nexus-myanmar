package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
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
