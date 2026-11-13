package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// CachedExamples returns generated examples for a word, if any were stored.
func (s *Store) CachedExamples(ctx context.Context, word string) ([]Example, bool, error) {
	key := strings.ToLower(strings.TrimSpace(word))
	if key == "" {
		return nil, false, nil
	}
	var payload string
	err := s.db.QueryRowContext(ctx,
		`SELECT payload FROM ai_example_cache WHERE word_lower = ?`, key).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read example cache: %w", err)
	}
	var examples []Example
	if err := json.Unmarshal([]byte(payload), &examples); err != nil {
		return nil, false, fmt.Errorf("decode example cache: %w", err)
	}
	return examples, true, nil
}

// SaveExamples stores generated examples, replacing any previous entry.
func (s *Store) SaveExamples(ctx context.Context, word, model string, examples []Example) error {
	key := strings.ToLower(strings.TrimSpace(word))
	if key == "" {
		return errors.New("store: empty word for example cache")
	}
	if examples == nil {
		examples = []Example{}
	}
	payload, err := json.Marshal(examples)
	if err != nil {
		return fmt.Errorf("encode example cache: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO ai_example_cache (word_lower, payload, model, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(word_lower) DO UPDATE SET
			payload = excluded.payload,
			model = excluded.model,
			created_at = excluded.created_at`,
		key, string(payload), model, formatTime(TimeNow()))
	if err != nil {
		return fmt.Errorf("save example cache: %w", err)
	}
	return nil
}
