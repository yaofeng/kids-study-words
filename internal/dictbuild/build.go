// Package dictbuild converts ECDICT and Tatoeba source files into the
// read-only dict.db that ships with the application.
package dictbuild

import (
	"archive/zip"
	"bufio"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// Stats reports how many rows were written.
type Stats struct {
	Entries   int
	Sentences int
}

const schemaSQL = `
CREATE TABLE entries (
  word        TEXT PRIMARY KEY,
  phonetic    TEXT,
  translation TEXT,
  definition  TEXT,
  pos         TEXT,
  collins     INTEGER,
  oxford      INTEGER,
  tag         TEXT,
  bnc         INTEGER,
  frq         INTEGER,
  exchange    TEXT
);
CREATE INDEX entries_word ON entries(word COLLATE NOCASE);

CREATE TABLE sentences (
  id  INTEGER PRIMARY KEY,
  en  TEXT NOT NULL,
  zh  TEXT NOT NULL,
  src TEXT NOT NULL DEFAULT 'tatoeba'
);
CREATE INDEX sentences_en ON sentences(en);

CREATE VIRTUAL TABLE sentences_fts USING fts5(en, zh, content='sentences', content_rowid='id');
`

// Build writes outPath from an ECDICT CSV and a Tatoeba file (.txt or .zip).
func Build(ecdictCSV, sentencesPath, outPath string) (Stats, error) {
	var stats Stats
	if dir := filepath.Dir(outPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return stats, fmt.Errorf("create output dir: %w", err)
		}
	}
	_ = os.Remove(outPath)

	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(outPath)+"?_pragma=journal_mode(DELETE)&_pragma=busy_timeout(5000)")
	if err != nil {
		return stats, fmt.Errorf("create dictionary: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schemaSQL); err != nil {
		return stats, fmt.Errorf("create schema: %w", err)
	}

	entries, err := importEntries(db, ecdictCSV)
	if err != nil {
		return stats, err
	}
	stats.Entries = entries

	sentences, err := importSentences(db, sentencesPath)
	if err != nil {
		return stats, err
	}
	stats.Sentences = sentences

	if _, err := db.Exec(`INSERT INTO sentences_fts(rowid, en, zh) SELECT id, en, zh FROM sentences`); err != nil {
		return stats, fmt.Errorf("build sentence index: %w", err)
	}
	return stats, nil
}

func importEntries(db *sql.DB, path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open ecdict: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return 0, fmt.Errorf("read ecdict header: %w", err)
	}
	columns := make(map[string]int, len(header))
	for i, name := range header {
		columns[strings.TrimSpace(strings.ToLower(name))] = i
	}
	for _, need := range []string{"word"} {
		if _, ok := columns[need]; !ok {
			return 0, fmt.Errorf("ecdict header missing %q", need)
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin entries: %w", err)
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO entries
		(word, phonetic, translation, definition, pos, collins, oxford, tag, bnc, frq, exchange)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("prepare entries: %w", err)
	}
	defer stmt.Close()

	field := func(rec []string, name string) string {
		i, ok := columns[name]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}
	number := func(rec []string, name string) int {
		n, _ := strconv.Atoi(field(rec, name))
		return n
	}

	count := 0
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("read ecdict: %w", err)
		}
		word := field(rec, "word")
		if word == "" {
			continue
		}
		if _, err := stmt.Exec(word, field(rec, "phonetic"), field(rec, "translation"),
			field(rec, "definition"), field(rec, "pos"), number(rec, "collins"),
			number(rec, "oxford"), field(rec, "tag"), number(rec, "bnc"),
			number(rec, "frq"), field(rec, "exchange")); err != nil {
			return 0, fmt.Errorf("insert entry %q: %w", word, err)
		}
		count++
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit entries: %w", err)
	}
	return count, nil
}

var offensiveWords = map[string]bool{
	"fuck": true, "fucking": true, "shit": true, "bitch": true, "cunt": true,
	"dick": true, "cock": true, "pussy": true, "asshole": true, "bastard": true,
	"slut": true, "whore": true, "nigger": true, "nigga": true, "faggot": true,
}

func importSentences(db *sql.DB, path string) (int, error) {
	rc, err := openSentences(path)
	if err != nil {
		return 0, err
	}
	defer rc.Close()

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin sentences: %w", err)
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO sentences (en, zh, src) VALUES (?, ?, 'tatoeba')`)
	if err != nil {
		return 0, fmt.Errorf("prepare sentences: %w", err)
	}
	defer stmt.Close()

	seen := make(map[string]bool)
	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	count := 0
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		en := strings.TrimSpace(parts[0])
		zh := strings.TrimSpace(parts[1])
		if !keepSentence(en, zh) {
			continue
		}
		key := strings.ToLower(en) + "\x00" + zh
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, err := stmt.Exec(en, zh); err != nil {
			return 0, fmt.Errorf("insert sentence: %w", err)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("read sentences: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit sentences: %w", err)
	}
	return count, nil
}

func keepSentence(en, zh string) bool {
	if len(en) < 3 || len(en) > 120 || len(zh) < 2 || len(zh) > 80 {
		return false
	}
	if strings.ContainsAny(en, "\t\n\r") || strings.ContainsAny(zh, "\t\n\r") {
		return false
	}
	for _, token := range strings.FieldsFunc(strings.ToLower(en), func(r rune) bool {
		return !(r >= 'a' && r <= 'z')
	}) {
		if offensiveWords[token] {
			return false
		}
	}
	return true
}

// openSentences opens a plain .txt file or the first .txt inside a .zip.
func openSentences(path string) (io.ReadCloser, error) {
	if !strings.HasSuffix(strings.ToLower(path), ".zip") {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open sentences: %w", err)
		}
		return f, nil
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open sentences zip: %w", err)
	}
	for _, f := range zr.File {
		if strings.HasSuffix(strings.ToLower(f.Name), ".txt") {
			rc, err := f.Open()
			if err != nil {
				zr.Close()
				return nil, fmt.Errorf("open sentence file: %w", err)
			}
			return &zipEntryReader{ReadCloser: rc, zip: zr}, nil
		}
	}
	zr.Close()
	return nil, fmt.Errorf("no .txt found in %s", path)
}

type zipEntryReader struct {
	io.ReadCloser
	zip *zip.ReadCloser
}

func (z *zipEntryReader) Close() error {
	err := z.ReadCloser.Close()
	if zerr := z.zip.Close(); err == nil {
		err = zerr
	}
	return err
}
