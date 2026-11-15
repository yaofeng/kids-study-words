package store

import (
	"context"
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

	query := `SELECT ` + recallLogColumns + `
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
		l, err := scanRecallLog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
