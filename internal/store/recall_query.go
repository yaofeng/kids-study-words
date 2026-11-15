package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// RecallLogFilter describes the optional filters and paging of QueryRecallLogs.
// Nil pointer fields mean "no constraint"; Limit<=0 means 200 and is capped at
// 1000; Offset<0 means 0.
type RecallLogFilter struct {
	From, To       *time.Time
	Word           string
	NotebookID     *int64
	GroupID        *int64
	Known          *bool
	ProficiencyMin *int
	ProficiencyMax *int
	ReviewCountMin *int
	ReviewCountMax *int
	Ascending      bool
	Limit, Offset  int
}

// recallLogColumns is the column list shared by recall log scans, in the order
// scanRecallLog expects.
const recallLogColumns = `id, user_id, notebook_id, notebook_name, word_id, word,
	known, proficiency_after, review_count, reviewed_at`

// scanRecallLog scans one recall_logs row, converting nullable IDs and the
// string timestamp.
func scanRecallLog(row interface{ Scan(...any) error }) (RecallLog, error) {
	var l RecallLog
	var notebook sql.NullInt64
	var wordID sql.NullInt64
	var known int
	var reviewed string
	if err := row.Scan(&l.ID, &l.UserID, &notebook, &l.NotebookName, &wordID, &l.Word,
		&known, &l.ProficiencyAfter, &l.ReviewCount, &reviewed); err != nil {
		return RecallLog{}, fmt.Errorf("scan recall log: %w", err)
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
		return RecallLog{}, err
	}
	return l, nil
}

// QueryRecallLogs returns the user's recall logs matching f plus the total
// number of matches before paging. Results are ordered by reviewed_at then id,
// descending unless f.Ascending, and the order is stable. Empty results are a
// non-nil empty slice.
func (s *Store) QueryRecallLogs(ctx context.Context, userID int64, f RecallLogFilter) ([]RecallLog, int, error) {
	where := []string{"user_id = ?"}
	args := []any{userID}
	if f.From != nil {
		where = append(where, "reviewed_at >= ?")
		args = append(args, formatTime(*f.From))
	}
	if f.To != nil {
		where = append(where, "reviewed_at <= ?")
		args = append(args, formatTime(*f.To))
	}
	if w := strings.TrimSpace(f.Word); w != "" {
		where = append(where, "LOWER(word) = LOWER(?)")
		args = append(args, w)
	}
	if f.NotebookID != nil {
		where = append(where, "notebook_id = ?")
		args = append(args, *f.NotebookID)
	}
	if f.GroupID != nil {
		where = append(where, "word_id IN (SELECT word_id FROM word_group_members WHERE group_id = ?)")
		args = append(args, *f.GroupID)
	}
	if f.Known != nil {
		where = append(where, "known = ?")
		args = append(args, boolInt(*f.Known))
	}
	if f.ProficiencyMin != nil {
		where = append(where, "proficiency_after >= ?")
		args = append(args, *f.ProficiencyMin)
	}
	if f.ProficiencyMax != nil {
		where = append(where, "proficiency_after <= ?")
		args = append(args, *f.ProficiencyMax)
	}
	if f.ReviewCountMin != nil {
		where = append(where, "review_count >= ?")
		args = append(args, *f.ReviewCountMin)
	}
	if f.ReviewCountMax != nil {
		where = append(where, "review_count <= ?")
		args = append(args, *f.ReviewCountMax)
	}
	clause := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM recall_logs WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count recall logs: %w", err)
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	dir := "DESC"
	if f.Ascending {
		dir = "ASC"
	}

	query := `SELECT ` + recallLogColumns + ` FROM recall_logs WHERE ` + clause +
		` ORDER BY reviewed_at ` + dir + `, id ` + dir + ` LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query recall logs: %w", err)
	}
	defer rows.Close()

	out := []RecallLog{}
	for rows.Next() {
		l, err := scanRecallLog(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}
