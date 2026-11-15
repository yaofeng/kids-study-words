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

func TestQueryRecallLogsDateOnlyToCoversDSTDays(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("America/New_York unavailable: %v", err)
	}
	local := func(y int, m time.Month, d, hh, mm int) time.Time {
		return time.Date(y, m, d, hh, mm, 0, 0, loc)
	}
	cases := []struct {
		name      string
		date      string
		events    []time.Time
		wantKnown []bool
	}{
		{
			// 2026-03-08 春季调时：凌晨 2 点直接跳到 3 点。
			name: "spring forward",
			date: "2026-03-08",
			events: []time.Time{
				local(2026, 3, 8, 1, 30),  // 当天，调时前（EST）
				local(2026, 3, 8, 3, 30),  // 当天，调时后（EDT）
				local(2026, 3, 7, 23, 30), // 前一天
				local(2026, 3, 9, 0, 30),  // 后一天凌晨
			},
			wantKnown: []bool{true, false},
		},
		{
			// 2026-11-01 秋季调时：凌晨 2 点回拨到 1 点。
			name: "fall back",
			date: "2026-11-01",
			events: []time.Time{
				local(2026, 11, 1, 0, 30),  // 当天，调时前（EDT）
				local(2026, 11, 1, 23, 30), // 当天，调时后（EST）
				local(2026, 10, 31, 23, 30),
				local(2026, 11, 2, 0, 30),
			},
			wantKnown: []bool{true, false},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newTestEnvWithLocation(t, loc)
			ctx := context.Background()
			w, err := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "apple"})
			if err != nil {
				t.Fatal(err)
			}
			knowns := []bool{true, false, true, true}
			for i, at := range c.events {
				if _, _, err := env.Store.AnswerWord(ctx, env.Notebook.ID, w.ID, knowns[i], at); err != nil {
					t.Fatal(err)
				}
			}
			out := decodeOut[QueryRecallLogsOutput](t, env.call(t, "query_recall_logs", map[string]any{
				"from": c.date, "to": c.date, "order": "asc",
			}))
			if out.Total != 2 || len(out.Logs) != 2 {
				t.Fatalf("date range should cover exactly that local day: %+v", out)
			}
			for i, want := range c.wantKnown {
				if out.Logs[i].Known != want {
					t.Fatalf("log %d known=%v want=%v: %+v", i, out.Logs[i].Known, want, out)
				}
			}
		})
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
