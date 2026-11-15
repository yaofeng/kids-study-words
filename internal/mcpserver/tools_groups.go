package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yaofeng/kids-study-words/internal/store"
)

// maxGroupWords is the most word names one add/remove call accepts.
const maxGroupWords = 200

// ListGroupsInput is the input of list_groups.
type ListGroupsInput struct {
	NotebookID int64 `json:"notebook_id" jsonschema:"生词本 ID"`
}

// ListGroupsOutput is the structured result of list_groups.
type ListGroupsOutput struct {
	Groups []GroupInfo `json:"groups"`
}

// GetGroupInput is the input of get_group.
type GetGroupInput struct {
	GroupID int64 `json:"group_id" jsonschema:"分组 ID"`
	Limit   int   `json:"limit,omitempty" jsonschema:"每页数量，默认 200，最多 500"`
	Offset  int   `json:"offset,omitempty" jsonschema:"跳过多少个单词，默认 0"`
}

// GetGroupOutput is the structured result of get_group.
type GetGroupOutput struct {
	Group      GroupInfo  `json:"group"`
	Words      []WordInfo `json:"words"`
	Total      int        `json:"total"`
	HasMore    bool       `json:"has_more"`
	NextOffset int        `json:"next_offset,omitempty"`
}

// CreateGroupInput is the input of create_group.
type CreateGroupInput struct {
	NotebookID int64  `json:"notebook_id" jsonschema:"生词本 ID"`
	Name       string `json:"name" jsonschema:"分组名称，1-40 个字"`
}

// CreateGroupOutput is the structured result of create_group.
type CreateGroupOutput struct {
	Group GroupInfo `json:"group"`
}

// RenameGroupInput is the input of rename_group.
type RenameGroupInput struct {
	GroupID int64  `json:"group_id" jsonschema:"分组 ID"`
	Name    string `json:"name" jsonschema:"新的分组名称，1-40 个字"`
}

// RenameGroupOutput is the structured result of rename_group.
type RenameGroupOutput struct {
	Group GroupInfo `json:"group"`
}

// DeleteGroupInput is the input of delete_group.
type DeleteGroupInput struct {
	GroupID int64 `json:"group_id" jsonschema:"分组 ID"`
}

// DeleteGroupOutput is the structured result of delete_group.
type DeleteGroupOutput struct {
	Deleted bool   `json:"deleted"`
	Name    string `json:"name"`
}

// GroupWordsInput is the input of add_group_words and remove_group_words.
type GroupWordsInput struct {
	GroupID int64    `json:"group_id" jsonschema:"分组 ID"`
	Words   []string `json:"words" jsonschema:"英文单词列表，一次最多 200 个"`
}

// AddGroupWordsOutput is the structured result of add_group_words.
type AddGroupWordsOutput struct {
	Added          []string `json:"added"`
	AlreadyPresent []string `json:"already_present"`
	NotFound       []string `json:"not_found"`
}

// RemoveGroupWordsOutput is the structured result of remove_group_words.
type RemoveGroupWordsOutput struct {
	Removed    []string `json:"removed"`
	NotPresent []string `json:"not_present"`
	NotFound   []string `json:"not_found"`
}

// validGroupName trims a group name and enforces its 1–40 character limit.
func validGroupName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(name); n < 1 || n > 40 {
		return "", fmt.Errorf("分组名称需要 1 到 40 个字")
	}
	return name, nil
}

// loadMCPGroup resolves a group the account owns, mapping a missing or foreign
// group to the same short Chinese error.
func loadMCPGroup(ctx context.Context, cfg Config, userID, groupID int64) (store.WordGroup, error) {
	g, err := cfg.Store.WordGroupByID(ctx, userID, groupID)
	if errors.Is(err, store.ErrNotFound) {
		return store.WordGroup{}, fmt.Errorf("分组不存在")
	}
	if err != nil {
		slog.Error("load group", "user_id", userID, "group_id", groupID, "err", err)
		return store.WordGroup{}, fmt.Errorf("读取分组失败，请稍后重试")
	}
	return g, nil
}

