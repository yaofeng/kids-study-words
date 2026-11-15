package store

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestExportImportRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, err := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	w, err := st.AddWord(ctx, nb.ID, Word{Word: "apple", Translation: "n. 苹果", Proficiency: 6})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AnswerWord(ctx, nb.ID, w.ID, true, now); err != nil {
		t.Fatal(err)
	}
	f, err := st.ExportAll(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}

	dst := newTestStore(t)
	u2, _, err := dst.CreateFirstUser(ctx, "other", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dst.Import(ctx, u2.ID, f, ImportOverwrite); err != nil {
		t.Fatal(err)
	}
	def, err := dst.DefaultNotebook(ctx, u2.ID)
	if err != nil {
		t.Fatal(err)
	}
	words, err := dst.ListWords(ctx, def.ID, "added")
	if err != nil || len(words) != 1 || words[0].Word != "apple" || words[0].Proficiency != 7 {
		t.Fatalf("words=%+v err=%v", words, err)
	}
	logs, err := dst.ListRecallLogs(ctx, u2.ID, nil, 0, 10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("logs=%+v err=%v", logs, err)
	}
}

func TestMergeTakesHigherProficiencyAndEarlierAddedAt(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, err := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	w, err := st.AddWord(ctx, nb.ID, Word{Word: "apple", Translation: "本地释义", Proficiency: 6, LastReviewedAt: &newer})
	if err != nil {
		t.Fatal(err)
	}
	f := ExportFile{Format: "study-words", Version: 1, Notebooks: []ExportNotebook{{
		Name: "默认生词本",
		Words: []ExportWord{{
			Word: "apple", Translation: "", Proficiency: 8, AddedAt: older, LastReviewedAt: &older, ReviewCount: 9,
		}},
	}}}
	if _, err := st.Import(ctx, u.ID, f, ImportMerge); err != nil {
		t.Fatal(err)
	}
	got, err := st.WordByID(ctx, nb.ID, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Proficiency != 8 || got.Translation != "本地释义" || !got.AddedAt.Equal(older) || !got.LastReviewedAt.Equal(newer) {
		t.Fatalf("%+v", got)
	}
	if got.ReviewCount != 9 {
		t.Fatalf("review count should take max: %+v", got)
	}
}

func TestMergeFillsOnlyEmptyLocalFields(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, err := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddWord(ctx, nb.ID, Word{Word: "apple"}); err != nil {
		t.Fatal(err)
	}
	f := ExportFile{Format: "study-words", Version: 1, Notebooks: []ExportNotebook{{
		Name: "默认生词本",
		Words: []ExportWord{{
			Word: "apple", Phonetic: "/ˈæpl/", Translation: "n. 苹果", Definition: "a fruit",
			Examples: []Example{{EN: "An apple.", ZH: "一个苹果。"}}, Proficiency: 5,
		}},
	}}}
	if _, err := st.Import(ctx, u.ID, f, ImportMerge); err != nil {
		t.Fatal(err)
	}
	words, _ := st.ListWords(ctx, nb.ID, "added")
	if len(words) != 1 || words[0].Translation != "n. 苹果" || len(words[0].Examples) != 1 {
		t.Fatalf("words=%+v", words)
	}
}

func TestImportRejectsBadProficiencyAndRollsBack(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, err := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddWord(ctx, nb.ID, Word{Word: "keep"}); err != nil {
		t.Fatal(err)
	}
	f := ExportFile{Format: "study-words", Version: 1, Notebooks: []ExportNotebook{{
		Name: "x", Words: []ExportWord{{Word: "ok", Proficiency: 5}, {Word: "bad", Proficiency: 11}},
	}}}
	if _, err := st.Import(ctx, u.ID, f, ImportOverwrite); err == nil {
		t.Fatal("expected error")
	}
	words, err := st.ListWords(ctx, nb.ID, "added")
	if err != nil || len(words) != 1 || words[0].Word != "keep" {
		t.Fatalf("rollback failed: %+v err=%v", words, err)
	}
}

func TestImportNormalizesDefaultNotebook(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, _, err := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	f := ExportFile{Format: "study-words", Version: 1, Notebooks: []ExportNotebook{{Name: "A"}, {Name: "B"}}}
	if _, err := st.Import(ctx, u.ID, f, ImportOverwrite); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListNotebooks(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	defaults, defaultName := 0, ""
	for _, nb := range list {
		if nb.IsDefault {
			defaults++
			defaultName = nb.Name
		}
	}
	if len(list) != 2 || defaults != 1 || defaultName != "A" {
		t.Fatalf("list=%+v", list)
	}
}

func TestImportDedupesLogs(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, _, err := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	f := ExportFile{Format: "study-words", Version: 1,
		Notebooks:  []ExportNotebook{{Name: "默认生词本", Words: []ExportWord{{Word: "apple", Proficiency: 5}}}},
		RecallLogs: []ExportLog{{Notebook: "默认生词本", Word: "apple", Known: true, ProficiencyAfter: 6, ReviewCount: 1, ReviewedAt: ts}},
	}
	if _, err := st.Import(ctx, u.ID, f, ImportMerge); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Import(ctx, u.ID, f, ImportMerge); err != nil {
		t.Fatal(err)
	}
	logs, err := st.ListRecallLogs(ctx, u.ID, nil, 0, 10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("logs=%d err=%v", len(logs), err)
	}
}

func TestImportMergeCreatesMissingNotebook(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, _, err := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	f := ExportFile{Format: "study-words", Version: 1, Notebooks: []ExportNotebook{{
		Name: "动物", Words: []ExportWord{{Word: "cat", Proficiency: 5}},
	}}}
	stats, err := st.Import(ctx, u.ID, f, ImportMerge)
	if err != nil {
		t.Fatal(err)
	}
	if stats.NotebooksCreated != 1 || stats.WordsCreated != 1 {
		t.Fatalf("stats=%+v", stats)
	}
	nbs, _ := st.ListNotebooks(ctx, u.ID)
	if len(nbs) != 2 {
		t.Fatalf("notebooks=%+v", nbs)
	}
	if def, err := st.DefaultNotebook(ctx, u.ID); err != nil || def.Name != "默认生词本" {
		t.Fatalf("merge must keep current default: %+v %v", def, err)
	}
}

func TestImportOverwriteClearsExistingData(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, err := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	w, _ := st.AddWord(ctx, nb.ID, Word{Word: "old"})
	if _, _, err := st.AnswerWord(ctx, nb.ID, w.ID, true, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	f := ExportFile{Format: "study-words", Version: 1, Notebooks: []ExportNotebook{{
		Name: "全新", IsDefault: true, Words: []ExportWord{{Word: "new", Proficiency: 5}},
	}}}
	if _, err := st.Import(ctx, u.ID, f, ImportOverwrite); err != nil {
		t.Fatal(err)
	}
	nbs, _ := st.ListNotebooks(ctx, u.ID)
	if len(nbs) != 1 || nbs[0].Name != "全新" || !nbs[0].IsDefault {
		t.Fatalf("notebooks=%+v", nbs)
	}
	words, _ := st.ListWords(ctx, nbs[0].ID, "added")
	if len(words) != 1 || words[0].Word != "new" {
		t.Fatalf("words=%+v", words)
	}
	logs, _ := st.ListRecallLogs(ctx, u.ID, nil, 0, 10)
	if len(logs) != 0 {
		t.Fatalf("logs should be cleared: %+v", logs)
	}
}

func TestImportOverwriteCascadesExistingGroups(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	apple, _ := st.AddWord(ctx, nb.ID, Word{Word: "apple"})
	g, _ := st.CreateWordGroup(ctx, nb.ID, "旧组")
	_ = st.SetGroupWords(ctx, u.ID, g.ID, []int64{apple.ID})

	f := ExportFile{Format: "study-words", Version: 2, Notebooks: []ExportNotebook{{
		Name:  "全新",
		Words: []ExportWord{{Word: "new", Proficiency: 5}},
	}}}
	stats, err := st.Import(ctx, u.ID, f, ImportOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	if stats.GroupsCreated != 0 || stats.GroupMembershipsAdded != 0 {
		t.Fatalf("stats=%+v", stats)
	}
	var groups, members int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM word_groups`).Scan(&groups); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM word_group_members`).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if groups != 0 || members != 0 {
		t.Fatalf("old groups survived overwrite: groups=%d members=%d", groups, members)
	}
}

func TestImportV2GroupsRoundTrip(t *testing.T) {
	src := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := src.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	apple, _ := src.AddWord(ctx, nb.ID, Word{Word: "apple"})
	g, _ := src.CreateWordGroup(ctx, nb.ID, "动物")
	_ = src.SetGroupWords(ctx, u.ID, g.ID, []int64{apple.ID})
	f, err := src.ExportAll(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}

	dst := newTestStore(t)
	u2, _, _ := dst.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	stats, err := dst.Import(ctx, u2.ID, f, ImportOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	if stats.GroupsCreated != 1 || stats.GroupMembershipsAdded != 1 {
		t.Fatalf("stats=%+v", stats)
	}
	nbs, _ := dst.ListNotebooks(ctx, u2.ID)
	if len(nbs) != 1 {
		t.Fatalf("notebooks=%+v", nbs)
	}
	groups, _ := dst.ListWordGroups(ctx, nbs[0].ID)
	if len(groups) != 1 || groups[0].Name != "动物" || groups[0].WordCount != 1 {
		t.Fatalf("groups=%+v", groups)
	}
}

func TestImportMergeUnionsGroupMembers(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	apple, _ := st.AddWord(ctx, nb.ID, Word{Word: "apple"})
	pear, _ := st.AddWord(ctx, nb.ID, Word{Word: "pear"})
	g, _ := st.CreateWordGroup(ctx, nb.ID, "动物")
	_ = st.SetGroupWords(ctx, u.ID, g.ID, []int64{pear.ID})

	f := ExportFile{Format: "study-words", Version: 2, Notebooks: []ExportNotebook{{
		Name:   nb.Name,
		Words:  []ExportWord{{Word: "apple", Proficiency: 5}},
		Groups: []ExportGroup{{Name: "动物", Words: []string{"apple"}}},
	}}}
	stats, err := st.Import(ctx, u.ID, f, ImportMerge)
	if err != nil {
		t.Fatal(err)
	}
	if stats.GroupsCreated != 0 || stats.GroupMembershipsAdded != 1 {
		t.Fatalf("stats=%+v", stats)
	}
	ids, _ := st.GroupWordIDs(ctx, u.ID, g.ID)
	if !slices.Equal(ids, []int64{apple.ID, pear.ID}) {
		t.Fatalf("ids=%v", ids)
	}
}

func TestImportRejectsUnknownGroupWordAndRollsBack(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	f := ExportFile{Format: "study-words", Version: 2, Notebooks: []ExportNotebook{{
		Name:   "新本",
		Words:  []ExportWord{{Word: "apple", Proficiency: 5}},
		Groups: []ExportGroup{{Name: "动物", Words: []string{"ghost"}}},
	}}}
	if _, err := st.Import(ctx, u.ID, f, ImportMerge); err == nil {
		t.Fatal("import should reject unknown group word")
	}
	nbs, _ := st.ListNotebooks(ctx, u.ID)
	if len(nbs) != 1 || nbs[0].ID != nb.ID {
		t.Fatalf("partial import: %+v", nbs)
	}
}

func TestValidateExportRejectsBadGroups(t *testing.T) {
	base := ExportNotebook{
		Name:  "A",
		Words: []ExportWord{{Word: "apple", Proficiency: 5}},
	}
	cases := []ExportNotebook{
		{Name: "A", Words: base.Words, Groups: []ExportGroup{{Name: "", Words: []string{"apple"}}}},
		{Name: "A", Words: base.Words, Groups: []ExportGroup{{Name: "动物", Words: []string{"ghost"}}}},
		{Name: "A", Words: base.Words, Groups: []ExportGroup{{Name: "动物", Words: []string{"apple", "APPLE"}}}},
		{Name: "A", Words: base.Words, Groups: []ExportGroup{{Name: "动物"}, {Name: "动物"}}},
	}
	for i, nb := range cases {
		f := ExportFile{Format: "study-words", Version: 2, Notebooks: []ExportNotebook{nb}}
		if err := ValidateExport(f); err == nil {
			t.Errorf("case %d should be invalid", i)
		}
	}
}

func TestValidateExportAcceptsVersion2(t *testing.T) {
	f := ExportFile{Format: "study-words", Version: 2, Notebooks: []ExportNotebook{{
		Name:   "A",
		Words:  []ExportWord{{Word: "apple", Proficiency: 5}},
		Groups: []ExportGroup{{Name: "动物", Words: []string{"apple"}}},
	}}}
	if err := ValidateExport(f); err != nil {
		t.Fatalf("valid v2 file rejected: %v", err)
	}
}

func TestValidateExportRejectsMalformed(t *testing.T) {
	cases := []ExportFile{
		{Format: "other", Version: 1, Notebooks: []ExportNotebook{{Name: "a"}}},
		{Format: "study-words", Version: 3, Notebooks: []ExportNotebook{{Name: "a"}}},
		{Format: "study-words", Version: 1},
		{Format: "study-words", Version: 1, Notebooks: []ExportNotebook{{Name: ""}}},
		{Format: "study-words", Version: 1, Notebooks: []ExportNotebook{{Name: "a", Words: []ExportWord{{Word: ""}}}}},
		{Format: "study-words", Version: 1, Notebooks: []ExportNotebook{{Name: "a", Words: []ExportWord{{Word: "x", Proficiency: 0}}}}},
		{Format: "study-words", Version: 1, Notebooks: []ExportNotebook{{Name: "a", Words: []ExportWord{{Word: "x", Proficiency: 5, Examples: make([]Example, 6)}}}}},
		{Format: "study-words", Version: 1, Notebooks: []ExportNotebook{{Name: "a"}}, RecallLogs: []ExportLog{{Word: "x"}}},
		{Format: "study-words", Version: 1, Notebooks: []ExportNotebook{{Name: "a"}}, RecallLogs: []ExportLog{{Word: "x", ProficiencyAfter: 0, ReviewedAt: time.Now()}}},
		{Format: "study-words", Version: 1, Notebooks: []ExportNotebook{{Name: "a"}}, RecallLogs: []ExportLog{{Word: "x", ProficiencyAfter: 11, ReviewedAt: time.Now()}}},
		{Format: "study-words", Version: 1, Notebooks: []ExportNotebook{{Name: "a"}}, RecallLogs: []ExportLog{{Word: "x", ProficiencyAfter: 5, ReviewCount: -1, ReviewedAt: time.Now()}}},
		{Format: "study-words", Version: 1, Notebooks: []ExportNotebook{{Name: "a"}}, RecallLogs: []ExportLog{{Word: strings.Repeat("x", 101), ProficiencyAfter: 5, ReviewedAt: time.Now()}}},
	}
	for i, f := range cases {
		if err := ValidateExport(f); err == nil {
			t.Errorf("case %d should be invalid", i)
		}
	}
}
