package store

import (
	"context"
	"errors"
	"testing"
)

func TestWordGroupCRUD(t *testing.T) {
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

	g, err := st.CreateWordGroup(ctx, nb.ID, " 动物 ")
	if err != nil {
		t.Fatal(err)
	}
	if g.ID == 0 || g.NotebookID != nb.ID || g.Name != "动物" || g.CreatedAt.IsZero() || g.WordCount != 0 {
		t.Fatalf("group=%+v", g)
	}
	if _, err := st.CreateWordGroup(ctx, nb.ID, "动物"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := st.CreateWordGroup(ctx, other.ID, "动物"); err != nil {
		t.Fatalf("same name in another notebook should be allowed: %v", err)
	}

	groups, err := st.ListWordGroups(ctx, nb.ID)
	if err != nil || len(groups) != 1 || groups[0].Name != "动物" {
		t.Fatalf("groups=%+v err=%v", groups, err)
	}
	got, err := st.WordGroupByID(ctx, u.ID, g.ID)
	if err != nil || got.ID != g.ID || got.NotebookID != nb.ID {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if _, err := st.WordGroupByID(ctx, u.ID+1, g.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign lookup: %v", err)
	}

	renamed, err := st.RenameWordGroup(ctx, u.ID, g.ID, "食物")
	if err != nil || renamed.Name != "食物" {
		t.Fatalf("renamed=%+v err=%v", renamed, err)
	}
	if _, err := st.RenameWordGroup(ctx, u.ID+1, g.ID, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign rename: %v", err)
	}
	if err := st.DeleteWordGroup(ctx, u.ID+1, g.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign delete: %v", err)
	}
	if err := st.DeleteWordGroup(ctx, u.ID, g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WordGroupByID(ctx, u.ID, g.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestWordGroupCascadesWithNotebook(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, _, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	nb, _ := st.CreateNotebook(ctx, u.ID, "临时本")
	g, _ := st.CreateWordGroup(ctx, nb.ID, "动物")
	if err := st.DeleteNotebook(ctx, u.ID, nb.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WordGroupByID(ctx, u.ID, g.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("group should cascade: %v", err)
	}
}
