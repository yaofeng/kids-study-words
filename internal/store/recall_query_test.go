package store

import (
	"context"
	"testing"
	"time"
)

func TestQueryRecallLogsFiltersAndPagination(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	other, _ := st.CreateNotebook(ctx, u.ID, "另一本")
	apple, _ := st.AddWord(ctx, nb.ID, Word{Word: "apple"})
	pear, _ := st.AddWord(ctx, other.ID, Word{Word: "pear"})
	g, _ := st.CreateWordGroup(ctx, nb.ID, "水果")
	_ = st.SetGroupWords(ctx, u.ID, g.ID, []int64{apple.ID})

	base := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	insert := func(wordID int64, notebookID int64, word string, known bool, prof, count int, at time.Time) {
		_, err := st.db.ExecContext(ctx, `INSERT INTO recall_logs
			(user_id, notebook_id, notebook_name, word_id, word, known, proficiency_after, review_count, reviewed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			u.ID, notebookID, "本", wordID, word, boolInt(known), prof, count, formatTime(at))
		if err != nil {
			t.Fatal(err)
		}
	}
	insert(apple.ID, nb.ID, "apple", true, 6, 1, base)
	insert(apple.ID, nb.ID, "apple", false, 4, 2, base.Add(time.Hour))
	insert(pear.ID, other.ID, "pear", true, 8, 3, base.Add(2*time.Hour))
	insert(apple.ID, nb.ID, "apple", true, 9, 4, base.Add(3*time.Hour))

	known := true
	f := RecallLogFilter{Known: &known, Limit: 10}
	logs, total, err := st.QueryRecallLogs(ctx, u.ID, f)
	if err != nil || total != 3 || len(logs) != 3 {
		t.Fatalf("known total=%d len=%d err=%v", total, len(logs), err)
	}
	f = RecallLogFilter{NotebookID: &nb.ID, Limit: 10}
	_, total, _ = st.QueryRecallLogs(ctx, u.ID, f)
	if total != 3 {
		t.Fatalf("notebook total=%d", total)
	}
	f = RecallLogFilter{GroupID: &g.ID, Limit: 10}
	_, total, _ = st.QueryRecallLogs(ctx, u.ID, f)
	if total != 3 {
		t.Fatalf("group total=%d", total)
	}
	minProf, maxProf := 5, 8
	f = RecallLogFilter{ProficiencyMin: &minProf, ProficiencyMax: &maxProf, Limit: 10}
	_, total, _ = st.QueryRecallLogs(ctx, u.ID, f)
	if total != 2 {
		t.Fatalf("proficiency total=%d", total)
	}
	f = RecallLogFilter{Word: "APPLE", Limit: 10}
	_, total, _ = st.QueryRecallLogs(ctx, u.ID, f)
	if total != 3 {
		t.Fatalf("word total=%d", total)
	}
	f = RecallLogFilter{Limit: 2, Offset: 0}
	page1, total, _ := st.QueryRecallLogs(ctx, u.ID, f)
	f.Offset = 2
	page2, _, _ := st.QueryRecallLogs(ctx, u.ID, f)
	if total != 4 || len(page1) != 2 || len(page2) != 2 || page1[0].ID == page2[0].ID {
		t.Fatalf("pagination total=%d p1=%+v p2=%+v", total, page1, page2)
	}
	f = RecallLogFilter{Ascending: true, Limit: 10}
	asc, _, _ := st.QueryRecallLogs(ctx, u.ID, f)
	if len(asc) != 4 || asc[0].ReviewedAt.After(asc[3].ReviewedAt) {
		t.Fatalf("asc=%+v", asc)
	}
}