// normalizeGroupWordNames trims, validates and case-insensitively dedupes the
// requested word names while preserving their first-seen order.
func normalizeGroupWordNames(names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if n := utf8.RuneCountInString(name); n < 1 || n > 100 {
			return nil, fmt.Errorf("单词需要 1 到 100 个字符")
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out, nil
}

// resolveGroupWords loads a group the account owns and synchronises the
// requested names with the notebook's words and the group's current members.
func resolveGroupWords(ctx context.Context, cfg Config, userID, groupID int64, names []string) (g store.WordGroup, current map[int64]bool, found map[string]store.Word, notFound []string, err error) {
	g, err = loadMCPGroup(ctx, cfg, userID, groupID)
	if err != nil {
		return g, nil, nil, nil, err
	}
	if len(names) > maxGroupWords {
		return store.WordGroup{}, nil, nil, nil, fmt.Errorf("一次最多处理 %d 个单词", maxGroupWords)
	}

	words, err := cfg.Store.ListWords(ctx, g.NotebookID, "added")
	if err != nil {
		slog.Error("group words: list words", "user_id", userID, "group_id", g.ID, "notebook_id", g.NotebookID, "err", err)
		return store.WordGroup{}, nil, nil, nil, fmt.Errorf("读取生词失败，请稍后重试")
	}
	lookup := make(map[string]store.Word, len(words))
	for _, w := range words {
		lookup[strings.ToLower(w.Word)] = w
	}

	found = make(map[string]store.Word, len(names))
	notFound = make([]string, 0)
	for _, name := range names {
		key := strings.ToLower(name)
		if w, ok := lookup[key]; ok {
			found[key] = w
		} else {
			notFound = append(notFound, name)
		}
	}

	ids, err := cfg.Store.GroupWordIDs(ctx, userID, g.ID)
	if errors.Is(err, store.ErrNotFound) {
		return store.WordGroup{}, nil, nil, nil, fmt.Errorf("分组不存在")
	}
	if err != nil {
		slog.Error("group words: group word ids", "user_id", userID, "group_id", g.ID, "err", err)
		return store.WordGroup{}, nil, nil, nil, fmt.Errorf("读取分组单词失败，请稍后重试")
	}
	current = make(map[int64]bool, len(ids))
	for _, id := range ids {
		current[id] = true
	}
	return g, current, found, notFound, nil
}

// registerGroupTools registers the word group tools for one account.
func registerGroupTools(s *mcp.Server, cfg Config, userID int64) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_groups",
		Description: "列出某个生词本里的全部分组。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ListGroupsInput) (*mcp.CallToolResult, ListGroupsOutput, error) {
		nb, err := loadMCPNotebook(ctx, cfg, userID, in.NotebookID)
		if err != nil {
			return nil, ListGroupsOutput{}, err
		}
		groups, err := cfg.Store.ListWordGroups(ctx, nb.ID)
		if err != nil {
			slog.Error("list_groups", "user_id", userID, "notebook_id", nb.ID, "err", err)
			return nil, ListGroupsOutput{}, fmt.Errorf("读取分组失败，请稍后重试")
		}
		out := ListGroupsOutput{Groups: make([]GroupInfo, 0, len(groups))}
		for _, g := range groups {
			out.Groups = append(out.Groups, groupInfo(g))
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_group",
		Description: "查看一个分组及其中的单词，支持分页。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in GetGroupInput) (*mcp.CallToolResult, GetGroupOutput, error) {
		g, err := loadMCPGroup(ctx, cfg, userID, in.GroupID)
		if err != nil {
			return nil, GetGroupOutput{}, err
		}

		ids, err := cfg.Store.GroupWordIDs(ctx, userID, g.ID)
		if errors.Is(err, store.ErrNotFound) {
			return nil, GetGroupOutput{}, fmt.Errorf("分组不存在")
		}
		if err != nil {
			slog.Error("get_group: group word ids", "user_id", userID, "group_id", g.ID, "err", err)
			return nil, GetGroupOutput{}, fmt.Errorf("读取分组单词失败，请稍后重试")
		}
		keep := make(map[int64]bool, len(ids))
		for _, id := range ids {
			keep[id] = true
		}

		words, err := cfg.Store.ListWords(ctx, g.NotebookID, "added")
		if err != nil {
			slog.Error("get_group: list words", "user_id", userID, "group_id", g.ID, "notebook_id", g.NotebookID, "err", err)
			return nil, GetGroupOutput{}, fmt.Errorf("读取生词失败，请稍后重试")
		}
		filtered := make([]store.Word, 0, len(keep))
		for _, w := range words {
			if keep[w.ID] {
				filtered = append(filtered, w)
			}
		}

		memberships, err := cfg.Store.GroupMemberships(ctx, g.NotebookID)
		if err != nil {
			slog.Warn("get_group: group memberships", "notebook_id", g.NotebookID, "err", err)
			memberships = nil
		}

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
		total := len(filtered)
		start := min(offset, total)
		end := min(start+limit, total)
		page := filtered[start:end]

		out := GetGroupOutput{
			Group:   groupInfo(g),
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
		Name:        "create_group",
		Description: "在某个生词本里新建一个分组；同一生词本内名称不能重复（不区分大小写）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in CreateGroupInput) (*mcp.CallToolResult, CreateGroupOutput, error) {
		nb, err := loadMCPNotebook(ctx, cfg, userID, in.NotebookID)
		if err != nil {
			return nil, CreateGroupOutput{}, err
		}
		name, err := validGroupName(in.Name)
		if err != nil {
			return nil, CreateGroupOutput{}, err
		}
		g, err := cfg.Store.CreateWordGroup(ctx, nb.ID, name)
		if errors.Is(err, store.ErrDuplicate) {
			return nil, CreateGroupOutput{}, fmt.Errorf("已经有同名的分组了")
		}
		if err != nil {
			slog.Error("create_group", "user_id", userID, "notebook_id", nb.ID, "err", err)
			return nil, CreateGroupOutput{}, fmt.Errorf("创建分组失败，请稍后重试")
		}
		return nil, CreateGroupOutput{Group: groupInfo(g)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "rename_group",
		Description: "重命名一个分组；同一生词本内名称不能重复（不区分大小写）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RenameGroupInput) (*mcp.CallToolResult, RenameGroupOutput, error) {
		name, err := validGroupName(in.Name)
		if err != nil {
			return nil, RenameGroupOutput{}, err
		}
		g, err := cfg.Store.RenameWordGroup(ctx, userID, in.GroupID, name)
		if errors.Is(err, store.ErrNotFound) {
			return nil, RenameGroupOutput{}, fmt.Errorf("分组不存在")
		}
		if errors.Is(err, store.ErrDuplicate) {
			return nil, RenameGroupOutput{}, fmt.Errorf("已经有同名的分组了")
		}
		if err != nil {
			slog.Error("rename_group", "user_id", userID, "group_id", in.GroupID, "err", err)
			return nil, RenameGroupOutput{}, fmt.Errorf("重命名分组失败，请稍后重试")
		}
		return nil, RenameGroupOutput{Group: groupInfo(g)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_group",
		Description: "删除一个分组；分组里的单词会保留在生词本里。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in DeleteGroupInput) (*mcp.CallToolResult, DeleteGroupOutput, error) {
		g, err := loadMCPGroup(ctx, cfg, userID, in.GroupID)
		if err != nil {
			return nil, DeleteGroupOutput{}, err
		}
		if err := cfg.Store.DeleteWordGroup(ctx, userID, g.ID); errors.Is(err, store.ErrNotFound) {
			return nil, DeleteGroupOutput{}, fmt.Errorf("分组不存在")
		} else if err != nil {
			slog.Error("delete_group", "user_id", userID, "group_id", g.ID, "err", err)
			return nil, DeleteGroupOutput{}, fmt.Errorf("删除分组失败，请稍后重试")
		}
		return nil, DeleteGroupOutput{Deleted: true, Name: g.Name}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_group_words",
		Description: "把生词本里的若干单词加入分组；一次最多 200 个。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in GroupWordsInput) (*mcp.CallToolResult, AddGroupWordsOutput, error) {
		if len(in.Words) > maxGroupWords {
			return nil, AddGroupWordsOutput{}, fmt.Errorf("一次最多处理 %d 个单词", maxGroupWords)
		}
		names, err := normalizeGroupWordNames(in.Words)
		if err != nil {
			return nil, AddGroupWordsOutput{}, err
		}
		g, current, found, notFound, err := resolveGroupWords(ctx, cfg, userID, in.GroupID, names)
		if err != nil {
			return nil, AddGroupWordsOutput{}, err
		}

		out := AddGroupWordsOutput{
			Added:          make([]string, 0, len(names)),
			AlreadyPresent: make([]string, 0),
			NotFound:       notFound,
		}
		newIDs := make([]int64, 0, len(current)+len(found))
		for id := range current {
			newIDs = append(newIDs, id)
		}
		for _, name := range names {
			w, ok := found[strings.ToLower(name)]
			if !ok {
				continue
			}
			if current[w.ID] {
				out.AlreadyPresent = append(out.AlreadyPresent, name)
				continue
			}
			out.Added = append(out.Added, name)
			newIDs = append(newIDs, w.ID)
		}

		if err := cfg.Store.SetGroupWords(ctx, userID, g.ID, newIDs); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, AddGroupWordsOutput{}, fmt.Errorf("分组不存在")
			}
			slog.Error("add_group_words: set group words", "user_id", userID, "group_id", g.ID, "err", err)
			return nil, AddGroupWordsOutput{}, fmt.Errorf("保存分组单词失败，请稍后重试")
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "remove_group_words",
		Description: "把若干单词移出分组；一次最多 200 个。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in GroupWordsInput) (*mcp.CallToolResult, RemoveGroupWordsOutput, error) {
		if len(in.Words) > maxGroupWords {
			return nil, RemoveGroupWordsOutput{}, fmt.Errorf("一次最多处理 %d 个单词", maxGroupWords)
		}
		names, err := normalizeGroupWordNames(in.Words)
		if err != nil {
			return nil, RemoveGroupWordsOutput{}, err
		}
		g, current, found, notFound, err := resolveGroupWords(ctx, cfg, userID, in.GroupID, names)
		if err != nil {
			return nil, RemoveGroupWordsOutput{}, err
		}

		out := RemoveGroupWordsOutput{
			Removed:    make([]string, 0, len(names)),
			NotPresent: make([]string, 0),
			NotFound:   notFound,
		}
		removed := make(map[int64]bool, len(names))
		for _, name := range names {
			w, ok := found[strings.ToLower(name)]
			if !ok {
				continue
			}
			if current[w.ID] {
				out.Removed = append(out.Removed, name)
				removed[w.ID] = true
			} else {
				out.NotPresent = append(out.NotPresent, name)
			}
		}

		newIDs := make([]int64, 0, len(current))
		for id := range current {
			if !removed[id] {
				newIDs = append(newIDs, id)
			}
		}
		if err := cfg.Store.SetGroupWords(ctx, userID, g.ID, newIDs); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, RemoveGroupWordsOutput{}, fmt.Errorf("分组不存在")
			}
			slog.Error("remove_group_words: set group words", "user_id", userID, "group_id", g.ID, "err", err)
			return nil, RemoveGroupWordsOutput{}, fmt.Errorf("保存分组单词失败，请稍后重试")
		}
		return nil, out, nil
	})
}
