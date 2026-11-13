package store

import (
	"context"
	"errors"
	"testing"
)

func TestNotebookDefaultInvariants(t *testing.T) {
	st := newTestStore(t)
	u, first, err := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.CreateNotebook(context.Background(), u.ID, "动物")
	if err != nil {
		t.Fatal(err)
	}
	if second.IsDefault {
		t.Fatal("second notebook should not be default")
	}
	if err := st.SetDefaultNotebook(context.Background(), u.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListNotebooks(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	defaults := 0
	for _, nb := range list {
		if nb.IsDefault {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("defaults=%d", defaults)
	}
	if err := st.DeleteNotebook(context.Background(), u.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	def, err := st.DefaultNotebook(context.Background(), u.ID)
	if err != nil || def.ID != first.ID {
		t.Fatalf("promoted=%+v err=%v", def, err)
	}
	if err := st.DeleteNotebook(context.Background(), u.ID, first.ID); !errors.Is(err, ErrLastNotebook) {
		t.Fatalf("expected ErrLastNotebook, got %v", err)
	}
}

func TestNotebookFirstIsDefault(t *testing.T) {
	st := newTestStore(t)
	u, first, err := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	if !first.IsDefault {
		t.Fatal("first notebook should be default")
	}
	if err := st.DeleteNotebook(context.Background(), u.ID, first.ID); !errors.Is(err, ErrLastNotebook) {
		t.Fatalf("expected ErrLastNotebook, got %v", err)
	}
}

func TestNotebookNameDuplicateCaseInsensitive(t *testing.T) {
	st := newTestStore(t)
	u, _, err := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateNotebook(context.Background(), u.ID, "animals"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateNotebook(context.Background(), u.ID, "Animals"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
	nb, _ := st.ListNotebooks(context.Background(), u.ID)
	var animals Notebook
	for _, n := range nb {
		if n.Name == "animals" {
			animals = n
		}
	}
	if _, err := st.RenameNotebook(context.Background(), u.ID, animals.ID, "ANIMALS"); err != nil {
		t.Fatalf("renaming to same name differing in case should succeed: %v", err)
	}
	if _, err := st.RenameNotebook(context.Background(), u.ID, animals.ID, "默认生词本"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestNotebookOwnershipAndCounts(t *testing.T) {
	st := newTestStore(t)
	u, first, err := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.NotebookByID(context.Background(), u.ID+1, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for foreign owner, got %v", err)
	}
	list, err := st.ListNotebooks(context.Background(), u.ID)
	if err != nil || len(list) != 1 || list[0].WordCount != 0 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	renamed, err := st.RenameNotebook(context.Background(), u.ID, first.ID, "新名字")
	if err != nil || renamed.Name != "新名字" {
		t.Fatalf("renamed=%+v err=%v", renamed, err)
	}
}
