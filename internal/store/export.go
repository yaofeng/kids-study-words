package store

import (
	"context"
	"fmt"
	"time"
)

const (
	exportFormat  = "study-words"
	exportVersion = 1
)

// ExportFile is the JSON document exchanged by export and import.
type ExportFile struct {
	Format     string           `json:"format"`
	Version    int              `json:"version"`
	ExportedAt time.Time        `json:"exported_at"`
	Notebooks  []ExportNotebook `json:"notebooks"`
	RecallLogs []ExportLog      `json:"recall_logs"`
}

// ExportNotebook is one notebook in an export file.
type ExportNotebook struct {
	Name      string       `json:"name"`
	IsDefault bool         `json:"is_default"`
	Words     []ExportWord `json:"words"`
}

// ExportWord is one vocabulary item in an export file.
type ExportWord struct {
	Word           string     `json:"word"`
	Phonetic       string     `json:"phonetic"`
	Translation    string     `json:"translation"`
	Definition     string     `json:"definition"`
	Examples       []Example  `json:"examples"`
	Proficiency    int        `json:"proficiency"`
	AddedAt        time.Time  `json:"added_at"`
	LastReviewedAt *time.Time `json:"last_reviewed_at"`
	ReviewCount    int        `json:"review_count"`
}

// ExportLog is one recall history row in an export file. Notebook and word are
// snapshots.
type ExportLog struct {
	Notebook         string    `json:"notebook"`
	Word             string    `json:"word"`
	Known            bool      `json:"known"`
	ProficiencyAfter int       `json:"proficiency_after"`
	ReviewCount      int       `json:"review_count"`
	ReviewedAt       time.Time `json:"reviewed_at"`
}

// ExportAll exports every notebook, word, and recall log for a user.
func (s *Store) ExportAll(ctx context.Context, userID int64) (ExportFile, error) {
	return s.export(ctx, userID, nil)
}

// ExportNotebook exports one notebook, its words, and its recall logs.
func (s *Store) ExportNotebook(ctx context.Context, userID, notebookID int64) (ExportFile, error) {
	nb, err := s.NotebookByID(ctx, userID, notebookID)
	if err != nil {
		return ExportFile{}, err
	}
	return s.export(ctx, userID, &nb.ID)
}

func (s *Store) export(ctx context.Context, userID int64, only *int64) (ExportFile, error) {
	notebooks, err := s.ListNotebooks(ctx, userID)
	if err != nil {
		return ExportFile{}, err
	}
	f := ExportFile{
		Format:     exportFormat,
		Version:    exportVersion,
		ExportedAt: TimeNow(),
		Notebooks:  []ExportNotebook{},
		RecallLogs: []ExportLog{},
	}
	for _, nb := range notebooks {
		if only != nil && nb.ID != *only {
			continue
		}
		words, err := s.ListWords(ctx, nb.ID, "added")
		if err != nil {
			return ExportFile{}, err
		}
		en := ExportNotebook{Name: nb.Name, IsDefault: nb.IsDefault, Words: make([]ExportWord, 0, len(words))}
		for _, w := range words {
			examples := w.Examples
			if examples == nil {
				examples = []Example{}
			}
			en.Words = append(en.Words, ExportWord{
				Word:           w.Word,
				Phonetic:       w.Phonetic,
				Translation:    w.Translation,
				Definition:     w.Definition,
				Examples:       examples,
				Proficiency:    w.Proficiency,
				AddedAt:        w.AddedAt,
				LastReviewedAt: w.LastReviewedAt,
				ReviewCount:    w.ReviewCount,
			})
		}
		f.Notebooks = append(f.Notebooks, en)
	}
	logs, err := s.exportLogs(ctx, userID, only)
	if err != nil {
		return ExportFile{}, err
	}
	f.RecallLogs = logs
	return f, nil
}

func (s *Store) exportLogs(ctx context.Context, userID int64, only *int64) ([]ExportLog, error) {
	query := `SELECT notebook_name, word, known, proficiency_after, review_count, reviewed_at
	          FROM recall_logs WHERE user_id = ?`
	args := []any{userID}
	if only != nil {
		query += ` AND notebook_id = ?`
		args = append(args, *only)
	}
	query += ` ORDER BY id DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("export logs: %w", err)
	}
	defer rows.Close()

	out := []ExportLog{}
	for rows.Next() {
		var l ExportLog
		var known int
		var reviewed string
		if err := rows.Scan(&l.Notebook, &l.Word, &known, &l.ProficiencyAfter, &l.ReviewCount, &reviewed); err != nil {
			return nil, fmt.Errorf("scan export log: %w", err)
		}
		l.Known = known == 1
		if err := scanTime(&l.ReviewedAt, reviewed); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
