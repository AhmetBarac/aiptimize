package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Session is a server-side login session. ID is the SHA-256 hash of the token
// held by the browser, never the token itself.
type Session struct {
	ID        string
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
	LastSeen  time.Time
	IP        string
}

// CreateSession stores a new session.
func (s *Store) CreateSession(ctx context.Context, ses Session) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions(id, user_id, created_at, expires_at, last_seen, ip) VALUES (?, ?, ?, ?, ?, ?)`,
		ses.ID, ses.UserID, formatTime(ses.CreatedAt), formatTime(ses.ExpiresAt), formatTime(ses.LastSeen), ses.IP)
	return err
}

// SessionByID loads a session by its hashed id.
func (s *Store) SessionByID(ctx context.Context, id string) (Session, error) {
	var ses Session
	var created, expires, lastSeen string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, created_at, expires_at, last_seen, ip FROM sessions WHERE id = ?`, id).
		Scan(&ses.ID, &ses.UserID, &created, &expires, &lastSeen, &ses.IP)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	if ses.CreatedAt, err = parseTime(created); err != nil {
		return Session{}, err
	}
	if ses.ExpiresAt, err = parseTime(expires); err != nil {
		return Session{}, err
	}
	if ses.LastSeen, err = parseTime(lastSeen); err != nil {
		return Session{}, err
	}
	return ses, nil
}

// TouchSession extends the idle expiry of a session.
func (s *Store) TouchSession(ctx context.Context, id string, seen, expires time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET last_seen = ?, expires_at = ? WHERE id = ?`,
		formatTime(seen), formatTime(expires), id)
	return err
}

// DeleteSession revokes one session.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// DeleteSessionsForUser revokes all sessions of a user.
func (s *Store) DeleteSessionsForUser(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}
