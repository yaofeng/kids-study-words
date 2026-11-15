package store

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestExportAllIncludesWordsAndLogs(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, err := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	w, err := st.AddWord(ctx, nb.ID, Word{Word: "apple", Translation: "n. 苹果", Examples: []Example{{EN: "An apple.", ZH: "一个苹果。"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AnswerWord(ctx, nb.ID, w.ID, true, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	empty, err := st.CreateNotebook(ctx, u.ID, "空本")
	if err != nil {
		t.Fatal(err)
	}
	_ = empty

	f, err := st.ExportAll(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.Format != "study-words" || f.Version != 2 || f.ExportedAt.IsZero() {
		t.Fatalf("header=%+v", f)
	}
	if len(f.Notebooks) != 2 {
		t.Fatalf("notebooks=%+v", f.Notebooks)
	}
	if len(f.Notebooks[0].Words) != 1 || f.Notebooks[0].Words[0].Word != "apple" {
		t.Fatalf("words=%+v", f.Notebooks[0].Words)
	}
	if len(f.Notebooks[0].Words[0].Examples) != 1 || f.Notebooks[0].Words[0].Examples[0].EN != "An apple." {
		t.Fatalf("examples=%+v", f.Notebooks[0].Words[0].Examples)
	}
	if len(f.RecallLogs) != 1 || f.RecallLogs[0].Word != "apple" || !f.RecallLogs[0].Known {
		t.Fatalf("logs=%+v", f.RecallLogs)
	}
}

func TestExportIncludesGroups(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	apple, _ := st.AddWord(ctx, nb.ID, Word{Word: "apple"})
	pear, _ := st.AddWord(ctx, nb.ID, Word{Word: "pear"})
	g, _ := st.CreateWordGroup(ctx, nb.ID, "动物")
	_ = st.SetGroupWords(ctx, u.ID, g.ID, []int64{pear.ID, apple.ID})
	if _, err := st.CreateWordGroup(ctx, nb.ID, "空组"); err != nil {
		t.Fatal(err)
	}

	f, err := st.ExportAll(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != 2 {
		t.Fatalf("version=%d", f.Version)
	}
	if len(f.Notebooks[0].Groups) != 2 {
		t.Fatalf("groups=%+v", f.Notebooks[0].Groups)
	}
	byName := map[string]ExportGroup{}
	for _, g := range f.Notebooks[0].Groups {
		byName[g.Name] = g
	}
	if !slices.Equal(byName["动物"].Words, []string{"apple", "pear"}) {
		t.Fatalf("animal words=%v", byName["动物"].Words)
	}
	if byName["空组"].Words == nil || len(byName["空组"].Words) != 0 {
		t.Fatalf("empty group=%+v", byName["空组"])
	}
}

func TestExportNotebookFiltersLogs(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, err := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateNotebook(ctx, u.ID, "另一本")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := st.AddWord(ctx, nb.ID, Word{Word: "apple"})
	b, _ := st.AddWord(ctx, other.ID, Word{Word: "banana"})
	now := time.Now().UTC()
	if _, _, err := st.AnswerWord(ctx, nb.ID, a.ID, true, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AnswerWord(ctx, other.ID, b.ID, false, now); err != nil {
		t.Fatal(err)
	}

	f, err := st.ExportNotebook(ctx, u.ID, nb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Notebooks) != 1 || f.Notebooks[0].Name != nb.Name {
		t.Fatalf("notebooks=%+v", f.Notebooks)
	}
	if len(f.RecallLogs) != 1 || f.RecallLogs[0].Word != "apple" {
		t.Fatalf("logs=%+v", f.RecallLogs)
	}
	if _, err := st.ExportNotebook(ctx, u.ID+1, nb.ID); err != ErrNotFound {
		t.Fatalf("foreign export should fail: %v", err)
	}
}
