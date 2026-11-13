package web

import (
	"testing"
	"time"

	"github.com/yaofeng/kids-study-words/internal/store"
)

func TestGroupLogsByLocalDay(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-11-13 15:59 UTC = 23:59 Shanghai; 16:01 UTC = next day 00:01.
	logs := []store.RecallLog{
		{ID: 2, ReviewedAt: time.Date(2026, 11, 13, 16, 1, 0, 0, time.UTC), Known: false},
		{ID: 1, ReviewedAt: time.Date(2026, 11, 13, 15, 59, 0, 0, time.UTC), Known: true},
	}
	groups := GroupLogs(logs, loc)
	if len(groups) != 2 {
		t.Fatalf("groups=%d", len(groups))
	}
	if groups[0].Label != "11 月 14 日 · 周六" || groups[0].Unknown != 1 {
		t.Fatalf("%+v", groups[0])
	}
	if groups[1].Label != "11 月 13 日 · 周五" || groups[1].Known != 1 {
		t.Fatalf("%+v", groups[1])
	}
}

func TestGroupLogsSameDayAccumulates(t *testing.T) {
	loc := time.UTC
	day := time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)
	logs := []store.RecallLog{
		{ID: 3, ReviewedAt: day.Add(2 * time.Hour), Known: true},
		{ID: 2, ReviewedAt: day.Add(time.Hour), Known: true},
		{ID: 1, ReviewedAt: day, Known: false},
	}
	groups := GroupLogs(logs, loc)
	if len(groups) != 1 {
		t.Fatalf("groups=%d", len(groups))
	}
	g := groups[0]
	if g.Known != 2 || g.Unknown != 1 || len(g.Logs) != 3 || g.Date != "2026-11-13" {
		t.Fatalf("%+v", g)
	}
	if g.Logs[0].ID != 3 || g.Logs[2].ID != 1 {
		t.Fatalf("order not preserved: %+v", g.Logs)
	}
}
