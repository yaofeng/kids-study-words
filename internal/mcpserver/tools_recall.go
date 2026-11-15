package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yaofeng/kids-study-words/internal/store"
)

// QueryRecallLogsInput is the input of query_recall_logs.
type QueryRecallLogsInput struct {
	From           string `json:"from,omitempty" jsonschema:"起始时间，YYYY-MM-DD（按服务器时区）或 RFC3339"`
	To             string `json:"to,omitempty" jsonschema:"结束时间，YYYY-MM-DD 包含当天整天，或 RFC3339"`
	Word           string `json:"word,omitempty" jsonschema:"单词精确匹配，不区分大小写"`
	NotebookID     int64  `json:"notebook_id,omitempty"`
	GroupID        int64  `json:"group_id,omitempty"`
	Known          *bool  `json:"known,omitempty" jsonschema:"true 只看认识，false 只看不认识，省略表示都要"`
	ProficiencyMin int    `json:"proficiency_min,omitempty" jsonschema:"答后熟练度下限 1-10"`
	ProficiencyMax int    `json:"proficiency_max,omitempty" jsonschema:"答后熟练度上限 1-10"`
	ReviewCountMin int    `json:"review_count_min,omitempty" jsonschema:"复习次数下限，0 或不填表示不限制"`
	ReviewCountMax int    `json:"review_count_max,omitempty" jsonschema:"复习次数上限，0 或不填表示不限制"`
	Order          string `json:"order,omitempty" jsonschema:"desc（默认）或 asc"`
	Limit          int    `json:"limit,omitempty" jsonschema:"默认 200，最大 1000"`
	Offset         int    `json:"offset,omitempty"`
}

// RecallLogRow is the wire form of one raw recall log record.
type RecallLogRow struct {
	ID               int64     `json:"id"`
	ReviewedAt       time.Time `json:"reviewed_at"`
	Word             string    `json:"word"`
	WordID           *int64    `json:"word_id"`
	NotebookID       *int64    `json:"notebook_id"`
	NotebookName     string    `json:"notebook_name"`
	Known            bool      `json:"known"`
	ProficiencyAfter int       `json:"proficiency_after"`
	ReviewCount      int       `json:"review_count"`
}

// QueryRecallLogsOutput is the structured result of query_recall_logs.
type QueryRecallLogsOutput struct {
	Logs       []RecallLogRow `json:"logs"`
	Total      int            `json:"total"`
	Returned   int            `json:"returned"`
	HasMore    bool           `json:"has_more"`
	NextOffset int            `json:"next_offset,omitempty"`
}

// recallLogRow copies one store record into its wire form. Timestamps stay in
// UTC exactly as stored.
func recallLogRow(l store.RecallLog) RecallLogRow {
	return RecallLogRow{
		ID:               l.ID,
		ReviewedAt:       l.ReviewedAt,
		Word:             l.Word,
		WordID:           l.WordID,
		NotebookID:       l.NotebookID,
		NotebookName:     l.NotebookName,
		Known:            l.Known,
		ProficiencyAfter: l.ProficiencyAfter,
		ReviewCount:      l.ReviewCount,
	}
}

// parseTimeBound parses a query bound. An empty string yields nil. A
// YYYY-MM-DD value is interpreted in loc (endOfDay widens it to the whole
// day); otherwise RFC3339 is required. The result is always UTC.
func parseTimeBound(raw string, loc *time.Location, endOfDay bool) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if loc == nil {
		loc = time.Local
	}
	if t, err := time.ParseInLocation("2006-01-02", raw, loc); err == nil {
		if endOfDay {
			// AddDate advances by one local calendar day, so the bound stays at
			// the next local midnight across DST transitions.
			t = t.AddDate(0, 0, 1).Add(-time.Nanosecond)
		}
		u := t.UTC()
		return &u, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("时间格式应为 YYYY-MM-DD 或 RFC3339：%q", raw)
	}
	u := t.UTC()
	return &u, nil
}

