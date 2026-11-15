package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// RecallScopeKind selects which words a recall session draws from.
type RecallScopeKind int

const (
	// ScopeAll draws from every word in the notebook.
	ScopeAll RecallScopeKind = iota
	// ScopeProficiency draws from words below a proficiency threshold.
	ScopeProficiency
	// ScopeGroups draws from the words in the given groups.
	ScopeGroups
)

// RecallScope describes the word set for a recall session.
type RecallScope struct {
	Kind           RecallScopeKind
	MaxProficiency int
	GroupIDs       []int64
}

// wordColumnsAliased is wordColumns qualified with the words alias used by
// queries that join word_group_members.
const wordColumnsAliased = `w.id, w.notebook_id, w.word, w.phonetic, w.translation, w.definition,
	w.examples_json, w.proficiency, w.added_at, w.last_reviewed_at, w.review_count`

// ListRecallWords returns the notebook's words selected by scope, ordered by
// most recently added first. It returns an empty slice, never nil, when the
// scope matches no words. An unknown kind or an invalid scope returns
// ErrInvalidScope.
func (s *Store) ListRecallWords(ctx context.Context, notebookID int64, scope RecallScope) ([]Word, error) {
	switch scope.Kind {
	case ScopeAll:
		words, err := s.ListWords(ctx, notebookID, "added")
		if err != nil {
			return nil, err
		}
		if words == nil {
			words = []Word{}
		}
		return words, nil

	case ScopeProficiency:
		if scope.MaxProficiency < 2 || scope.MaxProficiency > 10 {
			return nil, ErrInvalidScope
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT `+wordColumnsAliased+`
			FROM words w
			WHERE w.notebook_id = ? AND w.proficiency < ?
			ORDER BY w.added_at DESC, w.id DESC`, notebookID, scope.MaxProficiency)
		if err != nil {
			return nil, fmt.Errorf("list recall words by proficiency: %w", err)
		}
		return collectWords(rows)

	case ScopeGroups:
		ids := uniqueSortedIDs(scope.GroupIDs)
		if len(ids) == 0 {
			return nil, ErrInvalidScope
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		args := make([]any, 0, len(ids)+1)
		args = append(args, notebookID)
		for _, id := range ids {
			args = append(args, id)
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT DISTINCT `+wordColumnsAliased+`
			FROM words w
			JOIN word_group_members m ON m.word_id = w.id
			WHERE w.notebook_id = ? AND m.group_id IN (`+placeholders+`)
			ORDER BY w.added_at DESC, w.id DESC`, args...)
		if err != nil {
			return nil, fmt.Errorf("list recall words by groups: %w", err)
		}
		return collectWords(rows)

	default:
		return nil, ErrInvalidScope
	}
}

// collectWords drains rows into a non-nil Word slice.
func collectWords(rows *sql.Rows) ([]Word, error) {
	defer rows.Close()
	out := []Word{}
	for rows.Next() {
		w, err := scanWord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// uniqueSortedIDs returns the distinct positive ids in ascending order.
func uniqueSortedIDs(ids []int64) []int64 {
	out := make([]int64, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
