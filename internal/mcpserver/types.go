package mcpserver

import (
	"time"

	"github.com/yaofeng/kids-study-words/internal/store"
)

// NotebookInfo is the wire form of a notebook.
type NotebookInfo struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	IsDefault bool      `json:"is_default"`
	WordCount int       `json:"word_count"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func notebookInfo(nb store.Notebook) NotebookInfo {
	return NotebookInfo{
		ID:        nb.ID,
		Name:      nb.Name,
		IsDefault: nb.IsDefault,
		WordCount: nb.WordCount,
		CreatedAt: nb.CreatedAt,
		UpdatedAt: nb.UpdatedAt,
	}
}
