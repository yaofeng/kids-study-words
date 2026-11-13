package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func seedNotebookWithWord(t *testing.T, proficiency int) (*Store, Notebook, Word) {
	t.Helper()
	st, nb := seedNotebook(t)
	w, err := st.AddWord(context.Background(), nb.ID, Word{Word: "apple", Translation: "n. 苹果", Proficiency: proficiency})
	if err != nil {
		t.Fatal(err)
	}
	return st, nb, w
}

func TestAnswerWordUpdatesAndLogs(t *testing.T) {
	st, nb, word := seedNotebookWithWord(t, 5)
	now := time.Now().UTC().Truncate(time.Second)
	w, log, err := st.AnswerWord(context.Background(), nb.ID, word.ID, false, now)
	if err != nil {
		t.Fatal(err)
	}
	if w.Proficiency != 4 || w.ReviewCount != 1 || w.LastReviewedAt == nil {
		t.Fatalf("word=%+v", w)
	}
	if log.Known || log.ProficiencyAfter != 4 || log.NotebookName != nb.Name || log.Word != "apple" {
		t.Fatalf("log=%+v", log)
	}
	if log.WordID == nil || *log.WordID != word.ID || log.NotebookID == nil || *log.NotebookID != nb.ID {
		t.Fatalf("log ids=%+v", log)
	}
	if !log.ReviewedAt.Equal(now) || log.ReviewCount != 1 || log.UserID != nb.UserID {
		t.Fatalf("log=%+v", log)
	}
}

func TestAnswerWordClampsProficiency(t *testing.T) {
	st, nb, word := seedNotebookWithWord(t, 10)
	ctx := context.Background()
	w, log, err := st.AnswerWord(ctx, nb.ID, word.ID, true, time.Now().UTC())
	if err != nil || w.Proficiency != 10 || log.ProficiencyAfter != 10 {
		t.Fatalf("w=%+v log=%+v err=%v", w, log, err)
	}
	low, err := st.AddWord(ctx, nb.ID, Word{Word: "low", Proficiency: 1})
	if err != nil {
		t.Fatal(err)
	}
	w, _, err = st.AnswerWord(ctx, nb.ID, low.ID, false, time.Now().UTC())
	if err != nil || w.Proficiency != 1 {
		t.Fatalf("w=%+v err=%v", w, err)
	}
}

func TestAnswerWordRejectsWrongNotebook(t *testing.T) {
	st, nb, word := seedNotebookWithWord(t, 5)
	other, err := st.CreateNotebook(context.Background(), nb.UserID, "另一本")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AnswerWord(context.Background(), other.ID, word.ID, true, time.Now().UTC()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
