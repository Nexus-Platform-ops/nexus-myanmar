package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// protectedIdempotent authenticates first, then replays a stored response when
// the caller retries a mutating request with the same Idempotency-Key. Only
// deterministic 2xx/3xx outcomes are recorded; transient failures are deleted
// so a retry can execute again.
func (s *Server) protectedIdempotent(pattern string, h handler) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		p, err := s.authenticate(r)
		if err != nil {
			s.wrap(func(http.ResponseWriter, *http.Request) error { return err })(w, r)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), contextKey{}, p))
		if err = s.idempotent(w, r, h); err != nil {
			s.wrap(func(http.ResponseWriter, *http.Request) error { return err })(w, r)
		}
	})
}

func (s *Server) idempotent(w http.ResponseWriter, r *http.Request, h handler) error {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		s.wrap(h)(w, r)
		return nil
	}
	if !validID(key) {
		return fail(400, "invalid_idempotency_key")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		return fail(400, "invalid_json")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	path := r.Method + " " + r.URL.Path
	var status int
	var response []byte
	var storedHash, storedPath string
	err = s.DB.QueryRow(r.Context(), `SELECT status,response,body_hash,method_path FROM identity.idempotency WHERE user_id=$1 AND key=$2`, who(r).ID, key).Scan(&status, &response, &storedHash, &storedPath)
	if err == nil {
		if storedPath != path || storedHash != hash {
			return fail(409, "idempotency_key_reuse")
		}
		if status == 0 {
			return fail(409, "idempotency_in_progress")
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Idempotency-Replayed", "true")
		w.WriteHeader(status)
		_, _ = w.Write(response)
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, err = s.DB.Exec(r.Context(), `INSERT INTO identity.idempotency(user_id,key,method_path,body_hash,status,response) VALUES($1,$2,$3,$4,0,'{}')`, who(r).ID, key, path, hash)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fail(409, "idempotency_in_progress")
		}
		return err
	}
	cw := &captureWriter{header: http.Header{}, status: 0}
	s.wrap(h)(cw, r)
	if cw.status == 0 {
		cw.status = 200
	}
	if cw.status >= 200 && cw.status < 400 {
		if _, err = s.DB.Exec(r.Context(), `UPDATE identity.idempotency SET status=$3,response=$4 WHERE user_id=$1 AND key=$2`, who(r).ID, key, cw.status, json.RawMessage(cw.body.Bytes())); err != nil {
			return err
		}
	} else {
		_, _ = s.DB.Exec(r.Context(), `DELETE FROM identity.idempotency WHERE user_id=$1 AND key=$2`, who(r).ID, key)
	}
	for k, v := range cw.header {
		w.Header()[k] = v
	}
	w.WriteHeader(cw.status)
	_, _ = w.Write(cw.body.Bytes())
	return nil
}

type captureWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (c *captureWriter) Header() http.Header { return c.header }
func (c *captureWriter) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}
func (c *captureWriter) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return c.body.Write(b)
}
