package web

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/yaofeng/kids-study-words/internal/store"
)

func TestRecallStartPageShowsCountsAndDefaultsToAll(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "apple", Proficiency: 3})
	tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "pear", Proficiency: 6})
	tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "cat", Proficiency: 9})
	g, _ := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	body := tc.get(fmt.Sprintf("/recall/%d/start", tc.notebook.ID)).Body.String()
	for _, want := range []string{
		"练哪些词", `value="all" checked`, "3 个",
		"低于 2 星（1 个）", "低于 3.5 星（2 个）", "动物（0 个）",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
	_ = g
}

func TestParseRecallScope(t *testing.T) {
	ok := []struct {
		v    url.Values
		want store.RecallScope
	}{
		{url.Values{}, store.RecallScope{Kind: store.ScopeAll}},
		{url.Values{"scope": {"all"}}, store.RecallScope{Kind: store.ScopeAll}},
		{url.Values{"scope": {"proficiency"}}, store.RecallScope{Kind: store.ScopeProficiency, MaxProficiency: 6}},
		{url.Values{"scope": {"proficiency"}, "max": {"8"}}, store.RecallScope{Kind: store.ScopeProficiency, MaxProficiency: 8}},
		{url.Values{"scope": {"groups"}, "group_id": {"2", "1", "2"}}, store.RecallScope{Kind: store.ScopeGroups, GroupIDs: []int64{1, 2}}},
	}
	for i, c := range ok {
		got, err := parseRecallScope(c.v)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("case %d got=%+v err=%v", i, got, err)
		}
	}
	bad := []url.Values{
		{"scope": {"proficiency"}, "max": {"1"}},
		{"scope": {"proficiency"}, "max": {"11"}},
		{"scope": {"groups"}},
		{"scope": {"weird"}},
	}
	for i, v := range bad {
		if _, err := parseRecallScope(v); !errors.Is(err, store.ErrInvalidScope) {
			t.Errorf("bad case %d: %v", i, err)
		}
	}
}

func TestScopeQueryAndLabel(t *testing.T) {
	if got := scopeQuery(store.RecallScope{Kind: store.ScopeAll}).Encode(); got != "scope=all" {
		t.Fatalf("all query=%s", got)
	}
	if got := scopeQuery(store.RecallScope{Kind: store.ScopeProficiency, MaxProficiency: 6}).Encode(); got != "max=6&scope=proficiency" {
		t.Fatalf("proficiency query=%s", got)
	}
	if got := scopeQuery(store.RecallScope{Kind: store.ScopeGroups, GroupIDs: []int64{2, 1}}).Encode(); got != "group_id=1&group_id=2&scope=groups" {
		t.Fatalf("groups query=%s", got)
	}
	groups := []store.WordGroup{{ID: 1, Name: "动物"}, {ID: 2, Name: "食物"}, {ID: 3, Name: "易错词"}, {ID: 4, Name: "Unit 4"}}
	if got := scopeLabel(store.RecallScope{Kind: store.ScopeAll}, groups); got != "全部" {
		t.Fatalf("label=%q", got)
	}
	if got := scopeLabel(store.RecallScope{Kind: store.ScopeProficiency, MaxProficiency: 6}, groups); got != "低于 3 星" {
		t.Fatalf("label=%q", got)
	}
	if got := scopeLabel(store.RecallScope{Kind: store.ScopeGroups, GroupIDs: []int64{1, 2}}, groups); got != "动物、食物" {
		t.Fatalf("label=%q", got)
	}
	if got := scopeLabel(store.RecallScope{Kind: store.ScopeGroups, GroupIDs: []int64{1, 2, 3, 4}}, groups); got != "动物、食物、易错词 等 4 组" {
		t.Fatalf("label=%q", got)
	}
	// Missing names must not leave a dangling separator or a panic when the
	// scope names more groups than the notebook still has.
	if got := scopeLabel(store.RecallScope{Kind: store.ScopeGroups, GroupIDs: []int64{1, 99, 100, 101}}, groups); got != "动物 等 4 组" {
		t.Fatalf("label=%q", got)
	}
}
