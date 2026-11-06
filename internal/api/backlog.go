package api

import (
	"net/http"
	"time"
)

// P1-3: symmetric friend graph. social.friendships stores both directions so
// listing and removal stay simple; visibility queries already accept either.
func (s *Server) friends(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.DB.Query(r.Context(), `SELECT json_build_object('user_id',u.user_id,'real_name',u.real_name,'township',u.township,'bio',u.bio) FROM social.friendships f JOIN social.user_profiles u ON u.user_id=f.friend_id JOIN identity.accounts a ON a.id=f.friend_id WHERE f.user_id=$1 AND NOT a.restricted AND NOT a.ghost AND NOT EXISTS(SELECT 1 FROM safety.blocks b WHERE (b.user_id=$1 AND b.target_id=f.friend_id) OR (b.user_id=f.friend_id AND b.target_id=$1)) ORDER BY u.real_name,u.user_id LIMIT 200`, who(r).ID)
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

func (s *Server) addFriend(w http.ResponseWriter, r *http.Request) error {
	if err := s.mayWrite(r.Context(), who(r).ID); err != nil {
		return err
	}
	var b struct {
		UserID string `json:"user_id"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if !validID(b.UserID) || b.UserID == who(r).ID {
		return fail(400, "invalid_friend")
	}
	ok, err := s.limit(r.Context(), "friend-add:"+who(r).ID, 60, time.Hour)
	if err != nil {
		return err
	}
	if !ok {
		return fail(429, "friend_request_limit")
	}
	var available bool
	if err = s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM identity.accounts WHERE id=$1 AND phone_verified AND NOT restricted AND NOT ghost)`, b.UserID).Scan(&available); err != nil {
		return err
	}
	if !available {
		return fail(404, "friend_unavailable")
	}
	blocked, err := s.blocked(r.Context(), who(r).ID, b.UserID)
	if err != nil {
		return err
	}
	if blocked {
		return fail(403, "friend_blocked")
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `INSERT INTO social.friendships(user_id,friend_id) VALUES($1,$2),($2,$1) ON CONFLICT DO NOTHING`, who(r).ID, b.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if _, err = tx.Exec(r.Context(), `INSERT INTO social.notifications(user_id,category,kind,resource_id) VALUES($1,'FOLLOW','FRIEND_ADDED',$2)`, b.UserID, who(r).ID); err != nil {
			return err
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	write(w, 201, map[string]any{"user_id": b.UserID})
	return nil
}

func (s *Server) removeFriend(w http.ResponseWriter, r *http.Request) error {
	id, err := requiredID(r)
	if err != nil {
		return err
	}
	tag, err := s.DB.Exec(r.Context(), `DELETE FROM social.friendships WHERE (user_id=$1 AND friend_id=$2) OR (user_id=$2 AND friend_id=$1)`, who(r).ID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fail(404, "friend_not_found")
	}
	w.WriteHeader(204)
	return nil
}

// P1-4: SAFETY notices are deliberately not mutable; muting an abuse response
// would hide a safety-relevant event from the person it concerns.
const notificationPrefsSQL = `SELECT COALESCE((SELECT mute_activity FROM social.notification_prefs WHERE user_id=$1),false),COALESCE((SELECT mute_follow FROM social.notification_prefs WHERE user_id=$1),false)`

func (s *Server) notificationPrefs(w http.ResponseWriter, r *http.Request) error {
	var activity, follow bool
	err := s.DB.QueryRow(r.Context(), notificationPrefsSQL, who(r).ID).Scan(&activity, &follow)
	if err != nil {
		return err
	}
	write(w, 200, map[string]any{"activity_muted": activity, "follow_muted": follow, "safety_locked": true})
	return nil
}

func (s *Server) putNotificationPrefs(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Activity bool `json:"activity_muted"`
		Follow   bool `json:"follow_muted"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	_, err := s.DB.Exec(r.Context(), `INSERT INTO social.notification_prefs(user_id,mute_activity,mute_follow) VALUES($1,$2,$3) ON CONFLICT(user_id) DO UPDATE SET mute_activity=excluded.mute_activity,mute_follow=excluded.mute_follow,updated_at=now()`, who(r).ID, b.Activity, b.Follow)
	if err != nil {
		return err
	}
	write(w, 200, map[string]any{"activity_muted": b.Activity, "follow_muted": b.Follow, "safety_locked": true})
	return nil
}
