package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Meet-Safe: trusted contacts plus check-ins. A check-in is never auto-confirmed;
// contacts see it as overdue from due_at, and only the owner can resolve it.
const (
	trustedContactLimit = 10
	openCheckinLimit    = 5
	checkinStatusSQL    = `CASE WHEN status='PENDING' AND due_at<=now() THEN 'OVERDUE' ELSE status END`
)

func (s *Server) trustedContacts(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.DB.Query(r.Context(), `SELECT json_build_object('user_id',c.contact_id,'real_name',u.real_name,'label',c.label,'created_at',c.created_at) FROM safety.trusted_contacts c JOIN social.user_profiles u ON u.user_id=c.contact_id JOIN identity.accounts a ON a.id=c.contact_id WHERE c.user_id=$1 AND NOT a.restricted AND NOT a.ghost AND NOT EXISTS(SELECT 1 FROM safety.blocks b WHERE (b.user_id=$1 AND b.target_id=c.contact_id) OR (b.user_id=c.contact_id AND b.target_id=$1)) ORDER BY c.created_at,c.contact_id`, who(r).ID)
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

func (s *Server) addTrustedContact(w http.ResponseWriter, r *http.Request) error {
	if err := s.mayWrite(r.Context(), who(r).ID); err != nil {
		return err
	}
	var b struct {
		UserID string `json:"user_id"`
		Label  string `json:"label"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	b.Label = strings.TrimSpace(b.Label)
	if !validID(b.UserID) || b.UserID == who(r).ID || !textOK(b.Label, 40) {
		return fail(400, "invalid_trusted_contact")
	}
	var restricted, ghost bool
	err := s.DB.QueryRow(r.Context(), `SELECT restricted,ghost FROM identity.accounts WHERE id=$1`, b.UserID).Scan(&restricted, &ghost)
	if err == pgx.ErrNoRows || restricted || ghost {
		return fail(404, "contact_unavailable")
	}
	if err != nil {
		return err
	}
	if blocked, err := s.blocked(r.Context(), who(r).ID, b.UserID); err != nil {
		return err
	} else if blocked {
		return fail(403, "contact_blocked")
	}
	var count int
	if err = s.DB.QueryRow(r.Context(), `SELECT count(*) FROM safety.trusted_contacts WHERE user_id=$1 AND contact_id<>$2`, who(r).ID, b.UserID).Scan(&count); err != nil {
		return err
	}
	if count >= trustedContactLimit {
		return fail(409, "trusted_contact_limit")
	}
	if _, err = s.DB.Exec(r.Context(), `INSERT INTO safety.trusted_contacts(user_id,contact_id,label) VALUES($1,$2,$3) ON CONFLICT(user_id,contact_id) DO UPDATE SET label=excluded.label`, who(r).ID, b.UserID, b.Label); err != nil {
		return err
	}
	write(w, 201, map[string]any{"user_id": b.UserID, "label": b.Label})
	return nil
}

func (s *Server) removeTrustedContact(w http.ResponseWriter, r *http.Request) error {
	id, err := requiredID(r)
	if err != nil {
		return err
	}
	tag, err := s.DB.Exec(r.Context(), `DELETE FROM safety.trusted_contacts WHERE user_id=$1 AND contact_id=$2`, who(r).ID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fail(404, "contact_not_found")
	}
	write(w, 204, nil)
	return nil
}

func (s *Server) createCheckin(w http.ResponseWriter, r *http.Request) error {
	if err := s.mayWrite(r.Context(), who(r).ID); err != nil {
		return err
	}
	var b struct {
		EventID    string `json:"event_id"`
		PlaceLabel string `json:"place_label"`
		Minutes    int    `json:"minutes"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	b.PlaceLabel = strings.TrimSpace(b.PlaceLabel)
	if !textOK(b.PlaceLabel, 80) || b.Minutes < 5 || b.Minutes > 1440 {
		return fail(400, "invalid_checkin")
	}
	var event any
	if b.EventID != "" {
		if !validID(b.EventID) {
			return fail(400, "invalid_event")
		}
		var live bool
		err := s.DB.QueryRow(r.Context(), `SELECT status='LIVE' FROM social.events WHERE id=$1`, b.EventID).Scan(&live)
		if err == pgx.ErrNoRows || !live {
			return fail(400, "invalid_event")
		}
		if err != nil {
			return err
		}
		event = b.EventID
	}
	if ok, err := s.limit(r.Context(), "checkin:"+who(r).ID, 10, time.Hour); err != nil {
		return err
	} else if !ok {
		return fail(429, "checkin_rate_limited")
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var open int
	if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM safety.checkins WHERE user_id=$1 AND status='PENDING' AND due_at>now()`, who(r).ID).Scan(&open); err != nil {
		return err
	}
	if open >= openCheckinLimit {
		return fail(409, "too_many_open_checkins")
	}
	var id string
	var due time.Time
	if err = tx.QueryRow(r.Context(), `INSERT INTO safety.checkins(user_id,event_id,place_label,due_at) VALUES($1,$2,$3,now()+make_interval(mins=>$4)) RETURNING id,due_at`, who(r).ID, event, b.PlaceLabel, b.Minutes).Scan(&id, &due); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO social.notifications(user_id,category,kind,resource_id) SELECT contact_id,'SAFETY','CHECKIN_SCHEDULED',$1 FROM safety.trusted_contacts WHERE user_id=$2`, id, who(r).ID); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	write(w, 201, map[string]any{"id": id, "due_at": due, "status": "PENDING"})
	return nil
}

func (s *Server) confirmCheckin(w http.ResponseWriter, r *http.Request) error {
	return s.resolveCheckin(w, r, "CONFIRMED")
}

func (s *Server) cancelCheckin(w http.ResponseWriter, r *http.Request) error {
	return s.resolveCheckin(w, r, "CANCELLED")
}

func (s *Server) resolveCheckin(w http.ResponseWriter, r *http.Request, status string) error {
	id, err := requiredID(r)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var owner string
	var current string
	err = tx.QueryRow(r.Context(), `SELECT user_id,`+checkinStatusSQL+` FROM safety.checkins WHERE id=$1 FOR UPDATE`, id).Scan(&owner, &current)
	if err == pgx.ErrNoRows || (err == nil && owner != who(r).ID) {
		return fail(404, "checkin_not_found")
	}
	if err != nil {
		return err
	}
	if current != "PENDING" && current != "OVERDUE" {
		return fail(409, "checkin_resolved")
	}
	if _, err = tx.Exec(r.Context(), `UPDATE safety.checkins SET status=$2,resolved_at=now() WHERE id=$1`, id, status); err != nil {
		return err
	}
	if status == "CONFIRMED" {
		if _, err = tx.Exec(r.Context(), `INSERT INTO social.notifications(user_id,category,kind,resource_id) SELECT contact_id,'SAFETY','CHECKIN_CONFIRMED',$1 FROM safety.trusted_contacts WHERE user_id=$2`, id, owner); err != nil {
			return err
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	write(w, 200, map[string]any{"id": id, "status": status})
	return nil
}

func (s *Server) checkins(w http.ResponseWriter, r *http.Request) error {
	role := r.URL.Query().Get("role")
	if role == "" {
		role = "owner"
	}
	var rows pgx.Rows
	var err error
	switch role {
	case "owner":
		rows, err = s.DB.Query(r.Context(), `SELECT json_build_object('id',c.id,'owner_id',c.user_id,'owner',u.real_name,'event_id',c.event_id,'place_label',c.place_label,'due_at',c.due_at,'status',`+checkinStatusSQL+`,'created_at',c.created_at,'resolved_at',c.resolved_at) FROM safety.checkins c JOIN social.user_profiles u ON u.user_id=c.user_id WHERE c.user_id=$1 ORDER BY c.created_at DESC,c.id LIMIT 50`, who(r).ID)
	case "contact":
		// Contacts see only check-ins of accounts that named them, and only for
		// the last 30 days.
		rows, err = s.DB.Query(r.Context(), `SELECT json_build_object('id',c.id,'owner_id',c.user_id,'owner',u.real_name,'event_id',c.event_id,'place_label',c.place_label,'due_at',c.due_at,'status',`+checkinStatusSQL+`,'created_at',c.created_at,'resolved_at',c.resolved_at) FROM safety.checkins c JOIN safety.trusted_contacts t ON t.user_id=c.user_id AND t.contact_id=$1 JOIN social.user_profiles u ON u.user_id=c.user_id JOIN identity.accounts a ON a.id=c.user_id WHERE NOT a.restricted AND NOT a.ghost AND c.created_at>now()-interval '30 days' AND NOT EXISTS(SELECT 1 FROM safety.blocks b WHERE (b.user_id=$1 AND b.target_id=c.user_id) OR (b.user_id=c.user_id AND b.target_id=$1)) ORDER BY c.created_at DESC,c.id LIMIT 50`, who(r).ID)
	default:
		return fail(400, "invalid_role")
	}
	if err != nil {
		return err
	}
	out, err := jsonRows(rows)
	if err != nil {
		return err
	}
	write(w, 200, map[string]any{"items": out, "role": role})
	return nil
}
