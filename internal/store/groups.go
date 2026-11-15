package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// WordGroup is a named collection of words inside a notebook.
type WordGroup struct {
	ID         int64
	NotebookID int64
	Name       string
	CreatedAt  time.Time
	WordCount  int
}

const wordGroupColumns = `g.id, g.notebook_id, g.name, g.created_at,
	(SELECT COUNT(*) FROM word_group_members m WHERE m.group_id = g.id)`

func scanWordGroup(row interface{ Scan(...any) error }) (WordGroup, error) {
	var g WordGroup
	var created string
	if err := row.Scan(&g.ID, &g.NotebookID, &g.Name, &created, &g.WordCount); err != nil {
		return WordGroup{}, mapNoRows(err)
	}
	if err := scanTime(&g.CreatedAt, created); err != nil {
		return WordGroup{}, err
	}
	return g, nil
}

// ListWordGroups returns a notebook's groups ordered by name, then id.
func (s *Store) ListWordGroups(ctx context.Context, notebookID int64) ([]WordGroup, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+wordGroupColumns+`
		FROM word_groups g
		WHERE g.notebook_id = ?
		ORDER BY g.name COLLATE NOCASE, g.id`, notebookID)
	if err != nil {
		return nil, fmt.Errorf("list word groups: %w", err)
	}
	defer rows.Close()

	var out []WordGroup
	for rows.Next() {
		g, err := scanWordGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// WordGroupByID loads one group scoped to its owning user.
func (s *Store) WordGroupByID(ctx context.Context, userID, groupID int64) (WordGroup, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+wordGroupColumns+`
		FROM word_groups g
		JOIN notebooks n ON n.id = g.notebook_id
		WHERE g.id = ? AND n.user_id = ?`, groupID, userID)
	return scanWordGroup(row)
}

// CreateWordGroup adds a group to a notebook.
func (s *Store) CreateWordGroup(ctx context.Context, notebookID int64, name string) (WordGroup, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return WordGroup{}, errors.New("group name is required")
	}
	now := TimeNow()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO word_groups (notebook_id, name, created_at) VALUES (?, ?, ?)`,
		notebookID, name, formatTime(now))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return WordGroup{}, ErrDuplicate
		}
		return WordGroup{}, fmt.Errorf("insert word group: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return WordGroup{}, fmt.Errorf("word group id: %w", err)
	}
	return WordGroup{ID: id, NotebookID: notebookID, Name: name, CreatedAt: now, WordCount: 0}, nil
}

// RenameWordGroup renames a group the user owns.
func (s *Store) RenameWordGroup(ctx context.Context, userID, groupID int64, name string) (WordGroup, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return WordGroup{}, errors.New("group name is required")
	}
	if _, err := s.WordGroupByID(ctx, userID, groupID); err != nil {
		return WordGroup{}, err
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE word_groups SET name = ? WHERE id = ?`, name, groupID); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return WordGroup{}, ErrDuplicate
		}
		return WordGroup{}, fmt.Errorf("rename word group: %w", err)
	}
	return s.WordGroupByID(ctx, userID, groupID)
}

// DeleteWordGroup removes a group the user owns.
func (s *Store) DeleteWordGroup(ctx context.Context, userID, groupID int64) error {
	if _, err := s.WordGroupByID(ctx, userID, groupID); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM word_groups WHERE id = ?`, groupID)
	if err != nil {
		return fmt.Errorf("delete word group: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete word group: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
