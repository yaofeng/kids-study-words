package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// CountUsers returns how many accounts exist.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// CreateFirstUser atomically creates the only account on this instance and
// its default notebook. It returns ErrSetupComplete when a user already
// exists.
func (s *Store) CreateFirstUser(ctx context.Context, username, hash, defaultNotebook string) (User, Notebook, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, Notebook{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return User{}, Notebook{}, fmt.Errorf("count users: %w", err)
	}
	if n > 0 {
		return User{}, Notebook{}, ErrSetupComplete
	}
	return s.insertUser(ctx, tx, username, hash, defaultNotebook)
}

// CreateUser creates another account and its default notebook. It returns
// ErrDuplicate when the username is already taken.
func (s *Store) CreateUser(ctx context.Context, username, hash, defaultNotebook string) (User, Notebook, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, Notebook{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	return s.insertUser(ctx, tx, username, hash, defaultNotebook)
}

func (s *Store) insertUser(ctx context.Context, tx *sql.Tx, username, hash, defaultNotebook string) (User, Notebook, error) {
	now := TimeNow()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, created_at) VALUES (?, ?, ?)`,
		username, hash, formatTime(now))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return User{}, Notebook{}, ErrDuplicate
		}
		return User{}, Notebook{}, fmt.Errorf("insert user: %w", err)
	}
	userID, err := res.LastInsertId()
	if err != nil {
		return User{}, Notebook{}, fmt.Errorf("user id: %w", err)
	}

	nbRes, err := tx.ExecContext(ctx,
		`INSERT INTO notebooks (user_id, name, is_default, created_at, updated_at) VALUES (?, ?, 1, ?, ?)`,
		userID, defaultNotebook, formatTime(now), formatTime(now))
	if err != nil {
		return User{}, Notebook{}, fmt.Errorf("insert notebook: %w", err)
	}
	nbID, err := nbRes.LastInsertId()
	if err != nil {
		return User{}, Notebook{}, fmt.Errorf("notebook id: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return User{}, Notebook{}, fmt.Errorf("commit: %w", err)
	}

	return User{ID: userID, Username: username, PasswordHash: hash, CreatedAt: now},
		Notebook{ID: nbID, UserID: userID, Name: defaultNotebook, IsDefault: true, CreatedAt: now, UpdatedAt: now},
		nil
}

const userColumns = `id, username, password_hash, created_at`

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	var created string
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &created); err != nil {
		return User{}, mapNoRows(err)
	}
	if err := scanTime(&u.CreatedAt, created); err != nil {
		return User{}, err
	}
	return u, nil
}

// UserByUsername looks up an account by its exact username.
func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE username = ?`, username)
	return scanUser(row)
}

// UserByID looks up an account by id.
func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id)
	return scanUser(row)
}

// UpdatePassword replaces the password hash for a user.
func (s *Store) UpdatePassword(ctx context.Context, userID int64, hash string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DefaultNotebook returns the user's default notebook.
func (s *Store) DefaultNotebook(ctx context.Context, userID int64) (Notebook, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, is_default, created_at, updated_at FROM notebooks WHERE user_id = ? AND is_default = 1`,
		userID)
	return scanNotebook(row)
}

func scanNotebook(row interface{ Scan(...any) error }) (Notebook, error) {
	var nb Notebook
	var isDefault int
	var created, updated string
	if err := row.Scan(&nb.ID, &nb.UserID, &nb.Name, &isDefault, &created, &updated); err != nil {
		return Notebook{}, mapNoRows(err)
	}
	if err := scanTime(&nb.CreatedAt, created); err != nil {
		return Notebook{}, err
	}
	if err := scanTime(&nb.UpdatedAt, updated); err != nil {
		return Notebook{}, err
	}
	nb.IsDefault = isDefault == 1
	return nb, nil
}
