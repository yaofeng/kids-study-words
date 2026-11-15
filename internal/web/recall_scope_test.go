package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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

func TestRecallEndPreservesScopeInSummary(t *testing.T) {
	tc := newAuthedTestClient(t)
	apple, _ := tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "apple"})
	g, _ := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	_ = tc.store.SetGroupWords(context.Background(), tc.user.ID, g.ID, []int64{apple.ID})
	start := tc.postForm(fmt.Sprintf("/recall/%d/start", tc.notebook.ID),
		url.Values{"scope": {"groups"}, "group_id": {fmt.Sprint(g.ID)}})
	if start.Code != http.StatusSeeOther {
		t.Fatalf("start code=%d", start.Code)
	}
	end := tc.postForm(fmt.Sprintf("/recall/%d/end", tc.notebook.ID), url.Values{})
	if end.Code != http.StatusSeeOther {
		t.Fatalf("end code=%d", end.Code)
	}
	loc := end.Header().Get("Location")
	if !strings.Contains(loc, "scope=groups") || !strings.Contains(loc, "group_id=") {
		t.Fatalf("location=%s", loc)
	}
	body := tc.get(loc).Body.String()
	if !strings.Contains(body, "本轮范围：动物") || !strings.Contains(body, "再来一轮") || !strings.Contains(body, "换个范围") {
		t.Fatalf("summary=%s", body)
	}
}

func TestRecallRestartUsesSameScope(t *testing.T) {
	tc := newAuthedTestClient(t)
	apple, _ := tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "apple"})
	pear, _ := tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "pear"})
	g, _ := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	_ = tc.store.SetGroupWords(context.Background(), tc.user.ID, g.ID, []int64{apple.ID})
	loc := fmt.Sprintf("/recall/%d/restart?scope=groups&group_id=%d", tc.notebook.ID, g.ID)
	resp := tc.get(loc)
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("restart code=%d body=%s", resp.Code, resp.Body.String())
	}
	body := tc.get(resp.Header().Get("Location")).Body.String()
	if !strings.Contains(body, "本轮范围：动物") || !strings.Contains(body, apple.Word) || strings.Contains(body, pear.Word) {
		t.Fatalf("restart scope lost: %s", body)
	}
}

func TestRecallRestartEmptyScopeReturnsToScopePage(t *testing.T) {
	tc := newAuthedTestClient(t)
	_, _ = tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "apple", Proficiency: 5})
	loc := fmt.Sprintf("/recall/%d/restart?scope=proficiency&max=2", tc.notebook.ID)
	resp := tc.get(loc)
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d", resp.Code)
	}
	if !strings.Contains(resp.Header().Get("Location"), "/start?") {
		t.Fatalf("location=%s", resp.Header().Get("Location"))
	}
	page := tc.get(resp.Header().Get("Location")).Body.String()
	if !strings.Contains(page, "这个范围里还没有单词") {
		t.Fatalf("flash missing: %s", page)
	}
}

func TestRecallRestartRejectsForeignGroup(t *testing.T) {
	tc := newAuthedTestClient(t)
	other, _ := tc.store.CreateNotebook(context.Background(), tc.user.ID, "另一本")
	g, _ := tc.store.CreateWordGroup(context.Background(), other.ID, "动物")
	resp := tc.get(fmt.Sprintf("/recall/%d/restart?scope=groups&group_id=%d", tc.notebook.ID, g.ID))
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d", resp.Code)
	}
	if !strings.Contains(resp.Header().Get("Location"), "/start?") {
		t.Fatalf("location=%s", resp.Header().Get("Location"))
	}
}
