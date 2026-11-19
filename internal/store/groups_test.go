package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestListWordGroupsOrdersByLastViewed(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, err := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.CreateWordGroup(ctx, nb.ID, "苹果")
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.CreateWordGroup(ctx, nb.ID, "香蕉")
	if err != nil {
		t.Fatal(err)
	}
	// Never-viewed groups fall back to name order.
	groups, err := st.ListWordGroups(ctx, nb.ID)
	if err != nil || len(groups) != 2 || groups[0].ID != first.ID {
		t.Fatalf("groups=%+v err=%v", groups, err)
	}
	if err := st.TouchWordGroupViewed(ctx, u.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	groups, err = st.ListWordGroups(ctx, nb.ID)
	if err != nil || len(groups) != 2 {
		t.Fatalf("groups=%+v err=%v", groups, err)
	}
	if groups[0].ID != second.ID || groups[1].ID != first.ID {
		t.Fatalf("viewed group should come first: %+v", groups)
	}
	if groups[0].LastViewedAt == nil {
		t.Fatal("last viewed time not recorded")
	}
	if err := st.TouchWordGroupViewed(ctx, u.ID+1, second.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign touch: %v", err)
	}
}

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

func TestWordGroupNameLengthGuard(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	g, err := st.CreateWordGroup(ctx, nb.ID, "动物")
	if err != nil {
		t.Fatal(err)
	}
	blank := "   "
	tooLong := strings.Repeat("字", 41)
	if _, err := st.CreateWordGroup(ctx, nb.ID, blank); !errors.Is(err, ErrInvalidGroupName) {
		t.Fatalf("blank create: %v", err)
	}
	if _, err := st.CreateWordGroup(ctx, nb.ID, tooLong); !errors.Is(err, ErrInvalidGroupName) {
		t.Fatalf("long create: %v", err)
	}
	if _, err := st.RenameWordGroup(ctx, u.ID, g.ID, blank); !errors.Is(err, ErrInvalidGroupName) {
		t.Fatalf("blank rename: %v", err)
	}
	if _, err := st.RenameWordGroup(ctx, u.ID, g.ID, tooLong); !errors.Is(err, ErrInvalidGroupName) {
		t.Fatalf("long rename: %v", err)
	}
	if _, err := st.CreateWordGroup(ctx, nb.ID, strings.Repeat("字", 40)); err != nil {
		t.Fatalf("40 runes should be valid: %v", err)
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

func TestSetGroupWordsReplacesMembers(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	apple, _ := st.AddWord(ctx, nb.ID, Word{Word: "apple"})
	pear, _ := st.AddWord(ctx, nb.ID, Word{Word: "pear"})
	cat, _ := st.AddWord(ctx, nb.ID, Word{Word: "cat"})
	g, _ := st.CreateWordGroup(ctx, nb.ID, "动物")

	if err := st.SetGroupWords(ctx, u.ID, g.ID, []int64{pear.ID, apple.ID, apple.ID}); err != nil {
		t.Fatal(err)
	}
	ids, err := st.GroupWordIDs(ctx, u.ID, g.ID)
	if err != nil || !slices.Equal(ids, []int64{apple.ID, pear.ID}) {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	if err := st.SetGroupWords(ctx, u.ID, g.ID, []int64{cat.ID}); err != nil {
		t.Fatal(err)
	}
	ids, _ = st.GroupWordIDs(ctx, u.ID, g.ID)
	if !slices.Equal(ids, []int64{cat.ID}) {
		t.Fatalf("replace failed: %v", ids)
	}
	if err := st.SetGroupWords(ctx, u.ID, g.ID, nil); err != nil {
		t.Fatal(err)
	}
	ids, _ = st.GroupWordIDs(ctx, u.ID, g.ID)
	if len(ids) != 0 {
		t.Fatalf("clear failed: %v", ids)
	}
}

func TestSetGroupWordsRejectsForeignWord(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	other, _ := st.CreateNotebook(ctx, u.ID, "另一本")
	foreign, _ := st.AddWord(ctx, other.ID, Word{Word: "dog"})
	g, _ := st.CreateWordGroup(ctx, nb.ID, "动物")
	if err := st.SetGroupWords(ctx, u.ID, g.ID, []int64{foreign.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign word: %v", err)
	}
}

func TestGroupMembershipsMapsWordsToGroups(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	apple, _ := st.AddWord(ctx, nb.ID, Word{Word: "apple"})
	pear, _ := st.AddWord(ctx, nb.ID, Word{Word: "pear"})
	animals, _ := st.CreateWordGroup(ctx, nb.ID, "动物")
	food, _ := st.CreateWordGroup(ctx, nb.ID, "食物")
	_ = st.SetGroupWords(ctx, u.ID, animals.ID, []int64{apple.ID, pear.ID})
	_ = st.SetGroupWords(ctx, u.ID, food.ID, []int64{apple.ID})

	m, err := st.GroupMemberships(ctx, nb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(m[apple.ID]) != 2 || len(m[pear.ID]) != 1 {
		t.Fatalf("memberships=%+v", m)
	}
}

func TestGroupCascadesWithWordAndGroup(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	apple, _ := st.AddWord(ctx, nb.ID, Word{Word: "apple"})
	g, _ := st.CreateWordGroup(ctx, nb.ID, "动物")
	_ = st.SetGroupWords(ctx, u.ID, g.ID, []int64{apple.ID})

	if err := st.DeleteWord(ctx, nb.ID, apple.ID); err != nil {
		t.Fatal(err)
	}
	m, _ := st.GroupMemberships(ctx, nb.ID)
	if len(m) != 0 {
		t.Fatalf("word delete should cascade: %+v", m)
	}

	pear, _ := st.AddWord(ctx, nb.ID, Word{Word: "pear"})
	_ = st.SetGroupWords(ctx, u.ID, g.ID, []int64{pear.ID})
	if err := st.DeleteWordGroup(ctx, u.ID, g.ID); err != nil {
		t.Fatal(err)
	}
	ids, _ := st.GroupWordIDs(ctx, u.ID, g.ID)
	if len(ids) != 0 {
		t.Fatalf("group delete should cascade: %v", ids)
	}
}
