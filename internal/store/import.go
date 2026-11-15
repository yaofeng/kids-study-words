package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// ImportMode selects how existing data is treated.
type ImportMode int

const (
	// ImportMerge adds to existing data and keeps the local default notebook.
	ImportMerge ImportMode = iota
	// ImportOverwrite clears the user's data first.
	ImportOverwrite
)

// ImportStats summarises what an import changed.
type ImportStats struct {
	NotebooksCreated      int
	WordsCreated          int
	WordsUpdated          int
	WordsSkipped          int
	LogsAdded             int
	GroupsCreated         int
	GroupMembershipsAdded int
}

// ValidateExport checks the document before any transaction starts.
func ValidateExport(f ExportFile) error {
	if f.Format != exportFormat {
		return fmt.Errorf("不是少儿背单词的导出文件（format=%q）", f.Format)
	}
	if f.Version != 1 && f.Version != 2 {
		return fmt.Errorf("不支持的导出文件版本 %d", f.Version)
	}
	if len(f.Notebooks) == 0 {
		return errors.New("文件里没有生词本")
	}
	if len(f.Notebooks) > 1000 {
		return errors.New("生词本数量超过 1000 个")
	}
	if len(f.RecallLogs) > 100000 {
		return errors.New("回忆记录超过 100000 条")
	}
	for _, nb := range f.Notebooks {
		name := strings.TrimSpace(nb.Name)
		if n := utf8.RuneCountInString(name); n == 0 || n > 100 {
			return fmt.Errorf("生词本名称需为 1–100 个字符：%q", nb.Name)
		}
		if len(nb.Words) > 10000 {
			return fmt.Errorf("生词本「%s」的单词超过 10000 个", nb.Name)
		}
		for _, w := range nb.Words {
			if n := utf8.RuneCountInString(strings.TrimSpace(w.Word)); n == 0 || n > 100 {
				return fmt.Errorf("单词需为 1–100 个字符：%q", w.Word)
			}
			if w.Proficiency < 1 || w.Proficiency > 10 {
				return fmt.Errorf("「%s」的熟练度 %d 不在 1–10 之间", w.Word, w.Proficiency)
			}
			if len(w.Examples) > 5 {
				return fmt.Errorf("「%s」的例句超过 5 条", w.Word)
			}
			if utf8.RuneCountInString(w.Translation) > 4000 || utf8.RuneCountInString(w.Definition) > 4000 {
				return fmt.Errorf("「%s」的释义过长", w.Word)
			}
			for _, ex := range w.Examples {
				if utf8.RuneCountInString(ex.EN) > 500 || utf8.RuneCountInString(ex.ZH) > 500 {
					return fmt.Errorf("「%s」有例句过长", w.Word)
				}
			}
		}
		if f.Version >= 2 {
			if err := validateGroups(nb); err != nil {
				return err
			}
		}
	}
	for _, l := range f.RecallLogs {
		word := strings.TrimSpace(l.Word)
		if n := utf8.RuneCountInString(word); n == 0 || n > 100 {
			return fmt.Errorf("回忆记录的单词需为 1–100 个字符：%q", l.Word)
		}
		if utf8.RuneCountInString(strings.TrimSpace(l.Notebook)) > 100 {
			return fmt.Errorf("回忆记录「%s」的生词本名称过长", l.Word)
		}
		if l.ProficiencyAfter < 1 || l.ProficiencyAfter > 10 {
			return fmt.Errorf("回忆记录「%s」的熟练度 %d 不在 1–10 之间", l.Word, l.ProficiencyAfter)
		}
		if l.ReviewCount < 0 {
			return fmt.Errorf("回忆记录「%s」的回忆次数不能为负", l.Word)
		}
		if l.ReviewedAt.IsZero() {
			return fmt.Errorf("回忆记录「%s」缺少时间", l.Word)
		}
	}
	return nil
}

