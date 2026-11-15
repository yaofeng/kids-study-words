package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ListNotebooksOutput is the structured result of list_notebooks.
type ListNotebooksOutput struct {
	Notebooks []NotebookInfo `json:"notebooks"`
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
}
