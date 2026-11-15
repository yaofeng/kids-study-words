package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// MCPToken is the credential a user has issued for the MCP server. Only the
// SHA-256 hash of the token is stored.
type MCPToken struct {
	UserID     int64
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// MCPTokenByUser returns the user's MCP token metadata.
func (s *Store) MCPTokenByUser(ctx context.Context, userID int64) (MCPToken, error) {
	var tok MCPToken
	var created string
	var lastUsed sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, created_at, last_used_at FROM mcp_tokens WHERE user_id = ?`,
		userID).Scan(&tok.UserID, &created, &lastUsed)
	if err != nil {
		return MCPToken{}, mapNoRows(err)
	}
	if err := scanTime(&tok.CreatedAt, created); err != nil {
		return MCPToken{}, err
	}
	if err := scanNullTime(&tok.LastUsedAt, lastUsed); err != nil {
		return MCPToken{}, err
	}
	return tok, nil
}

// ReplaceMCPToken issues a new token hash for the user, replacing any previous
// token. The creation time is reset and last-used is cleared.
func (s *Store) ReplaceMCPToken(ctx context.Context, userID int64, hash string) (MCPToken, error) {
	now := TimeNow()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mcp_tokens (user_id, token_hash, created_at, last_used_at)
		 VALUES (?, ?, ?, NULL)
		 ON CONFLICT(user_id) DO UPDATE SET
		   token_hash = excluded.token_hash,
		   created_at = excluded.created_at,
		   last_used_at = NULL`,
		userID, hash, formatTime(now))
	if err != nil {
		return MCPToken{}, fmt.Errorf("replace mcp token: %w", err)
	}
	return MCPToken{UserID: userID, CreatedAt: now}, nil
}

// RevokeMCPToken deletes the user's MCP token. It is not an error when no
// token exists.
func (s *Store) RevokeMCPToken(ctx context.Context, userID int64) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM mcp_tokens WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("revoke mcp token: %w", err)
	}
	return nil
}

// UserByMCPToken resolves the account that owns the given token hash.
func (s *Store) UserByMCPToken(ctx context.Context, hash string) (User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT u.id, u.username, u.password_hash, u.created_at
		 FROM mcp_tokens t JOIN users u ON u.id = t.user_id
		 WHERE t.token_hash = ?`, hash)
	return scanUser(row)
}

// TouchMCPToken records token use, but at most once every five minutes to
// avoid a write on every request.
func (s *Store) TouchMCPToken(ctx context.Context, userID int64, at time.Time) error {
	cutoff := formatTime(at.UTC().Add(-5 * time.Minute))
	if _, err := s.db.ExecContext(ctx,
		`UPDATE mcp_tokens SET last_used_at = ?
		 WHERE user_id = ? AND (last_used_at IS NULL OR last_used_at < ?)`,
		formatTime(at), userID, cutoff); err != nil {
		return fmt.Errorf("touch mcp token: %w", err)
	}
	return nil
}