// validateGroups checks the group section of a version 2 notebook.
func validateGroups(nb ExportNotebook) error {
	if len(nb.Groups) > 100 {
		return fmt.Errorf("生词本「%s」的分组超过 100 个", nb.Name)
	}
	wordSet := make(map[string]bool, len(nb.Words))
	for _, w := range nb.Words {
		wordSet[strings.ToLower(strings.TrimSpace(w.Word))] = true
	}
	groupNames := make(map[string]bool, len(nb.Groups))
	for _, g := range nb.Groups {
		name := strings.TrimSpace(g.Name)
		if n := utf8.RuneCountInString(name); n == 0 || n > 40 {
			return fmt.Errorf("分组名称需为 1–40 个字符：%q", g.Name)
		}
		key := strings.ToLower(name)
		if groupNames[key] {
			return fmt.Errorf("生词本「%s」有重名分组：%q", nb.Name, name)
		}
		groupNames[key] = true
		if len(g.Words) > 10000 {
			return fmt.Errorf("分组「%s」的单词超过 10000 个", name)
		}
		seen := make(map[string]bool, len(g.Words))
		for _, w := range g.Words {
			word := strings.TrimSpace(w)
			if n := utf8.RuneCountInString(word); n == 0 || n > 100 {
				return fmt.Errorf("分组「%s」的单词需为 1–100 个字符：%q", name, w)
			}
			wkey := strings.ToLower(word)
			if !wordSet[wkey] {
				return fmt.Errorf("分组「%s」的单词不在生词本中：%q", name, w)
			}
			if seen[wkey] {
				return fmt.Errorf("分组「%s」有重复单词：%q", name, w)
			}
			seen[wkey] = true
		}
	}
	return nil
}

