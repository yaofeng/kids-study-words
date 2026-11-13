package store

import (
	"context"
	"testing"
	"time"
)

func TestListRecallLogsNewestFirstAndCursor(t *testing.T) {
	st, nb, word := seedNotebookWithWord(t, 5)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 5; i++ {
		if _, _, err := st.AnswerWord(ctx, nb.ID, word.ID, i%2 == 0, base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	page1, err := st.ListRecallLogs(ctx, nb.UserID, nil, 0, 2)
	if err != nil || len(page1) != 2 {
		t.Fatalf("page1=%+v err=%v", page1, err)
	}
	if page1[0].ID <= page1[1].ID {
		t.Fatalf("not newest first: %+v", page1)
	}
	page2, err := st.ListRecallLogs(ctx, nb.UserID, nil, page1[len(page1)-1].ID, 2)
	if err != nil || len(page2) != 2 {
		t.Fatalf("page2=%+v err=%v", page2, err)
	}
	for _, a := range page1 {
		for _, b := range page2 {
			if a.ID == b.ID {
				t.Fatalf("duplicate log across pages: %d", a.ID)
			}
		}
	}
	if page2[0].ID >= page1[len(page1)-1].ID {
		t.Fatalf("cursor should move older: %+v then %+v", page1, page2)
	}

	all, err := st.ListRecallLogs(ctx, nb.UserID, nil, 0, 500)
	if err != nil || len(all) != 5 {
		t.Fatalf("all=%+v err=%v", all, err)
	}
	knownCount := 0
	for _, l := range all {
		if l.Known {
			knownCount++
		}
	}
	if knownCount != 3 {
		t.Fatalf("known=%d want 3", knownCount)
	}
}

func TestListRecallLogsFilterByNotebook(t *testing.T) {
	st, nb, word := seedNotebookWithWord(t, 5)
	ctx := context.Background()
	other, err := st.CreateNotebook(ctx, nb.UserID, "另一本")
	if err != nil {
		t.Fatal(err)
	}
	otherWord, err := st.AddWord(ctx, other.ID, Word{Word: "pear"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, _, err := st.AnswerWord(ctx, nb.ID, word.ID, true, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AnswerWord(ctx, other.ID, otherWord.ID, false, now); err != nil {
		t.Fatal(err)
	}

	logs, err := st.ListRecallLogs(ctx, nb.UserID, &nb.ID, 0, 200)
	if err != nil || len(logs) != 1 || logs[0].Word != "apple" {
		t.Fatalf("logs=%+v err=%v", logs, err)
	}
	all, err := st.ListRecallLogs(ctx, nb.UserID, nil, 0, 200)
	if err != nil || len(all) != 2 {
		t.Fatalf("all=%+v err=%v", all, err)
	}
}

func TestListRecallLogsKeepsSnapshotsAfterDelete(t *testing.T) {
	st, nb, word := seedNotebookWithWord(t, 5)
	ctx := context.Background()
	if _, _, err := st.AnswerWord(ctx, nb.ID, word.ID, true, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteWord(ctx, nb.ID, word.ID); err != nil {
		t.Fatal(err)
	}
	logs, err := st.ListRecallLogs(ctx, nb.UserID, nil, 0, 200)
	if err != nil || len(logs) != 1 {
		t.Fatalf("logs=%+v err=%v", logs, err)
	}
	if logs[0].Word != "apple" || logs[0].NotebookName != nb.Name || logs[0].WordID != nil {
		t.Fatalf("snapshot lost: %+v", logs[0])
	}
}
