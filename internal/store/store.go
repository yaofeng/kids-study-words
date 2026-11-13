// Package store persists users, notebooks, words, and recall history in SQLite.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// timeLayout is RFC3339 with fixed-width nanoseconds so that stored
// timestamps sort correctly as strings.
const timeLayout = "2006-01-02T15:04:05.000000000Z07:00"

// Store wraps the SQLite connection pool.
type Store struct {
	db *sql.DB
}

// Open creates the database file and its parent directory, applies the
// schema, and verifies connectivity.
func Open(ctx context.Context, path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "/" && path != ":memory:" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
	}

	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database connection.
func (s *Store) Close() error { return s.db.Close() }

// Ping verifies that the database is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// User is an account on this single-user instance.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

// Session is a logged-in browser session. Only a hash of the cookie token
// is stored.
type Session struct {
	TokenHash string
	UserID    int64
	CSRFToken string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Notebook is a user-created word collection.
type Notebook struct {
	ID        int64
	UserID    int64
	Name      string
	IsDefault bool
	CreatedAt time.Time
	UpdatedAt time.Time
	WordCount int
}

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time %q: %w", s, err)
	}
	return t.UTC(), nil
}

// scanTime parses a required timestamp column.
func scanTime(dest *time.Time, raw string) error {
	t, err := parseTime(raw)
	if err != nil {
		return err
	}
	*dest = t
	return nil
}

// scanNullTime parses a nullable timestamp column.
func scanNullTime(dest **time.Time, raw sql.NullString) error {
	if !raw.Valid {
		*dest = nil
		return nil
	}
	t, err := parseTime(raw.String)
	if err != nil {
		return err
	}
	*dest = &t
	return nil
}

// TimeNow is a variable so tests can freeze time if needed.
var TimeNow = func() time.Time { return time.Now().UTC() }

func mapNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
