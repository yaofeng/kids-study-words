package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yaofeng/kids-study-words/internal/store"
)

// ListNotebooksOutput is the structured result of list_notebooks.
type ListNotebooksOutput struct {
	Notebooks []NotebookInfo `json:"notebooks"`
}

// CreateNotebookInput is the input of create_notebook.
type CreateNotebookInput struct {
	Name string `json:"name" jsonschema:"生词本名称，1-40 个字"`
}

// RenameNotebookInput is the input of rename_notebook.
type RenameNotebookInput struct {
	NotebookID int64  `json:"notebook_id" jsonschema:"生词本 ID"`
	Name       string `json:"name" jsonschema:"新的生词本名称，1-40 个字"`
}

// NotebookOutput is the structured result of create_notebook and rename_notebook.
type NotebookOutput struct {
	Notebook NotebookInfo `json:"notebook"`
}

func validNotebookName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	n := utf8.RuneCountInString(name)
	if n < 1 || n > 40 {
		return "", fmt.Errorf("生词本名称需要 1 到 40 个字")
	}
	return name, nil
}

func registerNotebookTools(s *mcp.Server, cfg Config, userID int64) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_notebooks",
		Description: "列出当前账号的全部生词本，包含词数、默认标记和创建/更新时间。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListNotebooksOutput, error) {
		nbs, err := cfg.Store.ListNotebooks(ctx, userID)
		if err != nil {
			return nil, ListNotebooksOutput{}, err
		}
		out := ListNotebooksOutput{Notebooks: make([]NotebookInfo, 0, len(nbs))}
		for _, nb := range nbs {
			out.Notebooks = append(out.Notebooks, notebookInfo(nb))
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_notebook",
		Description: "创建一个新的生词本；名称在账号内不能重复。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in CreateNotebookInput) (*mcp.CallToolResult, NotebookOutput, error) {
		name, err := validNotebookName(in.Name)
		if err != nil {
			return nil, NotebookOutput{}, err
		}
		nb, err := cfg.Store.CreateNotebook(ctx, userID, name)
		if errors.Is(err, store.ErrDuplicate) {
			return nil, NotebookOutput{}, fmt.Errorf("已经有同名的生词本了")
		}
		if err != nil {
			return nil, NotebookOutput{}, err
		}
		return nil, NotebookOutput{Notebook: notebookInfo(nb)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "rename_notebook",
		Description: "重命名当前账号的某个生词本；名称在账号内不能重复。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RenameNotebookInput) (*mcp.CallToolResult, NotebookOutput, error) {
		name, err := validNotebookName(in.Name)
		if err != nil {
			return nil, NotebookOutput{}, err
		}
		nb, err := cfg.Store.RenameNotebook(ctx, userID, in.NotebookID, name)
		if errors.Is(err, store.ErrNotFound) {
			return nil, NotebookOutput{}, fmt.Errorf("生词本不存在")
		}
		if errors.Is(err, store.ErrDuplicate) {
			return nil, NotebookOutput{}, fmt.Errorf("已经有同名的生词本了")
		}
		if err != nil {
			return nil, NotebookOutput{}, err
		}
		return nil, NotebookOutput{Notebook: notebookInfo(nb)}, nil
	})
}
