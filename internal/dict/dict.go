// Package dict queries the read-only dictionary built by cmd/dictbuild.
package dict

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a word is absent from the dictionary.
var ErrNotFound = errors.New("word not found")

// Entry is one dictionary row.
type Entry struct {
	Word        string
	Phonetic    string
	Translation string
	Definition  string
	POS         string
	Collins     int
	Oxford      int
	Tag         string
	BNC         int
	FRQ         int
	Exchange    string
}

// Sentence is one bilingual example sentence.
type Sentence struct {
	EN     string
	ZH     string
	Source string
}

// Dict is a read-only handle to dict.db.
type Dict struct {
	db *sql.DB
}

// Open opens the dictionary read-only.
func Open(path string) (*Dict, error) {
	dsn := "file:" + filepath.ToSlash(path) + "?mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open dictionary: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open dictionary: %w", err)
	}
	return &Dict{db: db}, nil
}

// Close releases the dictionary handle.
func (d *Dict) Close() error { return d.db.Close() }

const entryColumns = `word, phonetic, translation, definition, pos, collins, oxford, tag, bnc, frq, exchange`

func scanEntry(row interface{ Scan(...any) error }) (Entry, error) {
	var e Entry
	if err := row.Scan(&e.Word, &e.Phonetic, &e.Translation, &e.Definition, &e.POS,
		&e.Collins, &e.Oxford, &e.Tag, &e.BNC, &e.FRQ, &e.Exchange); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Entry{}, ErrNotFound
		}
		return Entry{}, err
	}
	return e, nil
}

func collectEntries(rows *sql.Rows) ([]Entry, error) {
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func clampLimit(limit, max int) int {
	if limit <= 0 || limit > max {
		return max
	}
	return limit
}

// escapeLike escapes wildcards for a LIKE ... ESCAPE '\' pattern.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// SearchEnglish finds exact and prefix matches, exact first, then by frequency.
func (d *Dict) SearchEnglish(ctx context.Context, q string, limit int) ([]Entry, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	limit = clampLimit(limit, 20)
	rows, err := d.db.QueryContext(ctx,
		`SELECT `+entryColumns+` FROM entries
		 WHERE word = ? COLLATE NOCASE OR word LIKE ? ESCAPE '\' COLLATE NOCASE
		 ORDER BY (word = ? COLLATE NOCASE) DESC,
		          COALESCE(NULLIF(frq, 0), 1000000),
		          COALESCE(NULLIF(bnc, 0), 1000000),
		          length(word)
		 LIMIT ?`,
		q, escapeLike(q)+"%", q, limit)
	if err != nil {
		return nil, fmt.Errorf("search english: %w", err)
	}
	return collectEntries(rows)
}

// SearchChinese finds entries whose translation contains q, common words first.
func (d *Dict) SearchChinese(ctx context.Context, q string, limit int) ([]Entry, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	limit = clampLimit(limit, 30)
	rows, err := d.db.QueryContext(ctx,
		`SELECT `+entryColumns+` FROM entries
		 WHERE translation LIKE ? ESCAPE '\'
		   AND (bnc > 0 OR frq > 0 OR tag <> '')
		 ORDER BY COALESCE(NULLIF(frq, 0), 1000000),
		          COALESCE(NULLIF(bnc, 0), 1000000),
		          length(word)
		 LIMIT ?`,
		"%"+escapeLike(q)+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("search chinese: %w", err)
	}
	return collectEntries(rows)
}

// Lookup returns the exact entry for word or ErrNotFound.
func (d *Dict) Lookup(ctx context.Context, word string) (Entry, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT `+entryColumns+` FROM entries WHERE word = ? COLLATE NOCASE`, strings.TrimSpace(word))
	return scanEntry(row)
}

// Examples returns up to limit sentences containing the word or one of its
// forms, preferring sentences with the base word and then shorter ones.
func (d *Dict) Examples(ctx context.Context, word, exchange string, limit int) ([]Sentence, error) {
	forms := Forms(word, exchange)
	if len(forms) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 2
	}
	quoted := make([]string, 0, len(forms))
	for _, f := range forms {
		quoted = append(quoted, `"`+strings.ReplaceAll(f, `"`, `""`)+`"`)
	}
	rows, err := d.db.QueryContext(ctx,
		`SELECT s.en, s.zh FROM sentences_fts f JOIN sentences s ON s.id = f.rowid
		 WHERE sentences_fts MATCH ? LIMIT 10`,
		strings.Join(quoted, " OR "))
	if err != nil {
		return nil, fmt.Errorf("examples: %w", err)
	}
	defer rows.Close()

	base := strings.ToLower(strings.TrimSpace(word))
	var found []Sentence
	for rows.Next() {
		var s Sentence
		if err := rows.Scan(&s.EN, &s.ZH); err != nil {
			return nil, err
		}
		s.Source = "tatoeba"
		found = append(found, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.SliceStable(found, func(i, j int) bool {
		bi := containsWord(found[i].EN, base)
		bj := containsWord(found[j].EN, base)
		if bi != bj {
			return bi
		}
		return len(found[i].EN) < len(found[j].EN)
	})
	if len(found) > limit {
		found = found[:limit]
	}
	return found, nil
}

// containsWord reports whether text contains word as a whole word,
// case-insensitively.
func containsWord(text, word string) bool {
	if word == "" {
		return false
	}
	lower := strings.ToLower(text)
	for {
		i := strings.Index(lower, word)
		if i < 0 {
			return false
		}
		leftOK := i == 0 || !isWordByte(lower[i-1])
		end := i + len(word)
		rightOK := end >= len(lower) || !isWordByte(lower[end])
		if leftOK && rightOK {
			return true
		}
		lower = lower[i+len(word):]
	}
}

func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '\''
}
