package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/yaofeng/kids-study-words/internal/store"
)

func TestQueryRecallLogsToolFilters(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	w, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "apple"})
	// 通过答题生成两条记录；熟练度从 5 开始，认识 +1、不认识 -1。
	_, _, _ = env.Store.AnswerWord(ctx, env.Notebook.ID, w.ID, true, time.Date(2026, 11, 15, 2, 0, 0, 0, time.UTC))
	_, _, _ = env.Store.AnswerWord(ctx, env.Notebook.ID, w.ID, false, time.Date(2026, 11, 16, 2, 0, 0, 0, time.UTC))

	out := decodeOut[QueryRecallLogsOutput](t, env.call(t, "query_recall_logs", map[string]any{
		"word": "APPLE", "order": "asc",
	}))
	if out.Total != 2 || len(out.Logs) != 2 || out.Logs[0].ProficiencyAfter != 6 || out.Logs[1].ProficiencyAfter != 5 {
		t.Fatalf("out=%+v", out)
	}
	known := decodeOut[QueryRecallLogsOutput](t, env.call(t, "query_recall_logs", map[string]any{
		"known": false,
	}))
	if known.Total != 1 || known.Logs[0].Known {
		t.Fatalf("known=%+v", known)
	}
}

func TestQueryRecallLogsDateOnlyToIncludesWholeDay(t *testing.T) {
	env := newTestEnvWithLocation(t, time.FixedZone("CST", 8*3600))
	ctx := context.Background()
	w, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "apple"})
	// 本地 11 月 15 日 23:30 与 11 月 16 日 00:30，存 UTC。
	_, _, _ = env.Store.AnswerWord(ctx, env.Notebook.ID, w.ID, true, time.Date(2026, 11, 15, 15, 30, 0, 0, time.UTC))
	_, _, _ = env.Store.AnswerWord(ctx, env.Notebook.ID, w.ID, false, time.Date(2026, 11, 15, 16, 30, 0, 0, time.UTC))

	out := decodeOut[QueryRecallLogsOutput](t, env.call(t, "query_recall_logs", map[string]any{
		"from": "2026-11-15", "to": "2026-11-15",
	}))
	if out.Total != 1 || !out.Logs[0].Known {
		t.Fatalf("date range=%+v", out)
	}
}

func TestQueryRecallLogsClampsLimitAndOffset(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	w, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "apple"})
	for i := 0; i < 3; i++ {
		_, _, _ = env.Store.AnswerWord(ctx, env.Notebook.ID, w.ID, true, time.Date(2026, 11, 15, 10+i, 0, 0, 0, time.UTC))
	}
	out := decodeOut[QueryRecallLogsOutput](t, env.call(t, "query_recall_logs", map[string]any{
		"limit": 5000, "offset": -1,
	}))
	if out.Total != 3 || out.Returned != 3 || out.HasMore {
		t.Fatalf("out=%+v", out)
	}
	res := env.call(t, "query_recall_logs", map[string]any{"from": "2026-11-16", "to": "2026-11-15"})
	if !res.IsError {
		t.Fatal("from > to should error")
	}
}

func TestQueryRecallLogsDeletedWordSnapshot(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	w, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "apple"})
	_, _, _ = env.Store.AnswerWord(ctx, env.Notebook.ID, w.ID, true, time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC))
	if err := env.Store.DeleteWord(ctx, env.Notebook.ID, w.ID); err != nil {
		t.Fatal(err)
	}
	out := decodeOut[QueryRecallLogsOutput](t, env.call(t, "query_recall_logs", map[string]any{"word": "apple"}))
	if out.Total != 1 || out.Logs[0].WordID != nil || out.Logs[0].NotebookID == nil {
		t.Fatalf("out=%+v", out)
	}
}
