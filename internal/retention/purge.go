// Package retention implements the scheduled cross-store erase for accounts that
// passed the DSR grace period. It needs an owner-level connection: one account
// delete fans out through FK cascades the operational roles must not hold.
package retention

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PurgeDue erases one batch of due deletion requests and returns how many
// accounts were removed. Candidates are locked with SKIP LOCKED so concurrent
// runs never erase the same account twice.
func PurgeDue(ctx context.Context, db *pgxpool.Pool) (int, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT user_id FROM identity.deletion_requests WHERE purge_after<=now() ORDER BY purge_after LIMIT 100 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return 0, err
	}
	var due []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		due = append(due, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	purged := 0
	for _, id := range due {
		// OTP and outbox rows have no account FK; everything else (sessions,
		// social, dating, safety, chat membership and sent messages) cascades.
		if _, err = tx.Exec(ctx, `DELETE FROM identity.otp WHERE phone_hash=(SELECT phone_hash FROM identity.accounts WHERE id=$1)`, id); err != nil {
			return 0, err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM private.outbox WHERE user_id=$1`, id); err != nil {
			return 0, err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM identity.accounts WHERE id=$1`, id)
		if err != nil {
			return 0, err
		}
		if tag.RowsAffected() > 0 {
			purged++
		}
	}
	// Rooms left with fewer than two members are unreachable (roomAccess requires
	// two) and hold no remaining member's content.
	if _, err = tx.Exec(ctx, `DELETE FROM chat.rooms WHERE id IN(SELECT r.id FROM chat.rooms r LEFT JOIN chat.members m ON m.room_id=r.id GROUP BY r.id HAVING count(m.user_id)<2)`); err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return purged, nil
}
