package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func seedNotebook(t *testing.T) (*Store, Notebook) {
	t.Helper()
	st := newTestStore(t)
	_, nb, err := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	return st, nb
}

func TestAddWordDefaults(t *testing.T) {
	st, nb := seedNotebook(t)
	w, err := st.AddWord(context.Background(), nb.ID, Word{
		Word:        "apple",
		Translation: "n. 苹果",
		Examples:    []Example{{EN: "An apple.", ZH: "一个苹果。", Source: "manual"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.Proficiency != 5 || w.AddedAt.IsZero() || w.ReviewCount != 0 || w.LastReviewedAt != nil {
		t.Fatalf("bad: %+v", w)
	}
	if len(w.Examples) != 1 || w.Examples[0].EN != "An apple." {
		t.Fatalf("examples: %+v", w.Examples)
	}
	if _, err := st.AddWord(context.Background(), nb.ID, Word{Word: "Apple"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestUpdateWordKeepsProgress(t *testing.T) {
	st, nb := seedNotebook(t)
	w, err := st.AddWord(context.Background(), nb.ID, Word{Word: "apple", Translation: "old"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateWord(context.Background(), nb.ID, w.ID, "new", []Example{{EN: "E", ZH: "中"}}); err != nil {
		t.Fatal(err)
	}
	got, err := st.WordByID(context.Background(), nb.ID, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Translation != "new" || got.Proficiency != 5 || len(got.Examples) != 1 {
		t.Fatalf("got %+v", got)
	}
	if err := st.UpdateWord(context.Background(), nb.ID, w.ID+999, "x", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListWordsSorts(t *testing.T) {
	st, nb := seedNotebook(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	reviewed := base.Add(3 * time.Hour)

	a, _ := st.AddWord(ctx, nb.ID, Word{Word: "alpha", Proficiency: 9, AddedAt: base, LastReviewedAt: &reviewed, ReviewCount: 1})
	b, _ := st.AddWord(ctx, nb.ID, Word{Word: "beta", Proficiency: 2, AddedAt: base.Add(time.Hour)})
	c, _ := st.AddWord(ctx, nb.ID, Word{Word: "gamma", Proficiency: 5, AddedAt: base.Add(2 * time.Hour)})

	added, err := st.ListWords(ctx, nb.ID, "added")
	if err != nil || len(added) != 3 || added[0].ID != c.ID || added[2].ID != a.ID {
		t.Fatalf("added=%v err=%v", wordsIDs(added), err)
	}
	prof, err := st.ListWords(ctx, nb.ID, "proficiency")
	if err != nil || prof[0].ID != b.ID || prof[2].ID != a.ID {
		t.Fatalf("proficiency=%v err=%v", wordsIDs(prof), err)
	}
	rev, err := st.ListWords(ctx, nb.ID, "reviewed")
	if err != nil || len(rev) != 3 || rev[0].ID != a.ID {
		t.Fatalf("reviewed=%v err=%v", wordsIDs(rev), err)
	}
	if _, err := st.ListWords(ctx, nb.ID, "bogus"); err != nil {
		t.Fatalf("unknown sort should fall back to default: %v", err)
	}
	_ = c
}

func wordsIDs(ws []Word) []int64 {
	ids := make([]int64, len(ws))
	for i, w := range ws {
		ids[i] = w.ID
	}
	return ids
}

func TestDeleteWordAndTouchNotebook(t *testing.T) {
	st, nb := seedNotebook(t)
	ctx := context.Background()
	before, err := st.NotebookByID(ctx, nb.UserID, nb.ID)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	w, _ := st.AddWord(ctx, nb.ID, Word{Word: "apple", Translation: "n. 苹果"})
	afterAdd, _ := st.NotebookByID(ctx, nb.UserID, nb.ID)
	if !afterAdd.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("updated_at not bumped: %v -> %v", before.UpdatedAt, afterAdd.UpdatedAt)
	}
	if err := st.DeleteWord(ctx, nb.ID, w.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WordByID(ctx, nb.ID, w.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	list, err := st.ListWords(ctx, nb.ID, "added")
	if err != nil || len(list) != 0 {
		t.Fatalf("list=%v err=%v", wordsIDs(list), err)
	}
}
