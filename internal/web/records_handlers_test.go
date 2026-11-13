package web

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRecordsFilterByNotebook(t *testing.T) {
	tc := newAuthedTestClient(t)
	a := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	other, err := tc.store.CreateNotebook(context.Background(), tc.user.ID, "另一本")
	if err != nil {
		t.Fatal(err)
	}
	c := tc.seedWord(other.ID, "grape", "n. 葡萄")
	now := time.Now().UTC()
	if _, _, err := tc.store.AnswerWord(context.Background(), tc.notebook.ID, a.ID, true, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tc.store.AnswerWord(context.Background(), other.ID, c.ID, true, now); err != nil {
		t.Fatal(err)
	}
	body := tc.get(fmt.Sprintf("/records?notebook=%d", tc.notebook.ID)).Body.String()
	if !strings.Contains(body, "apple") || strings.Contains(body, "grape") {
		t.Fatalf("filter failed: %s", body)
	}
}

func TestRecordsPageShowsDayGroupsAndCounts(t *testing.T) {
	tc := newAuthedTestClient(t)
	a := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	b := tc.seedWord(tc.notebook.ID, "pear", "n. 梨")
	ctx := context.Background()
	now := time.Now().UTC()
	if _, _, err := tc.store.AnswerWord(ctx, tc.notebook.ID, a.ID, true, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tc.store.AnswerWord(ctx, tc.notebook.ID, b.ID, false, now); err != nil {
		t.Fatal(err)
	}
	body := tc.get("/records").Body.String()
	for _, want := range []string{"apple", "pear", "第 1 次", "认识", "不认识", "认识 1", "不认识 1"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestRecordsEmpty(t *testing.T) {
	tc := newAuthedTestClient(t)
	body := tc.get("/records").Body.String()
	if !strings.Contains(body, "还没有回忆记录") {
		t.Fatalf("body=%s", body)
	}
}

func TestRecordsCursorPagination(t *testing.T) {
	tc := newAuthedTestClient(t)
	w := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	ctx := context.Background()
	base := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if _, _, err := tc.store.AnswerWord(ctx, tc.notebook.ID, w.ID, true, base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	body := tc.get("/records").Body.String()
	if !strings.Contains(body, "第 3 次") {
		t.Fatalf("body=%s", body)
	}
}

func TestRecordsIgnoresForeignNotebookFilter(t *testing.T) {
	tc := newAuthedTestClient(t)
	w := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	if _, _, err := tc.store.AnswerWord(context.Background(), tc.notebook.ID, w.ID, true, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	body := tc.get("/records?notebook=99999").Body.String()
	if !strings.Contains(body, "apple") {
		t.Fatalf("invalid filter should fall back to all: %s", body)
	}
}
