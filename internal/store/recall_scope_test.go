package store

import (
	"context"
	"errors"
	"testing"
)

func TestListRecallWordsScopes(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	apple, _ := st.AddWord(ctx, nb.ID, Word{Word: "apple", Proficiency: 3})
	pear, _ := st.AddWord(ctx, nb.ID, Word{Word: "pear", Proficiency: 6})
	cat, _ := st.AddWord(ctx, nb.ID, Word{Word: "cat", Proficiency: 9})
	animals, _ := st.CreateWordGroup(ctx, nb.ID, "动物")
	food, _ := st.CreateWordGroup(ctx, nb.ID, "食物")
	_ = st.SetGroupWords(ctx, u.ID, animals.ID, []int64{apple.ID, pear.ID})
	_ = st.SetGroupWords(ctx, u.ID, food.ID, []int64{pear.ID, cat.ID})

	all, err := st.ListRecallWords(ctx, nb.ID, RecallScope{Kind: ScopeAll})
	if err != nil || len(all) != 3 {
		t.Fatalf("all=%d err=%v", len(all), err)
	}
	low, err := st.ListRecallWords(ctx, nb.ID, RecallScope{Kind: ScopeProficiency, MaxProficiency: 6})
	if err != nil || len(low) != 1 || low[0].Word != "apple" {
		t.Fatalf("low=%+v err=%v", low, err)
	}
	group, err := st.ListRecallWords(ctx, nb.ID, RecallScope{Kind: ScopeGroups, GroupIDs: []int64{animals.ID}})
	if err != nil || len(group) != 2 {
		t.Fatalf("group=%d err=%v", len(group), err)
	}
	union, err := st.ListRecallWords(ctx, nb.ID, RecallScope{Kind: ScopeGroups, GroupIDs: []int64{animals.ID, food.ID, animals.ID}})
	if err != nil || len(union) != 3 {
		t.Fatalf("union=%d err=%v", len(union), err)
	}
}

func TestListRecallWordsRejectsInvalidScope(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	_, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	cases := []RecallScope{
		{Kind: ScopeProficiency, MaxProficiency: 1},
		{Kind: ScopeProficiency, MaxProficiency: 11},
		{Kind: ScopeGroups},
		{Kind: RecallScopeKind(99)},
	}
	for i, sc := range cases {
		if _, err := st.ListRecallWords(ctx, nb.ID, sc); !errors.Is(err, ErrInvalidScope) {
			t.Errorf("case %d: %v", i, err)
		}
	}
}
