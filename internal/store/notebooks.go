package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ListNotebooks returns a user's notebooks with word counts, default first.
func (s *Store) ListNotebooks(ctx context.Context, userID int64) ([]Notebook, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT n.id, n.user_id, n.name, n.is_default, n.created_at, n.updated_at,
		       (SELECT COUNT(*) FROM words w WHERE w.notebook_id = n.id)
		FROM notebooks n
		WHERE n.user_id = ?
		ORDER BY n.is_default DESC, n.updated_at DESC, n.id ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list notebooks: %w", err)
	}
	defer rows.Close()

	var out []Notebook
	for rows.Next() {
		nb, err := scanNotebookCount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, nb)
	}
	return out, rows.Err()
}

// NotebookByID loads one notebook owned by userID.
func (s *Store) NotebookByID(ctx context.Context, userID, id int64) (Notebook, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT n.id, n.user_id, n.name, n.is_default, n.created_at, n.updated_at,
		       (SELECT COUNT(*) FROM words w WHERE w.notebook_id = n.id)
		FROM notebooks n
		WHERE n.id = ? AND n.user_id = ?`, id, userID)
	return scanNotebookCount(row)
}

func scanNotebookCount(row interface{ Scan(...any) error }) (Notebook, error) {
	var nb Notebook
	var isDefault int
	var created, updated string
	if err := row.Scan(&nb.ID, &nb.UserID, &nb.Name, &isDefault, &created, &updated, &nb.WordCount); err != nil {
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

// nameTaken reports whether another notebook already uses name (case-insensitive).
func (s *Store) nameTaken(ctx context.Context, q rowQuerier, userID int64, name string, excludeID int64) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notebooks WHERE user_id = ? AND name = ? COLLATE NOCASE AND id <> ?`,
		userID, name, excludeID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// rowQuerier is implemented by both *sql.DB and *sql.Tx.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// CreateNotebook adds a notebook; the user's first notebook becomes default.
func (s *Store) CreateNotebook(ctx context.Context, userID int64, name string) (Notebook, error) {
	name = strings.TrimSpace(name)
	taken, err := s.nameTaken(ctx, s.db, userID, name, 0)
	if err != nil {
		return Notebook{}, fmt.Errorf("check notebook name: %w", err)
	}
	if taken {
		return Notebook{}, ErrDuplicate
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notebooks WHERE user_id = ?`, userID).Scan(&count); err != nil {
		return Notebook{}, fmt.Errorf("count notebooks: %w", err)
	}

	now := TimeNow()
	isDefault := 0
	if count == 0 {
		isDefault = 1
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO notebooks (user_id, name, is_default, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		userID, name, isDefault, formatTime(now), formatTime(now))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Notebook{}, ErrDuplicate
		}
		return Notebook{}, fmt.Errorf("insert notebook: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Notebook{}, fmt.Errorf("notebook id: %w", err)
	}
	return Notebook{ID: id, UserID: userID, Name: name, IsDefault: isDefault == 1, CreatedAt: now, UpdatedAt: now}, nil
}

// RenameNotebook renames a notebook and bumps its updated_at.
func (s *Store) RenameNotebook(ctx context.Context, userID, id int64, name string) (Notebook, error) {
	name = strings.TrimSpace(name)
	if _, err := s.NotebookByID(ctx, userID, id); err != nil {
		return Notebook{}, err
	}
	taken, err := s.nameTaken(ctx, s.db, userID, name, id)
	if err != nil {
		return Notebook{}, fmt.Errorf("check notebook name: %w", err)
	}
	if taken {
		return Notebook{}, ErrDuplicate
	}
	now := TimeNow()
	if _, err := s.db.ExecContext(ctx,
		`UPDATE notebooks SET name = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		name, formatTime(now), id, userID); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Notebook{}, ErrDuplicate
		}
		return Notebook{}, fmt.Errorf("rename notebook: %w", err)
	}
	return s.NotebookByID(ctx, userID, id)
}

// SetDefaultNotebook marks one notebook as the user's default, clearing any
// previous default in the same transaction.
func (s *Store) SetDefaultNotebook(ctx context.Context, userID, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notebooks WHERE id = ? AND user_id = ?`, id, userID).Scan(&exists); err != nil {
		return fmt.Errorf("find notebook: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE notebooks SET is_default = 0 WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("clear default: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE notebooks SET is_default = 1, updated_at = ? WHERE id = ? AND user_id = ?`,
		formatTime(TimeNow()), id, userID); err != nil {
		return fmt.Errorf("set default: %w", err)
	}
	return tx.Commit()
}

// DeleteNotebook removes a notebook and its words, promoting another
// notebook to default when the deleted one was default. The last notebook
// cannot be deleted.
func (s *Store) DeleteNotebook(ctx context.Context, userID, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM notebooks WHERE user_id = ?`, userID).Scan(&total); err != nil {
		return fmt.Errorf("count notebooks: %w", err)
	}
	var isDefault int
	err = tx.QueryRowContext(ctx,
		`SELECT is_default FROM notebooks WHERE id = ? AND user_id = ?`, id, userID).Scan(&isDefault)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("find notebook: %w", err)
	}
	if total <= 1 {
		return ErrLastNotebook
	}

	if isDefault == 1 {
		if _, err := tx.ExecContext(ctx, `UPDATE notebooks SET is_default = 0 WHERE user_id = ?`, userID); err != nil {
			return fmt.Errorf("clear default: %w", err)
		}
		var nextID int64
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM notebooks WHERE user_id = ? AND id <> ? ORDER BY updated_at DESC, id ASC LIMIT 1`,
			userID, id).Scan(&nextID); err != nil {
			return fmt.Errorf("pick next default: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE notebooks SET is_default = 1 WHERE id = ?`, nextID); err != nil {
			return fmt.Errorf("promote default: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM notebooks WHERE id = ? AND user_id = ?`, id, userID); err != nil {
		return fmt.Errorf("delete notebook: %w", err)
	}
	return tx.Commit()
}

// TouchNotebook bumps updated_at, called when its words change.
func (s *Store) TouchNotebook(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE notebooks SET updated_at = ? WHERE id = ?`, formatTime(TimeNow()), id); err != nil {
		return fmt.Errorf("touch notebook: %w", err)
	}
	return nil
}
