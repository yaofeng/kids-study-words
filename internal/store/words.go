package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Example is one bilingual sentence attached to a word.
type Example struct {
	EN     string `json:"en"`
	ZH     string `json:"zh"`
	Source string `json:"source"`
}

// Word is a vocabulary item inside a notebook.
type Word struct {
	ID             int64
	NotebookID     int64
	Word           string
	Phonetic       string
	Translation    string
	Definition     string
	Examples       []Example
	Proficiency    int
	AddedAt        time.Time
	LastReviewedAt *time.Time
	ReviewCount    int
}

const wordColumns = `id, notebook_id, word, phonetic, translation, definition, examples_json,
	proficiency, added_at, last_reviewed_at, review_count`

func scanWord(row interface{ Scan(...any) error }) (Word, error) {
	var w Word
	var examplesJSON string
	var added string
	var lastReviewed sql.NullString
	if err := row.Scan(&w.ID, &w.NotebookID, &w.Word, &w.Phonetic, &w.Translation, &w.Definition,
		&examplesJSON, &w.Proficiency, &added, &lastReviewed, &w.ReviewCount); err != nil {
		return Word{}, mapNoRows(err)
	}
	if err := scanTime(&w.AddedAt, added); err != nil {
		return Word{}, err
	}
	if err := scanNullTime(&w.LastReviewedAt, lastReviewed); err != nil {
		return Word{}, err
	}
	w.Examples = []Example{}
	if examplesJSON != "" {
		if err := json.Unmarshal([]byte(examplesJSON), &w.Examples); err != nil {
			return Word{}, fmt.Errorf("parse examples for %q: %w", w.Word, err)
		}
	}
	return w, nil
}

func encodeExamples(examples []Example) (string, error) {
	if examples == nil {
		examples = []Example{}
	}
	b, err := json.Marshal(examples)
	if err != nil {
		return "", fmt.Errorf("encode examples: %w", err)
	}
	return string(b), nil
}

// ListWords returns a notebook's words ordered by the requested whitelist key.
func (s *Store) ListWords(ctx context.Context, notebookID int64, sort string) ([]Word, error) {
	order := "added_at DESC, id DESC"
	switch sort {
	case "proficiency":
		order = "proficiency ASC, added_at DESC, id DESC"
	case "reviewed":
		order = "last_reviewed_at IS NULL, last_reviewed_at DESC, added_at DESC, id DESC"
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+wordColumns+` FROM words WHERE notebook_id = ? ORDER BY `+order, notebookID)
	if err != nil {
		return nil, fmt.Errorf("list words: %w", err)
	}
	defer rows.Close()

	var out []Word
	for rows.Next() {
		w, err := scanWord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// WordNotebooks maps lowercased words to the names of the user's notebooks
// that contain them. Notebooks are ordered default first, then by name.
// Words with no match are absent from the map.
func (s *Store) WordNotebooks(ctx context.Context, userID int64, words []string) (map[string][]string, error) {
	out := make(map[string][]string)
	seen := make(map[string]bool, len(words))
	args := make([]any, 0, len(words)+1)
	args = append(args, userID)
	for _, raw := range words {
		w := strings.ToLower(strings.TrimSpace(raw))
		if w == "" || seen[w] {
			continue
		}
		seen[w] = true
		args = append(args, w)
	}
	if len(args) == 1 {
		return out, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(args)-1), ",")
	rows, err := s.db.QueryContext(ctx, `
		SELECT LOWER(w.word), n.name
		FROM words w
		JOIN notebooks n ON n.id = w.notebook_id
		WHERE n.user_id = ? AND LOWER(w.word) IN (`+placeholders+`)
		ORDER BY n.is_default DESC, n.name COLLATE NOCASE ASC, n.id ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("find word notebooks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var word, name string
		if err := rows.Scan(&word, &name); err != nil {
			return nil, fmt.Errorf("scan word notebook: %w", err)
		}
		out[word] = append(out[word], name)
	}
	return out, rows.Err()
}

// WordByID loads one word scoped to its notebook.
func (s *Store) WordByID(ctx context.Context, notebookID, id int64) (Word, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+wordColumns+` FROM words WHERE id = ? AND notebook_id = ?`, id, notebookID)
	return scanWord(row)
}

// WordByText loads one word by its case-insensitive text within a notebook.
func (s *Store) WordByText(ctx context.Context, notebookID int64, text string) (Word, error) {
	return scanWord(s.db.QueryRowContext(ctx,
		`SELECT `+wordColumns+` FROM words WHERE notebook_id = ? AND word = ? COLLATE NOCASE`,
		notebookID, strings.TrimSpace(text)))
}

// AddWord inserts a word. Zero proficiency means the starting value 5; a zero
// AddedAt means now. LastReviewedAt and ReviewCount are kept as provided so
// imports can restore history.
func (s *Store) AddWord(ctx context.Context, notebookID int64, w Word) (Word, error) {
	w.Word = strings.TrimSpace(w.Word)
	if w.Word == "" {
		return Word{}, errors.New("word is required")
	}
	var exists int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM words WHERE notebook_id = ? AND word = ? COLLATE NOCASE`,
		notebookID, w.Word).Scan(&exists)
	if err != nil {
		return Word{}, fmt.Errorf("check duplicate word: %w", err)
	}
	if exists > 0 {
		return Word{}, ErrDuplicate
	}
	if w.Proficiency == 0 {
		w.Proficiency = 5
	}
	if w.AddedAt.IsZero() {
		w.AddedAt = TimeNow()
	}
	examplesJSON, err := encodeExamples(w.Examples)
	if err != nil {
		return Word{}, err
	}
	var lastReviewed any
	if w.LastReviewedAt != nil {
		lastReviewed = formatTime(*w.LastReviewedAt)
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO words (notebook_id, word, phonetic, translation, definition, examples_json,
			proficiency, added_at, last_reviewed_at, review_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		notebookID, w.Word, w.Phonetic, w.Translation, w.Definition, examplesJSON,
		w.Proficiency, formatTime(w.AddedAt), lastReviewed, w.ReviewCount)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Word{}, ErrDuplicate
		}
		return Word{}, fmt.Errorf("insert word: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Word{}, fmt.Errorf("word id: %w", err)
	}
	if err := s.TouchNotebook(ctx, notebookID); err != nil {
		return Word{}, err
	}
	w.ID = id
	w.NotebookID = notebookID
	return w, nil
}

// UpdateWord replaces a word's translation and examples, leaving progress intact.
func (s *Store) UpdateWord(ctx context.Context, notebookID, id int64, translation string, examples []Example) error {
	examplesJSON, err := encodeExamples(examples)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE words SET translation = ?, examples_json = ? WHERE id = ? AND notebook_id = ?`,
		translation, examplesJSON, id, notebookID)
	if err != nil {
		return fmt.Errorf("update word: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update word: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return s.TouchNotebook(ctx, notebookID)
}

// DeleteWord removes a word from a notebook.
func (s *Store) DeleteWord(ctx context.Context, notebookID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM words WHERE id = ? AND notebook_id = ?`, id, notebookID)
	if err != nil {
		return fmt.Errorf("delete word: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete word: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return s.TouchNotebook(ctx, notebookID)
}
