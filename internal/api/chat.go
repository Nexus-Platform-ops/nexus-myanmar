package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
)

func (s *Server) roomAccess(r *http.Request, id string) error {
	var kind string
	var count int
	err := s.DB.QueryRow(r.Context(), `SELECT r.kind,(SELECT count(*) FROM chat.members WHERE room_id=r.id) FROM chat.rooms r JOIN chat.members m ON m.room_id=r.id WHERE r.id=$1 AND m.user_id=$2`, id, who(r).ID).Scan(&kind, &count)
	if err == pgx.ErrNoRows {
		return fail(404, "room_unavailable")
	}
	if err != nil {
		return err
	}
	if count < 2 {
		return fail(404, "room_unavailable")
	}
	rows, err := s.DB.Query(r.Context(), `SELECT m.user_id,a.restricted,a.ghost FROM chat.members m JOIN identity.accounts a ON a.id=m.user_id WHERE m.room_id=$1`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	type peer struct {
		id                string
		restricted, ghost bool
	}
	peers := []peer{}
	for rows.Next() {
		var p peer
		if err = rows.Scan(&p.id, &p.restricted, &p.ghost); err != nil {
			return err
		}
		peers = append(peers, p)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	rows.Close()
	for _, p := range peers {
		if p.restricted || p.ghost {
			return fail(403, "room_locked")
		}
		if p.id == who(r).ID {
			continue
		}
		b, err := s.blocked(r.Context(), who(r).ID, p.id)
		if err != nil {
			return err
		}
		if b {
			return fail(404, "room_unavailable")
		}
		if kind == "MATCH" {
			ok, err := s.canSeeDating(r.Context(), who(r).ID, p.id)
			if err != nil {
				return err
			}
			if !ok {
				return fail(404, "room_unavailable")
			}
		}
	}
	return nil
}
func (s *Server) rooms(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.DB.Query(r.Context(), `SELECT r.id,r.kind FROM chat.rooms r JOIN chat.members m ON m.room_id=r.id WHERE m.user_id=$1 ORDER BY r.created_at DESC LIMIT 100`, who(r).ID)
	if err != nil {
		return err
	}
	type room struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	}
	all := []room{}
	for rows.Next() {
		var v room
		if err = rows.Scan(&v.ID, &v.Kind); err != nil {
			rows.Close()
			return err
		}
		all = append(all, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	out := []room{}
	for _, v := range all {
		if err = s.roomAccess(r, v.ID); err == nil {
			out = append(out, v)
		} else if _, ok := err.(apiError); !ok {
			return err
		}
	}
	write(w, 200, map[string]any{"items": out})
	return nil
}
func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) error {
	if err := s.mayWrite(r.Context(), who(r).ID); err != nil {
		return err
	}
	var b struct {
		Members []string `json:"members"`
	}
	if err := decode(w, r, &b); err != nil {
		return err
	}
	if len(b.Members) < 1 || len(b.Members) > 9 {
		return fail(400, "members_1_to_9")
	}
	users := []string{who(r).ID}
	for _, u := range b.Members {
		if !validID(u) || contains(users, u) {
			return fail(400, "invalid_members")
		}
		blocked, err := s.blocked(r.Context(), who(r).ID, u)
		if err != nil {
			return err
		}
		if blocked {
			return fail(403, "member_unavailable")
		}
		var friend bool
		err = s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM social.friendships f JOIN identity.accounts a ON a.id=$2 WHERE ((f.user_id=$1 AND f.friend_id=$2) OR (f.user_id=$2 AND f.friend_id=$1)) AND a.phone_verified AND NOT a.restricted AND NOT a.ghost)`, who(r).ID, u).Scan(&friend)
		if err != nil {
			return err
		}
		if !friend {
			return fail(403, "social_dm_requires_friendship")
		}
		users = append(users, u)
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO chat.rooms(kind) VALUES('SOCIAL') RETURNING id`).Scan(&id)
	if err != nil {
		return err
	}
	for _, u := range users {
		if _, err = tx.Exec(r.Context(), `INSERT INTO chat.members(room_id,user_id) VALUES($1,$2)`, id, u); err != nil {
			return err
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	write(w, 201, map[string]any{"id": id})
	return nil
}
func (s *Server) messages(w http.ResponseWriter, r *http.Request) error {
	id, err := requiredID(r)
	if err != nil {
		return err
	}
	if err = s.roomAccess(r, id); err != nil {
		return err
	}
	before := int64(9223372036854775807)
	if v := r.URL.Query().Get("before_seq"); v != "" {
		before, err = strconv.ParseInt(v, 10, 64)
		if err != nil || before < 1 {
			return fail(400, "invalid_sequence")
		}
	}
	rows, err := s.DB.Query(r.Context(), `SELECT json_build_object('id',id,'sender',sender,'seq',seq,'text',pgp_sym_decrypt(body_cipher,$4),'mode',mode,'created_at',created_at) FROM chat.messages WHERE room_id=$1 AND seq<$2 AND NOT (mode='WHISPER' AND EXISTS(SELECT 1 FROM chat.whisper_reads w WHERE w.message_id=chat.messages.id AND w.reader=$3)) ORDER BY seq DESC LIMIT 50`, id, before, who(r).ID, s.Cfg.DataKey)
	if err != nil {
		return err
	}
	out, err := jsonRows(rows)
	if err != nil {
		return err
	}
	sort.Slice(out, func(i, j int) bool {
		var a, b struct {
			Seq int64 `json:"seq"`
		}
		_ = json.Unmarshal(out[i], &a)
		_ = json.Unmarshal(out[j], &b)
		return a.Seq < b.Seq
	})
	var next any
	if len(out) == 50 {
		var a struct {
			Seq int64 `json:"seq"`
		}
		if err = json.Unmarshal(out[0], &a); err != nil {
			return err
		}
		next = a.Seq
	}
	write(w, 200, map[string]any{"items": out, "next_before_seq": next})
	return nil
}
func (s *Server) send(w http.ResponseWriter, r *http.Request) error {
	if err := s.mayWrite(r.Context(), who(r).ID); err != nil {
		return err
	}
	id, err := requiredID(r)
	if err != nil {
		return err
	}
	if err = s.roomAccess(r, id); err != nil {
		return err
	}
	var b struct {
		Text     string `json:"text"`
		Mode     string `json:"mode"`
		ClientID string `json:"client_id"`
	}
	if err = decode(w, r, &b); err != nil {
		return err
	}
	if !textOK(b.Text, 4000) || !contains([]string{"NORMAL", "WHISPER"}, b.Mode) || !validID(b.ClientID) {
		return fail(400, "invalid_message")
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	key := id + ":" + who(r).ID + ":" + b.ClientID
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); err != nil {
		return err
	}
	var mid string
	var seq int64
	err = tx.QueryRow(r.Context(), `SELECT message_id,seq FROM chat.dedup WHERE room_id=$1 AND sender=$2 AND client_id=$3`, id, who(r).ID, b.ClientID).Scan(&mid, &seq)
	if err == nil {
		var expired bool
		if err = tx.QueryRow(r.Context(), `SELECT NOT EXISTS(SELECT 1 FROM chat.messages WHERE id=$1)`, mid).Scan(&expired); err != nil {
			return err
		}
		write(w, 200, map[string]any{"id": mid, "seq": seq, "duplicate": true, "expired": expired})
		return nil
	}
	if err != pgx.ErrNoRows {
		return err
	}
	mid = uuid.NewString()
	err = tx.QueryRow(r.Context(), `INSERT INTO chat.messages(id,room_id,sender,body_cipher,mode,client_id) VALUES($1,$2,$3,pgp_sym_encrypt($4,$5),$6,$7) RETURNING seq`, mid, id, who(r).ID, b.Text, s.Cfg.DataKey, b.Mode, b.ClientID).Scan(&seq)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO chat.dedup(room_id,sender,client_id,message_id,seq) VALUES($1,$2,$3,$4,$5)`, id, who(r).ID, b.ClientID, mid, seq); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO private.outbox(topic,user_id,payload) SELECT 'chat.changed',user_id,jsonb_build_object('room_id',$1::uuid::text) FROM chat.members WHERE room_id=$1 AND user_id<>$2`, id, who(r).ID); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	write(w, 201, map[string]any{"id": mid, "seq": seq, "duplicate": false})
	return nil
}
func (s *Server) readMessages(w http.ResponseWriter, r *http.Request) error {
	id, err := requiredID(r)
	if err != nil {
		return err
	}
	if err = s.roomAccess(r, id); err != nil {
		return err
	}
	var b struct {
		Through int64 `json:"through_seq"`
	}
	if err = decode(w, r, &b); err != nil {
		return err
	}
	if b.Through < 1 {
		return fail(400, "invalid_sequence")
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	// Per-recipient semantics: mark consumed for this reader only. A whisper is
	// physically deleted only when every non-sender room member has read it, so
	// one reader can never wipe a group room for everyone else.
	marked, err := tx.Exec(r.Context(), `INSERT INTO chat.whisper_reads(message_id,reader) SELECT id,$2 FROM chat.messages WHERE room_id=$1 AND sender<>$2 AND seq<=$3 AND mode='WHISPER' ON CONFLICT DO NOTHING`, id, who(r).ID, b.Through)
	if err != nil {
		return err
	}
	rows, err := tx.Query(r.Context(), `DELETE FROM chat.messages m WHERE m.room_id=$1 AND m.sender<>$2 AND m.seq<=$3 AND m.mode='WHISPER' AND NOT EXISTS(SELECT 1 FROM chat.members x WHERE x.room_id=m.room_id AND x.user_id<>m.sender AND NOT EXISTS(SELECT 1 FROM chat.whisper_reads w WHERE w.message_id=m.id AND w.reader=x.user_id)) RETURNING m.id,m.seq`, id, who(r).ID, b.Through)
	if err != nil {
		return err
	}
	type removed struct {
		id  string
		seq int64
	}
	deleted := []removed{}
	for rows.Next() {
		var v removed
		if err = rows.Scan(&v.id, &v.seq); err != nil {
			rows.Close()
			return err
		}
		deleted = append(deleted, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range deleted {
		if _, err = tx.Exec(r.Context(), `INSERT INTO chat.tombstones(message_id,room_id,seq) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, v.id, id, v.seq); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO private.outbox(topic,user_id,payload) SELECT 'chat.expired',user_id,jsonb_build_object('room_id',$1::uuid::text) FROM chat.members WHERE room_id=$1`, id); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	write(w, 200, map[string]any{"wiped": len(deleted), "marked_read": marked.RowsAffected(), "through_seq": b.Through})
	return nil
}

// Single-use ticket prevents putting a reusable JWT in a logged websocket query string.
func (s *Server) ticket(w http.ResponseWriter, r *http.Request) error {
	t := randomToken()
	raw, _ := json.Marshal(who(r))
	if err := s.Redis.Set(r.Context(), "ws-ticket:"+digest(t), raw, 30*time.Second).Err(); err != nil {
		return err
	}
	write(w, 201, map[string]any{"ticket": t, "expires_in": 30})
	return nil
}
func (s *Server) websocket(w http.ResponseWriter, r *http.Request) error {
	t := r.URL.Query().Get("ticket")
	if len(t) != 64 {
		return fail(401, "ticket_required")
	}
	raw, err := s.Redis.GetDel(r.Context(), "ws-ticket:"+digest(t)).Bytes()
	if err != nil {
		return fail(401, "ticket_expired")
	}
	var p principal
	if json.Unmarshal(raw, &p) != nil || !validID(p.ID) {
		return fail(401, "ticket_expired")
	}
	// Read-only invalidation stream, not a content broadcast. REST rechecks authorization.
	up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { o := r.Header.Get("Origin"); return o == "" || o == s.Cfg.Origin }, ReadBufferSize: 1024, WriteBufferSize: 1024}
	c, err := up.Upgrade(w, r, nil)
	if err != nil {
		return nil
	}
	defer c.Close()
	c.SetReadLimit(1024)
	_ = c.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.SetPongHandler(func(string) error { return c.SetReadDeadline(time.Now().Add(60 * time.Second)) })
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}()
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	last := time.Now().UTC()
	for {
		select {
		case <-done:
			return nil
		case <-r.Context().Done():
			return nil
		case <-heartbeat.C:
			if c.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
				return nil
			}
		case <-tick.C:
			var active bool
			err = s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM identity.sessions WHERE id=$1 AND user_id=$2 AND expires_at>now())`, p.Session, p.ID).Scan(&active)
			if err != nil || !active {
				return nil
			}
			var changed bool
			err = s.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM private.outbox WHERE user_id=$1 AND created_at>$2)`, p.ID, last).Scan(&changed)
			if err != nil {
				return nil
			}
			if changed {
				last = time.Now().UTC()
				_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if c.WriteJSON(map[string]any{"type": "invalidate", "channels": []string{"chats", "notifications"}}) != nil {
					return nil
				}
			}
		}
	}
}
