package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// $1 is always viewer. Social graph visibility remains social; dating identities never join it.
const postVisibility = `(p.author=$1 OR p.status='LIVE') AND p.status<>'REMOVED'
 AND NOT EXISTS(SELECT 1 FROM safety.blocks b WHERE (b.user_id=$1 AND b.target_id=p.author) OR (b.user_id=p.author AND b.target_id=$1))
 AND (p.author=$1 OR p.audience='PUBLIC' OR (p.audience='COMMUNITY' AND p.township=(SELECT township FROM social.user_profiles WHERE user_id=$1)) OR (p.audience='FRIENDS' AND EXISTS(SELECT 1 FROM social.friendships f WHERE (f.user_id=$1 AND f.friend_id=p.author) OR (f.user_id=p.author AND f.friend_id=$1))))`

func jsonRows(rows pgx.Rows) ([]json.RawMessage, error) {
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(b))
	}
	return out, rows.Err()
}
func (s *Server) visiblePost(r *http.Request, id string) (string, error) {
	var author string
	err := s.DB.QueryRow(r.Context(), `SELECT p.author FROM social.posts p WHERE p.id=$2 AND `+postVisibility, who(r).ID, id).Scan(&author)
	if err == pgx.ErrNoRows {
		return "", fail(404, "post_unavailable")
	}
	return author, err
}
func (s *Server) feed(w http.ResponseWriter, r *http.Request) error {
	c, err := parseCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}
	town := r.URL.Query().Get("township")
	if town != "" && !validTown(town) {
		return fail(400, "invalid_township")
	}
	rows, err := s.DB.Query(r.Context(), `SELECT json_build_object('id',p.id,'author_id',p.author,'author',u.real_name,'township',p.township,'body',p.body,'audience',p.audience,'status',p.status,'created_at',p.created_at,'reaction_count',(SELECT count(*) FROM social.reactions WHERE post_id=p.id),'my_reaction',(SELECT type FROM social.reactions WHERE post_id=p.id AND user_id=$1),'comment_count',(SELECT count(*) FROM social.comments WHERE post_id=p.id AND status='LIVE'),'share_count',(SELECT count(*) FROM social.shares WHERE post_id=p.id),'bookmarked',EXISTS(SELECT 1 FROM social.bookmarks WHERE post_id=p.id AND user_id=$1)) FROM social.posts p JOIN social.user_profiles u ON u.user_id=p.author WHERE `+postVisibility+` AND (p.created_at,p.id)<($2,$3::uuid) AND ($4='' OR p.township=$4) ORDER BY p.created_at DESC,p.id DESC LIMIT 21`, who(r).ID, c.Time, c.ID, town)
	if err != nil {
		return err
	}
	out, err := jsonRows(rows)
	if err != nil {
		return err
	}
	var next any
	if len(out) > 20 {
		out = out[:20]
		var last struct {
			ID   string    `json:"id"`
			Time time.Time `json:"created_at"`
		}
		if err = json.Unmarshal(out[19], &last); err != nil {
			return err
		}
		next = cursor(last.Time, last.ID)
	}
	write(w, 200, map[string]any{"items": out, "next_cursor": next})
	return nil
}
func (s *Server) createPost(w http.ResponseWriter, r *http.Request) error {
	if err := s.mayWrite(r.Context(), who(r).ID); err != nil {
		return err
	}
	var b struct {
		Body     string `json:"body"`
		Audience string `json:"audience"`
		Town     string `json:"township"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if !textOK(b.Body, 5000) || !contains([]string{"PUBLIC", "COMMUNITY", "FRIENDS"}, b.Audience) || !validTown(b.Town) {
		return fail(400, "invalid_post")
	}
	var id string
	err := s.DB.QueryRow(r.Context(), `INSERT INTO social.posts(author,body,township,audience) SELECT $1,$2,$3,$4 WHERE EXISTS(SELECT 1 FROM social.user_profiles WHERE user_id=$1) RETURNING id`, who(r).ID, b.Body, b.Town, b.Audience).Scan(&id)
	if err == pgx.ErrNoRows {
		return fail(409, "social_profile_required")
	}
	if err != nil {
		return err
	}
	write(w, 202, map[string]any{"id": id, "status": "UNDER_REVIEW"})
	return nil
}
func (s *Server) react(w http.ResponseWriter, r *http.Request) error {
	if err := s.mayWrite(r.Context(), who(r).ID); err != nil {
		return err
	}
	id, err := requiredID(r)
	if err != nil {
		return err
	}
	if _, err = s.visiblePost(r, id); err != nil {
		return err
	}
	var b struct {
		Type string `json:"type"`
	}
	if err = decode(w, r, &b); err != nil {
		return err
	}
	if !contains([]string{"LIKE", "LOVE", "HAHA", "WOW", "SAD", "ANGRY", "NONE"}, b.Type) {
		return fail(400, "invalid_reaction")
	}
	if b.Type == "NONE" {
		_, err = s.DB.Exec(r.Context(), `DELETE FROM social.reactions WHERE post_id=$1 AND user_id=$2`, id, who(r).ID)
	} else {
		_, err = s.DB.Exec(r.Context(), `INSERT INTO social.reactions(post_id,user_id,type) VALUES($1,$2,$3) ON CONFLICT(post_id,user_id) DO UPDATE SET type=excluded.type`, id, who(r).ID, b.Type)
	}
	if err != nil {
		return err
	}
	write(w, 200, map[string]any{"type": b.Type})
	return nil
}
func (s *Server) bookmark(w http.ResponseWriter, r *http.Request) error {
	id, err := requiredID(r)
	if err != nil {
		return err
	}
	if _, err = s.visiblePost(r, id); err != nil {
		return err
	}
	var b struct {
		Saved bool `json:"saved"`
	}
	if err = decode(w, r, &b); err != nil {
		return err
	}
	if b.Saved {
		_, err = s.DB.Exec(r.Context(), `INSERT INTO social.bookmarks(post_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, who(r).ID)
	} else {
		_, err = s.DB.Exec(r.Context(), `DELETE FROM social.bookmarks WHERE post_id=$1 AND user_id=$2`, id, who(r).ID)
	}
	if err != nil {
		return err
	}
	write(w, 200, map[string]any{"saved": b.Saved})
	return nil
}
func (s *Server) comments(w http.ResponseWriter, r *http.Request) error {
	id, err := requiredID(r)
	if err != nil {
		return err
	}
	if _, err = s.visiblePost(r, id); err != nil {
		return err
	}
	rows, err := s.DB.Query(r.Context(), `SELECT json_build_object('id',c.id,'parent_comment_id',c.parent_id,'author_id',c.author,'author',u.real_name,'body',c.body,'status',c.status,'created_at',c.created_at) FROM social.comments c JOIN social.user_profiles u ON u.user_id=c.author WHERE c.post_id=$2 AND (c.status='LIVE' OR c.author=$1) AND c.status<>'REMOVED' AND NOT EXISTS(SELECT 1 FROM safety.blocks b WHERE (b.user_id=$1 AND b.target_id=c.author) OR (b.user_id=c.author AND b.target_id=$1)) ORDER BY c.created_at,c.id LIMIT 100`, who(r).ID, id)
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
func (s *Server) createComment(w http.ResponseWriter, r *http.Request) error {
	if err := s.mayWrite(r.Context(), who(r).ID); err != nil {
		return err
	}
	post, err := requiredID(r)
	if err != nil {
		return err
	}
	if _, err = s.visiblePost(r, post); err != nil {
		return err
	}
	var b struct {
		Body   string  `json:"body"`
		Parent *string `json:"parent_comment_id"`
	}
	if err = decode(w, r, &b); err != nil {
		return err
	}
	if !textOK(b.Body, 2000) {
		return fail(400, "invalid_comment")
	}
	if b.Parent != nil {
		if !validID(*b.Parent) {
			return fail(400, "invalid_parent")
		}
		var root bool
		err = s.DB.QueryRow(r.Context(), `SELECT parent_id IS NULL FROM social.comments WHERE id=$1 AND post_id=$2 AND status='LIVE'`, *b.Parent, post).Scan(&root)
		if err == pgx.ErrNoRows || !root {
			return fail(400, "only_two_comment_levels_allowed")
		}
		if err != nil {
			return err
		}
	}
	var id string
	err = s.DB.QueryRow(r.Context(), `INSERT INTO social.comments(post_id,author,parent_id,body) VALUES($1,$2,$3,$4) RETURNING id`, post, who(r).ID, b.Parent, b.Body).Scan(&id)
	if err != nil {
		return err
	}
	write(w, 202, map[string]any{"id": id, "status": "UNDER_REVIEW"})
	return nil
}
func (s *Server) share(w http.ResponseWriter, r *http.Request) error {
	if err := s.mayWrite(r.Context(), who(r).ID); err != nil {
		return err
	}
	id, err := requiredID(r)
	if err != nil {
		return err
	}
	if _, err = s.visiblePost(r, id); err != nil {
		return err
	}
	var b struct {
		Mode string `json:"mode"`
		Room string `json:"room_id"`
	}
	if err = decode(w, r, &b); err != nil {
		return err
	}
	if !contains([]string{"RESHARE", "SEND_DM", "COPY_LINK"}, b.Mode) {
		return fail(400, "invalid_share")
	}
	// Forwarding non-public posts could cross audience boundaries.
	var public bool
	err = s.DB.QueryRow(r.Context(), `SELECT audience='PUBLIC' AND status='LIVE' FROM social.posts WHERE id=$1`, id).Scan(&public)
	if err != nil {
		return err
	}
	if !public {
		return fail(403, "only_public_live_posts_shareable")
	}
	if b.Mode == "SEND_DM" {
		if !validID(b.Room) {
			return fail(400, "room_required")
		}
		if err = s.roomAccess(r, b.Room); err != nil {
			return err
		}
		return fail(503, "share_delivery_integration_not_configured")
	}
	var shareID string
	err = s.DB.QueryRow(r.Context(), `INSERT INTO social.shares(post_id,user_id,mode) VALUES($1,$2,$3) RETURNING id`, id, who(r).ID, b.Mode).Scan(&shareID)
	if err != nil {
		return err
	}
	write(w, 201, map[string]any{"id": shareID, "post_id": id})
	return nil
}

type searchPage struct {
	Key string `json:"k"`
	ID  string `json:"id"`
}

func parseSearchCursor(raw string) (searchPage, error) {
	if raw == "" {
		return searchPage{}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return searchPage{}, fail(400, "invalid_cursor")
	}
	var c searchPage
	if json.Unmarshal(b, &c) != nil || c.Key == "" || !validID(c.ID) {
		return searchPage{}, fail(400, "invalid_cursor")
	}
	return c, nil
}

func searchCursor(key, id string) string {
	b, _ := json.Marshal(searchPage{key, id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) error {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	kind := r.URL.Query().Get("type")
	if kind == "" {
		kind = "people"
	}
	if len([]rune(q)) < 2 || len([]rune(q)) > 100 {
		return fail(400, "query_length_2_to_100")
	}
	c, err := parseSearchCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}
	has := c.Key != ""
	pageID := c.ID
	if !has {
		pageID = "00000000-0000-0000-0000-000000000000"
	}
	keyTime := time.Now().UTC().Add(24 * time.Hour)
	if has && kind != "people" {
		keyTime, err = time.Parse(time.RFC3339Nano, c.Key)
		if err != nil {
			return fail(400, "invalid_cursor")
		}
	}
	if kind == "events" && !has {
		keyTime = time.Time{}
	}
	pattern := "%" + strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(q, "\\", "\\\\"), "%", "\\%"), "_", "\\_") + "%"
	var rows pgx.Rows
	switch kind {
	case "people":
		rows, err = s.DB.Query(r.Context(), `SELECT json_build_object('user_id',p.user_id,'real_name',p.real_name,'township',p.township,'bio',p.bio) FROM social.user_profiles p JOIN identity.accounts a ON a.id=p.user_id WHERE p.real_name ILIKE $2 AND NOT a.restricted AND NOT a.ghost AND NOT EXISTS(SELECT 1 FROM safety.blocks b WHERE (b.user_id=$1 AND b.target_id=p.user_id) OR (b.user_id=p.user_id AND b.target_id=$1)) AND ($5=false OR (p.real_name,p.user_id)>($3,$4::uuid)) ORDER BY p.real_name,p.user_id LIMIT 21`, who(r).ID, pattern, c.Key, pageID, has)
	case "posts":
		rows, err = s.DB.Query(r.Context(), `SELECT json_build_object('id',p.id,'body',p.body,'township',p.township,'created_at',p.created_at) FROM social.posts p WHERE `+postVisibility+` AND p.body ILIKE $2 AND ($5=false OR (p.created_at,p.id)<($3::timestamptz,$4::uuid)) ORDER BY p.created_at DESC,p.id DESC LIMIT 21`, who(r).ID, pattern, keyTime, pageID, has)
	case "events":
		rows, err = s.DB.Query(r.Context(), `SELECT json_build_object('id',e.id,'title',e.title,'township',e.township,'starts_at',e.starts_at) FROM social.events e WHERE e.status='LIVE' AND e.title ILIKE $2 AND NOT EXISTS(SELECT 1 FROM safety.blocks b WHERE (b.user_id=$1 AND b.target_id=e.host) OR (b.user_id=e.host AND b.target_id=$1)) AND ($5=false OR (e.starts_at,e.id)>($3::timestamptz,$4::uuid)) ORDER BY e.starts_at,e.id LIMIT 21`, who(r).ID, pattern, keyTime, pageID, has)
	default:
		return fail(400, "unsupported_search_type")
	}
	if err != nil {
		return err
	}
	out, err := jsonRows(rows)
	if err != nil {
		return err
	}
	var next any
	if len(out) > 20 {
		out = out[:20]
		switch kind {
		case "people":
			var v struct {
				ID   string `json:"user_id"`
				Name string `json:"real_name"`
			}
			if err = json.Unmarshal(out[19], &v); err != nil {
				return err
			}
			next = searchCursor(v.Name, v.ID)
		case "posts":
			var v struct {
				ID string    `json:"id"`
				At time.Time `json:"created_at"`
			}
			if err = json.Unmarshal(out[19], &v); err != nil {
				return err
			}
			next = searchCursor(v.At.UTC().Format(time.RFC3339Nano), v.ID)
		case "events":
			var v struct {
				ID string    `json:"id"`
				At time.Time `json:"starts_at"`
			}
			if err = json.Unmarshal(out[19], &v); err != nil {
				return err
			}
			next = searchCursor(v.At.UTC().Format(time.RFC3339Nano), v.ID)
		}
	}
	if _, err = s.DB.Exec(r.Context(), `INSERT INTO social.recent_searches(user_id,query,kind) VALUES($1,$2,$3) ON CONFLICT(user_id,query,kind) DO UPDATE SET searched_at=now()`, who(r).ID, q, kind); err != nil {
		return err
	}
	_, _ = s.DB.Exec(r.Context(), `DELETE FROM social.recent_searches WHERE user_id=$1 AND (query,kind) NOT IN (SELECT query,kind FROM social.recent_searches WHERE user_id=$1 ORDER BY searched_at DESC LIMIT 20)`, who(r).ID)
	write(w, 200, map[string]any{"items": out, "type": kind, "next_cursor": next})
	return nil
}

