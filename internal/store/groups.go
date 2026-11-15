package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
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

// GroupWordIDs returns the word IDs in a group the user owns, sorted by ID.
// A group with no members yields an empty, non-nil slice.
func (s *Store) GroupWordIDs(ctx context.Context, userID, groupID int64) ([]int64, error) {
	if _, err := s.WordGroupByID(ctx, userID, groupID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT word_id FROM word_group_members WHERE group_id = ? ORDER BY word_id`, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group words: %w", err)
	}
	defer rows.Close()

	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan group word: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SetGroupWords replaces a group's members with wordIDs, which must all belong
// to the group's notebook. Duplicate IDs are ignored; nil clears the group.
// WordGroupByID runs before the transaction because the pool holds a single
// connection; no s.db call may happen inside the transaction.
func (s *Store) SetGroupWords(ctx context.Context, userID, groupID int64, wordIDs []int64) error {
	g, err := s.WordGroupByID(ctx, userID, groupID)
	if err != nil {
		return err
	}

	ids := append([]int64(nil), wordIDs...)
	slices.Sort(ids)
	ids = slices.Compact(ids)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	if len(ids) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		args := make([]any, 0, len(ids)+1)
		args = append(args, g.NotebookID)
		for _, id := range ids {
			args = append(args, id)
		}
		var count int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM words WHERE notebook_id = ? AND id IN (`+placeholders+`)`,
			args...).Scan(&count); err != nil {
			return fmt.Errorf("check group words: %w", err)
		}
		if count != len(ids) {
			return ErrNotFound
		}
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM word_group_members WHERE group_id = ?`, groupID); err != nil {
		return fmt.Errorf("clear group members: %w", err)
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO word_group_members (group_id, word_id) VALUES (?, ?)`,
			groupID, id); err != nil {
			return fmt.Errorf("add group member: %w", err)
		}
	}
	return tx.Commit()
}

// GroupMemberships maps each word in a notebook to the groups containing it.
// Words in no group are absent from the map.
func (s *Store) GroupMemberships(ctx context.Context, notebookID int64) (map[int64][]WordGroup, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.word_id, g.id, g.notebook_id, g.name, g.created_at,
		       (SELECT COUNT(*) FROM word_group_members m2 WHERE m2.group_id = g.id)
		FROM word_group_members m
		JOIN word_groups g ON g.id = m.group_id
		WHERE g.notebook_id = ?
		ORDER BY g.name COLLATE NOCASE, g.id`, notebookID)
	if err != nil {
		return nil, fmt.Errorf("group memberships: %w", err)
	}
	defer rows.Close()

	out := make(map[int64][]WordGroup)
	for rows.Next() {
		var wordID int64
		var g WordGroup
		var created string
		if err := rows.Scan(&wordID, &g.ID, &g.NotebookID, &g.Name, &created, &g.WordCount); err != nil {
			return nil, fmt.Errorf("scan group membership: %w", err)
		}
		if err := scanTime(&g.CreatedAt, created); err != nil {
			return nil, err
		}
		out[wordID] = append(out[wordID], g)
	}
	return out, rows.Err()
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
