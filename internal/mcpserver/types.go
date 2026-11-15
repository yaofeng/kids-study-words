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

// GroupInfo is the wire form of a word group.
type GroupInfo struct {
	ID         int64     `json:"id"`
	NotebookID int64     `json:"notebook_id"`
	Name       string    `json:"name"`
	WordCount  int       `json:"word_count"`
	CreatedAt  time.Time `json:"created_at"`
}

func groupInfo(g store.WordGroup) GroupInfo {
	return GroupInfo{
		ID:         g.ID,
		NotebookID: g.NotebookID,
		Name:       g.Name,
		WordCount:  g.WordCount,
		CreatedAt:  g.CreatedAt,
	}
}

// ExampleInfo is the wire form of one bilingual example sentence.
type ExampleInfo struct {
	EN     string `json:"en"`
	ZH     string `json:"zh"`
	Source string `json:"source"`
}

func examplesInfo(in []store.Example) []ExampleInfo {
	out := make([]ExampleInfo, 0, len(in))
	for _, ex := range in {
		out = append(out, ExampleInfo{EN: ex.EN, ZH: ex.ZH, Source: ex.Source})
	}
	return out
}

// WordInfo is the wire form of a vocabulary item, including its groups.
type WordInfo struct {
	ID             int64         `json:"id"`
	NotebookID     int64         `json:"notebook_id"`
	Word           string        `json:"word"`
	Phonetic       string        `json:"phonetic,omitempty"`
	Translation    string        `json:"translation,omitempty"`
	Definition     string        `json:"definition,omitempty"`
	Examples       []ExampleInfo `json:"examples"`
	Proficiency    int           `json:"proficiency"`
	AddedAt        time.Time     `json:"added_at"`
	LastReviewedAt *time.Time    `json:"last_reviewed_at,omitempty"`
	ReviewCount    int           `json:"review_count"`
	Groups         []GroupInfo   `json:"groups"`
}

func wordInfo(w store.Word, groups []store.WordGroup) WordInfo {
	out := WordInfo{
		ID:             w.ID,
		NotebookID:     w.NotebookID,
		Word:           w.Word,
		Phonetic:       w.Phonetic,
		Translation:    w.Translation,
		Definition:     w.Definition,
		Examples:       examplesInfo(w.Examples),
		Proficiency:    w.Proficiency,
		AddedAt:        w.AddedAt,
		LastReviewedAt: w.LastReviewedAt,
		ReviewCount:    w.ReviewCount,
		Groups:         make([]GroupInfo, 0, len(groups)),
	}
	for _, g := range groups {
		out.Groups = append(out.Groups, groupInfo(g))
	}
	return out
}