// registerRecallTools registers the recall log tools for one account.
func registerRecallTools(s *mcp.Server, cfg Config, userID int64) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "query_recall_logs",
		Description: "查询原始回忆记录（每次答题一条），不做任何分析或汇总；需要统计时请取回记录后自行计算。" +
			"from/to 接受 YYYY-MM-DD（按服务器时区解释，to 包含当天整天）或 RFC3339；" +
			"known 省略表示认识与不认识都要；group_id 按单词当前所属分组成员过滤；" +
			"复习次数上下限填 0 或省略表示不限制；" +
			"reviewed_at 始终以 UTC 返回。默认按 reviewed_at 与 id 倒序，默认每页 200 条、最多 1000 条。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in QueryRecallLogsInput) (*mcp.CallToolResult, QueryRecallLogsOutput, error) {
		from, err := parseTimeBound(in.From, cfg.Location, false)
		if err != nil {
			return nil, QueryRecallLogsOutput{}, err
		}
		to, err := parseTimeBound(in.To, cfg.Location, true)
		if err != nil {
			return nil, QueryRecallLogsOutput{}, err
		}
		if from != nil && to != nil && from.After(*to) {
			return nil, QueryRecallLogsOutput{}, fmt.Errorf("起始时间不能晚于结束时间")
		}

		if in.NotebookID != 0 {
			if _, err := loadMCPNotebook(ctx, cfg, userID, in.NotebookID); err != nil {
				return nil, QueryRecallLogsOutput{}, err
			}
		}
		if in.GroupID != 0 {
			if _, err := loadMCPGroup(ctx, cfg, userID, in.GroupID); err != nil {
				return nil, QueryRecallLogsOutput{}, err
			}
		}

		filter := store.RecallLogFilter{
			From:           from,
			To:             to,
			Word:           in.Word,
			NotebookID:     optionalID(in.NotebookID),
			GroupID:        optionalID(in.GroupID),
			Known:          in.Known,
			Ascending:      false,
			Limit:          clampRecallLimit(in.Limit),
			Offset:         clampRecallOffset(in.Offset),
			ReviewCountMin: optionalCount(in.ReviewCountMin),
			ReviewCountMax: optionalCount(in.ReviewCountMax),
		}
		if err := applyRecallBounds(&filter, in); err != nil {
			return nil, QueryRecallLogsOutput{}, err
		}

		order := strings.ToLower(strings.TrimSpace(in.Order))
		switch order {
		case "", "desc":
		case "asc":
			filter.Ascending = true
		default:
			return nil, QueryRecallLogsOutput{}, fmt.Errorf("排序方式只能是 desc 或 asc")
		}

		logs, total, err := cfg.Store.QueryRecallLogs(ctx, userID, filter)
		if err != nil {
			slog.Error("query_recall_logs", "user_id", userID, "err", err)
			return nil, QueryRecallLogsOutput{}, fmt.Errorf("查询回忆记录失败，请稍后重试")
		}

		out := QueryRecallLogsOutput{
			Logs:     make([]RecallLogRow, 0, len(logs)),
			Total:    total,
			Returned: len(logs),
		}
		for _, l := range logs {
			out.Logs = append(out.Logs, recallLogRow(l))
		}
		out.HasMore = filter.Offset+len(logs) < total
		if out.HasMore {
			out.NextOffset = filter.Offset + len(logs)
		}
		return nil, out, nil
	})
}

// optionalID converts the zero "not set" convention to a filter pointer.
func optionalID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

// optionalCount converts the zero "not set" convention to a filter pointer.
func optionalCount(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

// clampRecallLimit applies the tool contract: default 200, maximum 1000.
func clampRecallLimit(limit int) int {
	if limit <= 0 {
		return 200
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

// clampRecallOffset treats a negative offset as 0.
func clampRecallOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

// applyRecallBounds validates the proficiency and review-count ranges and
// copies them into the filter.
func applyRecallBounds(f *store.RecallLogFilter, in QueryRecallLogsInput) error {
	if in.ProficiencyMin != 0 {
		if in.ProficiencyMin < 1 || in.ProficiencyMin > 10 {
			return fmt.Errorf("熟练度下限需要在 1 到 10 之间")
		}
		f.ProficiencyMin = &in.ProficiencyMin
	}
	if in.ProficiencyMax != 0 {
		if in.ProficiencyMax < 1 || in.ProficiencyMax > 10 {
			return fmt.Errorf("熟练度上限需要在 1 到 10 之间")
		}
		f.ProficiencyMax = &in.ProficiencyMax
	}
	if f.ProficiencyMin != nil && f.ProficiencyMax != nil && *f.ProficiencyMin > *f.ProficiencyMax {
		return fmt.Errorf("熟练度下限不能大于上限")
	}
	if in.ReviewCountMin < 0 || in.ReviewCountMax < 0 {
		return fmt.Errorf("复习次数不能为负数")
	}
	if in.ReviewCountMin != 0 && in.ReviewCountMax != 0 && in.ReviewCountMin > in.ReviewCountMax {
		return fmt.Errorf("复习次数下限不能大于上限")
	}
	return nil
}
