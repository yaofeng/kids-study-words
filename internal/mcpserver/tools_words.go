package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yaofeng/kids-study-words/internal/dict"
	"github.com/yaofeng/kids-study-words/internal/store"
)

// ListWordsInput is the input of list_words.
type ListWordsInput struct {
	NotebookID int64  `json:"notebook_id" jsonschema:"生词本 ID"`
	GroupID    int64  `json:"group_id,omitempty" jsonschema:"只列出该分组内的单词"`
	Sort       string `json:"sort,omitempty" jsonschema:"排序方式：added（默认）、proficiency 或 reviewed"`
	Limit      int    `json:"limit,omitempty" jsonschema:"每页数量，默认 200，最多 500"`
	Offset     int    `json:"offset,omitempty" jsonschema:"跳过多少个单词，默认 0"`
}

// ListWordsOutput is the structured result of list_words.
type ListWordsOutput struct {
	Words      []WordInfo `json:"words"`
	Total      int        `json:"total"`
	HasMore    bool       `json:"has_more"`
	NextOffset int        `json:"next_offset,omitempty"`
}

// AddWordInput is the input of add_word.
type AddWordInput struct {
	NotebookID int64  `json:"notebook_id" jsonschema:"生词本 ID"`
	Word       string `json:"word" jsonschema:"要加入的英文单词"`
}

// AddWordOutput is the structured result of add_word.
type AddWordOutput struct {
	Word WordInfo `json:"word"`
}

// RemoveWordInput is the input of remove_word.
type RemoveWordInput struct {
	NotebookID int64  `json:"notebook_id" jsonschema:"生词本 ID"`
	Word       string `json:"word" jsonschema:"要移除的英文单词"`
}

// RemoveWordOutput is the structured result of remove_word, including the
// groups the word belonged to before removal.
type RemoveWordOutput struct {
	Removed bool        `json:"removed"`
	Word    string      `json:"word"`
	Groups  []GroupInfo `json:"groups"`
}

// loadMCPNotebook resolves a notebook the account owns, mapping a missing or
// foreign notebook to the same short Chinese error.
func loadMCPNotebook(ctx context.Context, cfg Config, userID, notebookID int64) (store.Notebook, error) {
	nb, err := cfg.Store.NotebookByID(ctx, userID, notebookID)
	if errors.Is(err, store.ErrNotFound) {
		return store.Notebook{}, fmt.Errorf("生词本不存在")
	}
	if err != nil {
		return store.Notebook{}, err
	}
	return nb, nil
}

