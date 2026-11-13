package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// RecallLog is one answered card. Notebook and word names are snapshots so
// history survives deletion.
type RecallLog struct {
	ID               int64
	UserID           int64
	NotebookID       *int64
	NotebookName     string
	WordID           *int64
	Word             string
	Known            bool
	ProficiencyAfter int
	ReviewCount      int
	ReviewedAt       time.Time
}

// ListRecallLogs returns a user's history newest first. beforeID=0 starts at
// the newest; pass the smallest ID of the previous page for the next page.
func (s *Store) ListRecallLogs(ctx context.Context, userID int64, notebookID *int64, beforeID int64, limit int) ([]RecallLog, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}

	query := `SELECT id, user_id, notebook_id, notebook_name, word_id, word, known,
	                 proficiency_after, review_count, reviewed_at
	          FROM recall_logs WHERE user_id = ?`
	args := []any{userID}
	if notebookID != nil {
		query += ` AND notebook_id = ?`
		args = append(args, *notebookID)
	}
	if beforeID > 0 {
		query += ` AND id < ?`
		args = append(args, beforeID)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list recall logs: %w", err)
	}
	defer rows.Close()

	var out []RecallLog
	for rows.Next() {
		var l RecallLog
		var notebook sql.NullInt64
		var wordID sql.NullInt64
		var known int
		var reviewed string
		if err := rows.Scan(&l.ID, &l.UserID, &notebook, &l.NotebookName, &wordID, &l.Word,
			&known, &l.ProficiencyAfter, &l.ReviewCount, &reviewed); err != nil {
			return nil, fmt.Errorf("scan recall log: %w", err)
		}
		if notebook.Valid {
			id := notebook.Int64
			l.NotebookID = &id
		}
		if wordID.Valid {
			id := wordID.Int64
			l.WordID = &id
		}
		l.Known = known == 1
		if err := scanTime(&l.ReviewedAt, reviewed); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
