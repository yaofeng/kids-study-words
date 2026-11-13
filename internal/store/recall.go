package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AnswerWord applies a recall answer: it moves proficiency by one (clamped to
// 1–10), stamps last_reviewed_at, bumps review_count, and appends a snapshot
// to recall_logs in one transaction.
func (s *Store) AnswerWord(ctx context.Context, notebookID, wordID int64, known bool, now time.Time) (Word, RecallLog, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Word{}, RecallLog{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var proficiency, reviewCount int
	var wordText, notebookName string
	var userID int64
	err = tx.QueryRowContext(ctx, `
		SELECT w.proficiency, w.review_count, w.word, n.name, n.user_id
		FROM words w JOIN notebooks n ON n.id = w.notebook_id
		WHERE w.id = ? AND w.notebook_id = ?`, wordID, notebookID).
		Scan(&proficiency, &reviewCount, &wordText, &notebookName, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return Word{}, RecallLog{}, ErrNotFound
	}
	if err != nil {
		return Word{}, RecallLog{}, fmt.Errorf("load word: %w", err)
	}

	newProficiency := proficiency
	if known {
		newProficiency++
	} else {
		newProficiency--
	}
	if newProficiency < 1 {
		newProficiency = 1
	}
	if newProficiency > 10 {
		newProficiency = 10
	}
	newCount := reviewCount + 1
	stamp := formatTime(now)

	if _, err := tx.ExecContext(ctx,
		`UPDATE words SET proficiency = ?, last_reviewed_at = ?, review_count = ? WHERE id = ?`,
		newProficiency, stamp, newCount, wordID); err != nil {
		return Word{}, RecallLog{}, fmt.Errorf("update word: %w", err)
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO recall_logs
		  (user_id, notebook_id, notebook_name, word_id, word, known, proficiency_after, review_count, reviewed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, notebookID, notebookName, wordID, wordText, boolInt(known), newProficiency, newCount, stamp)
	if err != nil {
		return Word{}, RecallLog{}, fmt.Errorf("insert recall log: %w", err)
	}
	logID, err := res.LastInsertId()
	if err != nil {
		return Word{}, RecallLog{}, fmt.Errorf("recall log id: %w", err)
	}

	word, err := scanWord(tx.QueryRowContext(ctx, `SELECT `+wordColumns+` FROM words WHERE id = ?`, wordID))
	if err != nil {
		return Word{}, RecallLog{}, err
	}
	if err := tx.Commit(); err != nil {
		return Word{}, RecallLog{}, fmt.Errorf("commit: %w", err)
	}

	log := RecallLog{
		ID:               logID,
		UserID:           userID,
		NotebookID:       &notebookID,
		NotebookName:     notebookName,
		WordID:           &wordID,
		Word:             wordText,
		Known:            known,
		ProficiencyAfter: newProficiency,
		ReviewCount:      newCount,
		ReviewedAt:       now.UTC(),
	}
	return word, log, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