// registerWordTools registers the word tools for one account.
func registerWordTools(s *mcp.Server, cfg Config, userID int64) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_words",
		Description: "列出某个生词本里的单词，可按分组过滤、选择排序并分页。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ListWordsInput) (*mcp.CallToolResult, ListWordsOutput, error) {
		nb, err := loadMCPNotebook(ctx, cfg, userID, in.NotebookID)
		if err != nil {
			return nil, ListWordsOutput{}, err
		}

		sortKey := in.Sort
		if sortKey == "" {
			sortKey = "added"
		}
		switch sortKey {
		case "added", "proficiency", "reviewed":
		default:
			return nil, ListWordsOutput{}, fmt.Errorf("排序方式只能是 added、proficiency 或 reviewed")
		}

		words, err := cfg.Store.ListWords(ctx, nb.ID, sortKey)
		if err != nil {
			return nil, ListWordsOutput{}, err
		}

		if in.GroupID != 0 {
			g, err := cfg.Store.WordGroupByID(ctx, userID, in.GroupID)
			if errors.Is(err, store.ErrNotFound) {
				return nil, ListWordsOutput{}, fmt.Errorf("分组不存在")
			}
			if err != nil {
				return nil, ListWordsOutput{}, err
			}
			if g.NotebookID != nb.ID {
				return nil, ListWordsOutput{}, fmt.Errorf("分组不在这个生词本里")
			}
			ids, err := cfg.Store.GroupWordIDs(ctx, userID, g.ID)
			if err != nil {
				return nil, ListWordsOutput{}, err
			}
			keep := make(map[int64]bool, len(ids))
			for _, id := range ids {
				keep[id] = true
			}
			filtered := make([]store.Word, 0, len(words))
			for _, w := range words {
				if keep[w.ID] {
					filtered = append(filtered, w)
				}
			}
			words = filtered
		}

		memberships, _ := cfg.Store.GroupMemberships(ctx, nb.ID)

		limit := in.Limit
		if limit <= 0 {
			limit = 200
		}
		if limit > 500 {
			limit = 500
		}
		offset := in.Offset
		if offset < 0 {
			offset = 0
		}
		total := len(words)
		start := min(offset, total)
		end := min(start+limit, total)
		page := words[start:end]

		out := ListWordsOutput{
			Words:   make([]WordInfo, 0, len(page)),
			Total:   total,
			HasMore: end < total,
		}
		if out.HasMore {
			out.NextOffset = end
		}
		for _, w := range page {
			out.Words = append(out.Words, wordInfo(w, memberships[w.ID]))
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_word",
		Description: "查词典并把一个英文单词加入指定生词本，同时保存音标、释义和例句。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in AddWordInput) (*mcp.CallToolResult, AddWordOutput, error) {
		nb, err := loadMCPNotebook(ctx, cfg, userID, in.NotebookID)
		if err != nil {
			return nil, AddWordOutput{}, err
		}
		word := strings.TrimSpace(in.Word)
		if n := utf8.RuneCountInString(word); n < 1 || n > 100 {
			return nil, AddWordOutput{}, fmt.Errorf("单词需要 1 到 100 个字符")
		}
		if cfg.Dict == nil {
			return nil, AddWordOutput{}, fmt.Errorf("词典未加载。")
		}
		entry, err := cfg.Dict.Lookup(ctx, word)
		if errors.Is(err, dict.ErrNotFound) {
			return nil, AddWordOutput{}, fmt.Errorf("词典里没有「%s」", word)
		}
		if err != nil {
			return nil, AddWordOutput{}, err
		}

		examples := []store.Example{}
		if cached, ok, err := cfg.Store.CachedExamples(ctx, entry.Word); err == nil && ok && len(cached) > 0 {
			examples = cached
		} else if sentences, err := cfg.Dict.Examples(ctx, entry.Word, entry.Exchange, 2); err == nil {
			for _, sent := range sentences {
				examples = append(examples, store.Example{EN: sent.EN, ZH: sent.ZH, Source: sent.Source})
			}
		}

		w, err := cfg.Store.AddWord(ctx, nb.ID, store.Word{
			Word:        entry.Word,
			Phonetic:    entry.Phonetic,
			Translation: entry.Translation,
			Definition:  entry.Definition,
			Examples:    examples,
		})
		if errors.Is(err, store.ErrDuplicate) {
			return nil, AddWordOutput{}, fmt.Errorf("「%s」已在这个生词本里", entry.Word)
		}
		if err != nil {
			return nil, AddWordOutput{}, err
		}
		return nil, AddWordOutput{Word: wordInfo(w, nil)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "remove_word",
		Description: "从指定生词本移除一个单词，并返回它移除前所属的分组。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RemoveWordInput) (*mcp.CallToolResult, RemoveWordOutput, error) {
		nb, err := loadMCPNotebook(ctx, cfg, userID, in.NotebookID)
		if err != nil {
			return nil, RemoveWordOutput{}, err
		}
		word := strings.TrimSpace(in.Word)
		w, err := cfg.Store.WordByText(ctx, nb.ID, word)
		if errors.Is(err, store.ErrNotFound) {
			return nil, RemoveWordOutput{}, fmt.Errorf("生词本里没有「%s」", word)
		}
		if err != nil {
			return nil, RemoveWordOutput{}, err
		}

		memberships, _ := cfg.Store.GroupMemberships(ctx, nb.ID)
		groups := memberships[w.ID]
		out := RemoveWordOutput{
			Removed: true,
			Word:    w.Word,
			Groups:  make([]GroupInfo, 0, len(groups)),
		}
		for _, g := range groups {
			out.Groups = append(out.Groups, groupInfo(g))
		}

		if err := cfg.Store.DeleteWord(ctx, nb.ID, w.ID); err != nil {
			return nil, RemoveWordOutput{}, err
		}
		return nil, out, nil
	})
}