func (s *Server) recentSearches(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.DB.Query(r.Context(), `SELECT json_build_object('query',query,'type',kind,'searched_at',searched_at) FROM social.recent_searches WHERE user_id=$1 ORDER BY searched_at DESC,query LIMIT 20`, who(r).ID)
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

func (s *Server) clearRecentSearches(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.DB.Exec(r.Context(), `DELETE FROM social.recent_searches WHERE user_id=$1`, who(r).ID); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) error {
	town := r.URL.Query().Get("township")
	rows, err := s.DB.Query(r.Context(), `SELECT json_build_object('id',e.id,'host_id',e.host,'title',e.title,'description',e.description,'township',e.township,'venue_area',e.venue_area,'starts_at',e.starts_at,'ends_at',e.ends_at,'host_tier',a.tier,'attending',(SELECT count(*) FROM social.rsvps WHERE event_id=e.id AND status='ATTEND'),'my_rsvp',(SELECT status FROM social.rsvps WHERE event_id=e.id AND user_id=$1)) FROM social.events e JOIN identity.accounts a ON a.id=e.host WHERE e.status='LIVE' AND a.tier=3 AND NOT a.restricted AND NOT a.ghost AND ($2='' OR e.township=$2) AND NOT EXISTS(SELECT 1 FROM safety.blocks b WHERE (b.user_id=$1 AND b.target_id=e.host) OR (b.user_id=e.host AND b.target_id=$1)) ORDER BY e.starts_at,e.id LIMIT 50`, who(r).ID, town)
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
func (s *Server) createEvent(w http.ResponseWriter, r *http.Request) error {
	if err := s.mayWrite(r.Context(), who(r).ID); err != nil {
		return err
	}
	var tier int
	if err := s.DB.QueryRow(r.Context(), "SELECT tier FROM identity.accounts WHERE id=$1", who(r).ID).Scan(&tier); err != nil {
		return err
	}
	if tier < 3 {
		return fail(403, "tier3_host_required")
	}
	var b struct {
		Title       string    `json:"title"`
		Description string    `json:"description"`
		Town        string    `json:"township"`
		Area        string    `json:"venue_area"`
		Start       time.Time `json:"starts_at"`
		End         time.Time `json:"ends_at"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if !textOK(b.Title, 150) || !textOK(b.Description, 3000) || !textOK(b.Area, 150) || !validTown(b.Town) || b.Start.Before(time.Now()) || !b.End.After(b.Start) {
		return fail(400, "invalid_event")
	}
	var id string
	err := s.DB.QueryRow(r.Context(), `INSERT INTO social.events(host,title,description,township,venue_area,starts_at,ends_at) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, who(r).ID, b.Title, b.Description, b.Town, b.Area, b.Start, b.End).Scan(&id)
	if err != nil {
		return err
	}
	write(w, 202, map[string]any{"id": id, "status": "UNDER_REVIEW"})
	return nil
}
func (s *Server) rsvp(w http.ResponseWriter, r *http.Request) error {
	if err := s.mayWrite(r.Context(), who(r).ID); err != nil {
		return err
	}
	id, err := requiredID(r)
	if err != nil {
		return err
	}
	var b struct {
		Status string `json:"status"`
	}
	if err = decode(w, r, &b); err != nil {
		return err
	}
	if !contains([]string{"ATTEND", "NOT_GOING"}, b.Status) {
		return fail(400, "invalid_rsvp")
	}
	var ok bool
	err = s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM social.events e JOIN identity.accounts a ON a.id=e.host WHERE e.id=$2 AND e.status='LIVE' AND a.tier=3 AND NOT a.restricted AND NOT EXISTS(SELECT 1 FROM safety.blocks b WHERE (b.user_id=$1 AND b.target_id=e.host) OR (b.user_id=e.host AND b.target_id=$1)))`, who(r).ID, id).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return fail(404, "event_unavailable")
	}
	_, err = s.DB.Exec(r.Context(), `INSERT INTO social.rsvps(event_id,user_id,status) VALUES($1,$2,$3) ON CONFLICT(event_id,user_id) DO UPDATE SET status=excluded.status`, id, who(r).ID, b.Status)
	if err != nil {
		return err
	}
	write(w, 200, map[string]any{"status": b.Status})
	return nil
}
func (s *Server) groups(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.DB.Query(r.Context(), `SELECT json_build_object('id',g.id,'name',g.name,'township',g.township,'description',g.description,'joined',EXISTS(SELECT 1 FROM social.memberships WHERE group_id=g.id AND user_id=$1)) FROM social.groups g WHERE NOT EXISTS(SELECT 1 FROM safety.blocks b WHERE (b.user_id=$1 AND b.target_id=g.owner) OR (b.user_id=g.owner AND b.target_id=$1)) ORDER BY g.name,g.id LIMIT 50`, who(r).ID)
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
func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) error {
	if err := s.mayWrite(r.Context(), who(r).ID); err != nil {
		return err
	}
	var b struct {
		Name        string `json:"name"`
		Town        string `json:"township"`
		Description string `json:"description"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if !textOK(b.Name, 100) || !validTown(b.Town) || !textOK(b.Description, 1000) {
		return fail(400, "invalid_group")
	}
	var id string
	err := s.DB.QueryRow(r.Context(), `INSERT INTO social.groups(owner,name,township,description) VALUES($1,$2,$3,$4) RETURNING id`, who(r).ID, b.Name, b.Town, b.Description).Scan(&id)
	if err != nil {
		return err
	}
	write(w, 201, map[string]any{"id": id})
	return nil
}
func (s *Server) joinGroup(w http.ResponseWriter, r *http.Request) error {
	if err := s.mayWrite(r.Context(), who(r).ID); err != nil {
		return err
	}
	id, err := requiredID(r)
	if err != nil {
		return err
	}
	tag, err := s.DB.Exec(r.Context(), `INSERT INTO social.memberships(group_id,user_id) SELECT g.id,$1 FROM social.groups g WHERE g.id=$2 AND NOT EXISTS(SELECT 1 FROM safety.blocks b WHERE (b.user_id=$1 AND b.target_id=g.owner) OR (b.user_id=g.owner AND b.target_id=$1)) ON CONFLICT DO NOTHING`, who(r).ID, id)
	if err != nil {
		return err
	}
	write(w, 200, map[string]any{"joined": tag.RowsAffected() > 0})
	return nil
}
