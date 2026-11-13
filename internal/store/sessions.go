package store

import (
	"context"
	"fmt"
	"time"
)

// CreateSession stores a session keyed by the hashed cookie token.
func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, csrf string, createdAt, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, csrf_token, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		tokenHash, userID, csrf, formatTime(createdAt), formatTime(expiresAt))
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// SessionByToken returns a session regardless of expiry. Callers that need
// freshness should use UserBySession.
func (s *Store) SessionByToken(ctx context.Context, tokenHash string) (Session, error) {
	var sess Session
	var created, expires string
	err := s.db.QueryRowContext(ctx,
		`SELECT token_hash, user_id, csrf_token, created_at, expires_at FROM sessions WHERE token_hash = ?`,
		tokenHash).
		Scan(&sess.TokenHash, &sess.UserID, &sess.CSRFToken, &created, &expires)
	if err != nil {
		return Session{}, mapNoRows(err)
	}
	if err := scanTime(&sess.CreatedAt, created); err != nil {
		return Session{}, err
	}
	if err := scanTime(&sess.ExpiresAt, expires); err != nil {
		return Session{}, err
	}
	return sess, nil
}

// UserBySession returns the user behind a token if the session has not expired.
func (s *Store) UserBySession(ctx context.Context, tokenHash string, now time.Time) (User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT u.id, u.username, u.password_hash, u.created_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash = ? AND s.expires_at > ?`,
		tokenHash, formatTime(now))
	return scanUser(row)
}

// DeleteSession removes one session.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteSessionsForUser removes every session belonging to a user.
func (s *Store) DeleteSessionsForUser(ctx context.Context, userID int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
	return nil
}

// DeleteSessionsForUserExcept removes all of a user's sessions except the
// one identified by keepTokenHash.
func (s *Store) DeleteSessionsForUserExcept(ctx context.Context, userID int64, keepTokenHash string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?`, userID, keepTokenHash); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
	return nil
}

// DeleteExpiredSessions removes sessions whose expiry is at or before now.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at <= ?`, formatTime(now)); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	return nil
}