// Import writes a validated export file in one transaction.
func (s *Store) Import(ctx context.Context, userID int64, f ExportFile, mode ImportMode) (ImportStats, error) {
	var stats ImportStats
	if err := ValidateExport(f); err != nil {
		return stats, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return stats, fmt.Errorf("begin import: %w", err)
	}
	defer tx.Rollback()

	if mode == ImportOverwrite {
		// Deleting notebooks cascades to their words, groups, and members.
		if _, err := tx.ExecContext(ctx, `DELETE FROM recall_logs WHERE user_id = ?`, userID); err != nil {
			return stats, fmt.Errorf("clear logs: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM notebooks WHERE user_id = ?`, userID); err != nil {
			return stats, fmt.Errorf("clear notebooks: %w", err)
		}
	}

	nbIDs, err := existingNotebooks(ctx, tx, userID)
	if err != nil {
		return stats, err
	}
	existingDefault, err := defaultNotebookID(ctx, tx, userID)
	if err != nil {
		return stats, err
	}

	var fileDefaultID, firstNBID int64
	for _, nb := range f.Notebooks {
		name := strings.TrimSpace(nb.Name)
		key := strings.ToLower(name)
		id, exists := nbIDs[key]
		if !exists {
			res, err := tx.ExecContext(ctx,
				`INSERT INTO notebooks (user_id, name, is_default, created_at, updated_at) VALUES (?, ?, 0, ?, ?)`,
				userID, name, formatTime(TimeNow()), formatTime(TimeNow()))
			if err != nil {
				return stats, fmt.Errorf("create notebook %q: %w", name, err)
			}
			id, err = res.LastInsertId()
			if err != nil {
				return stats, fmt.Errorf("notebook id: %w", err)
			}
			nbIDs[key] = id
			stats.NotebooksCreated++
		}
		if firstNBID == 0 {
			firstNBID = id
		}
		if nb.IsDefault && fileDefaultID == 0 {
			fileDefaultID = id
		}
		for _, w := range nb.Words {
			action, err := importWord(ctx, tx, id, w)
			if err != nil {
				return stats, err
			}
			switch action {
			case wordCreated:
				stats.WordsCreated++
			case wordUpdated:
				stats.WordsUpdated++
			case wordSkipped:
				stats.WordsSkipped++
			}
		}
		if f.Version >= 2 && len(nb.Groups) > 0 {
			if err := importGroups(ctx, tx, id, nb.Groups, &stats); err != nil {
				return stats, err
			}
		}
	}

	logsAdded, err := importLogs(ctx, tx, userID, f.RecallLogs, mode, nbIDs)
	if err != nil {
		return stats, err
	}
	stats.LogsAdded = logsAdded

	// Exactly one default notebook: keep the current one when valid, else use
	// the file's marker, else the first notebook.
	if existingDefault == 0 || !notebookExists(ctx, tx, userID, existingDefault) {
		target := fileDefaultID
		if target == 0 {
			target = firstNBID
		}
		if target == 0 {
			if err := tx.QueryRowContext(ctx,
				`SELECT id FROM notebooks WHERE user_id = ? ORDER BY id ASC LIMIT 1`, userID).Scan(&target); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return stats, errors.New("导入后没有生词本")
				}
				return stats, fmt.Errorf("pick default notebook: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE notebooks SET is_default = 0 WHERE user_id = ?`, userID); err != nil {
			return stats, fmt.Errorf("clear default: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE notebooks SET is_default = 1 WHERE id = ? AND user_id = ?`, target, userID); err != nil {
			return stats, fmt.Errorf("set default: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return stats, fmt.Errorf("commit import: %w", err)
	}
	return stats, nil
}

type wordAction int

const (
	wordSkipped wordAction = iota
	wordCreated
	wordUpdated
)

func existingNotebooks(ctx context.Context, tx *sql.Tx, userID int64) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, name FROM notebooks WHERE user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("load notebooks: %w", err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[strings.ToLower(name)] = id
	}
	return out, rows.Err()
}

func defaultNotebookID(ctx context.Context, tx *sql.Tx, userID int64) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM notebooks WHERE user_id = ? AND is_default = 1 LIMIT 1`, userID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("find default notebook: %w", err)
	}
	return id, nil
}

func notebookExists(ctx context.Context, tx *sql.Tx, userID, id int64) bool {
	var n int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notebooks WHERE id = ? AND user_id = ?`, id, userID).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

func importWord(ctx context.Context, tx *sql.Tx, notebookID int64, w ExportWord) (wordAction, error) {
	word := strings.TrimSpace(w.Word)
	examplesJSON, err := encodeExamples(w.Examples)
	if err != nil {
		return wordSkipped, err
	}
	addedAt := w.AddedAt
	if addedAt.IsZero() {
		addedAt = TimeNow()
	}
	var lastReviewed any
	if w.LastReviewedAt != nil {
		lastReviewed = formatTime(*w.LastReviewedAt)
	}
	reviewCount := w.ReviewCount
	if reviewCount < 0 {
		reviewCount = 0
	}

	var (
		id           int64
		prof         int
		translation  string
		definition   string
		phonetic     string
		examples     string
		existingAdd  string
		existingLast sql.NullString
		existingRev  int
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, proficiency, translation, definition, phonetic, examples_json, added_at, last_reviewed_at, review_count
		FROM words WHERE notebook_id = ? AND word = ? COLLATE NOCASE`, notebookID, word).
		Scan(&id, &prof, &translation, &definition, &phonetic, &examples, &existingAdd, &existingLast, &existingRev)
	if errors.Is(err, sql.ErrNoRows) {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO words (notebook_id, word, phonetic, translation, definition, examples_json,
				proficiency, added_at, last_reviewed_at, review_count)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			notebookID, word, w.Phonetic, w.Translation, w.Definition, examplesJSON,
			w.Proficiency, formatTime(addedAt), lastReviewed, reviewCount)
		if err != nil {
			return wordSkipped, fmt.Errorf("insert word %q: %w", word, err)
		}
		return wordCreated, nil
	}
	if err != nil {
		return wordSkipped, fmt.Errorf("find word %q: %w", word, err)
	}

	newProf := prof
	if w.Proficiency > newProf {
		newProf = w.Proficiency
	}
	newCount := existingRev
	if reviewCount > newCount {
		newCount = reviewCount
	}
	localAdded, err := parseTime(existingAdd)
	if err != nil {
		return wordSkipped, err
	}
	newAdded := localAdded
	if addedAt.Before(newAdded) {
		newAdded = addedAt
	}
	newLast := existingLast
	if existingLast.Valid {
		localLast, err := parseTime(existingLast.String)
		if err != nil {
			return wordSkipped, err
		}
		if w.LastReviewedAt != nil && w.LastReviewedAt.After(localLast) {
			newLast = sql.NullString{String: formatTime(*w.LastReviewedAt), Valid: true}
		}
	} else if w.LastReviewedAt != nil {
		newLast = sql.NullString{String: formatTime(*w.LastReviewedAt), Valid: true}
	}

	mergedTranslation := translation
	if strings.TrimSpace(mergedTranslation) == "" {
		mergedTranslation = w.Translation
	}
	mergedDefinition := definition
	if strings.TrimSpace(mergedDefinition) == "" {
		mergedDefinition = w.Definition
	}
	mergedPhonetic := phonetic
	if strings.TrimSpace(mergedPhonetic) == "" {
		mergedPhonetic = w.Phonetic
	}
	mergedExamples := examples
	var localExamples []Example
	_ = json.Unmarshal([]byte(examples), &localExamples)
	if len(localExamples) == 0 && len(w.Examples) > 0 {
		mergedExamples = examplesJSON
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE words SET phonetic = ?, translation = ?, definition = ?, examples_json = ?,
			proficiency = ?, added_at = ?, last_reviewed_at = ?, review_count = ?
		WHERE id = ?`,
		mergedPhonetic, mergedTranslation, mergedDefinition, mergedExamples,
		newProf, formatTime(newAdded), newLast, newCount, id)
	if err != nil {
		return wordSkipped, fmt.Errorf("update word %q: %w", word, err)
	}
	return wordUpdated, nil
}

// importGroups creates or reuses groups in one notebook and adds their members.
// It runs inside the import transaction, so it only uses tx (the pool is capped
// at one connection). Validation has already checked that every group word
// exists in the notebook.
func importGroups(ctx context.Context, tx *sql.Tx, notebookID int64, groups []ExportGroup, stats *ImportStats) error {
	wordIDs := map[string]int64{}
	rows, err := tx.QueryContext(ctx, `SELECT id, word FROM words WHERE notebook_id = ?`, notebookID)
	if err != nil {
		return fmt.Errorf("load import words: %w", err)
	}
	for rows.Next() {
		var id int64
		var word string
		if err := rows.Scan(&id, &word); err != nil {
			rows.Close()
			return err
		}
		wordIDs[strings.ToLower(strings.TrimSpace(word))] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	groupIDs := map[string]int64{}
	rows, err = tx.QueryContext(ctx, `SELECT id, name FROM word_groups WHERE notebook_id = ?`, notebookID)
	if err != nil {
		return fmt.Errorf("load import groups: %w", err)
	}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return err
		}
		groupIDs[strings.ToLower(strings.TrimSpace(name))] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, g := range groups {
		name := strings.TrimSpace(g.Name)
		key := strings.ToLower(name)
		groupID, ok := groupIDs[key]
		if !ok {
			res, err := tx.ExecContext(ctx,
				`INSERT INTO word_groups (notebook_id, name, created_at) VALUES (?, ?, ?)`,
				notebookID, name, formatTime(TimeNow()))
			if err != nil {
				return fmt.Errorf("create group %q: %w", name, err)
			}
			groupID, err = res.LastInsertId()
			if err != nil {
				return fmt.Errorf("group id: %w", err)
			}
			groupIDs[key] = groupID
			stats.GroupsCreated++
		}
		for _, w := range g.Words {
			wordID, ok := wordIDs[strings.ToLower(strings.TrimSpace(w))]
			if !ok {
				return fmt.Errorf("分组「%s」的单词不在生词本中：%q", name, w)
			}
			res, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO word_group_members (group_id, word_id) VALUES (?, ?)`,
				groupID, wordID)
			if err != nil {
				return fmt.Errorf("add group member: %w", err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return fmt.Errorf("add group member: %w", err)
			}
			if n > 0 {
				stats.GroupMembershipsAdded++
			}
		}
	}
	return nil
}

func logKey(word string, reviewedAt time.Time, known bool) string {
	return strings.ToLower(strings.TrimSpace(word)) + "\x00" + formatTime(reviewedAt) + "\x00" + fmt.Sprint(known)
}

func importLogs(ctx context.Context, tx *sql.Tx, userID int64, logs []ExportLog, mode ImportMode, nbIDs map[string]int64) (int, error) {
	seen := make(map[string]bool)
	if mode == ImportMerge {
		rows, err := tx.QueryContext(ctx,
			`SELECT word, reviewed_at, known FROM recall_logs WHERE user_id = ?`, userID)
		if err != nil {
			return 0, fmt.Errorf("load existing logs: %w", err)
		}
		for rows.Next() {
			var word, reviewed string
			var known int
			if err := rows.Scan(&word, &reviewed, &known); err != nil {
				rows.Close()
				return 0, err
			}
			t, err := parseTime(reviewed)
			if err != nil {
				rows.Close()
				return 0, err
			}
			seen[logKey(word, t, known == 1)] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return 0, err
		}
	}

	added := 0
	for _, l := range logs {
		key := logKey(l.Word, l.ReviewedAt, l.Known)
		if seen[key] {
			continue
		}
		seen[key] = true

		var notebookID any
		var wordID any
		if id, ok := nbIDs[strings.ToLower(strings.TrimSpace(l.Notebook))]; ok {
			notebookID = id
			var wid int64
			err := tx.QueryRowContext(ctx,
				`SELECT id FROM words WHERE notebook_id = ? AND word = ? COLLATE NOCASE`, id, l.Word).Scan(&wid)
			if err == nil {
				wordID = wid
			} else if !errors.Is(err, sql.ErrNoRows) {
				return added, fmt.Errorf("resolve log word: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO recall_logs
			  (user_id, notebook_id, notebook_name, word_id, word, known, proficiency_after, review_count, reviewed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			userID, notebookID, l.Notebook, wordID, l.Word, boolInt(l.Known),
			l.ProficiencyAfter, l.ReviewCount, formatTime(l.ReviewedAt)); err != nil {
			return added, fmt.Errorf("insert log %q: %w", l.Word, err)
		}
		added++
	}
	return added, nil
}
