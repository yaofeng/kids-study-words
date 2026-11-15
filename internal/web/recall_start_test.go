package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/yaofeng/kids-study-words/internal/store"
)

func TestRecallStartAllScope(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	resp := tc.postForm(fmt.Sprintf("/recall/%d/start", tc.notebook.ID), url.Values{"scope": {"all"}})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	body := tc.get(resp.Header().Get("Location")).Body.String()
	if !strings.Contains(body, "本轮范围：全部") || !strings.Contains(body, "apple") {
		t.Fatalf("body=%s", body)
	}
}

func TestRecallStartProficiencyOnlyPicksQualifyingWord(t *testing.T) {
	tc := newAuthedTestClient(t)
	apple, _ := tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "apple", Proficiency: 8})
	low, _ := tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "pear", Proficiency: 3})
	resp := tc.postForm(fmt.Sprintf("/recall/%d/start", tc.notebook.ID),
		url.Values{"scope": {"proficiency"}, "max": {"6"}})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	body := tc.get(resp.Header().Get("Location")).Body.String()
	if !strings.Contains(body, low.Word) || strings.Contains(body, apple.Word) {
		t.Fatalf("scope leaked: %s", body)
	}
	if !strings.Contains(body, "本轮范围：低于 3 星") {
		t.Fatalf("label missing: %s", body)
	}
}

func TestRecallStartGroupsOnlyPicksGroupWord(t *testing.T) {
	tc := newAuthedTestClient(t)
	apple, _ := tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "apple"})
	pear, _ := tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "pear"})
	g, _ := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	_ = tc.store.SetGroupWords(context.Background(), tc.user.ID, g.ID, []int64{apple.ID})
	resp := tc.postForm(fmt.Sprintf("/recall/%d/start", tc.notebook.ID),
		url.Values{"scope": {"groups"}, "group_id": {fmt.Sprint(g.ID)}})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	body := tc.get(resp.Header().Get("Location")).Body.String()
	if !strings.Contains(body, apple.Word) || strings.Contains(body, pear.Word) {
		t.Fatalf("scope leaked: %s", body)
	}
	if !strings.Contains(body, "本轮范围：动物") {
		t.Fatalf("label missing: %s", body)
	}
}

func TestRecallStartRejectsForeignGroup(t *testing.T) {
	tc := newAuthedTestClient(t)
	other, _ := tc.store.CreateNotebook(context.Background(), tc.user.ID, "另一本")
	g, _ := tc.store.CreateWordGroup(context.Background(), other.ID, "动物")
	resp := tc.postForm(fmt.Sprintf("/recall/%d/start", tc.notebook.ID),
		url.Values{"scope": {"groups"}, "group_id": {fmt.Sprint(g.ID)}})
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "分组已经不存在") {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestRecallStartGroupsWithoutSelectionShowsInlineError(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	if _, err := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物"); err != nil {
		t.Fatal(err)
	}
	resp := tc.postForm(fmt.Sprintf("/recall/%d/start", tc.notebook.ID), url.Values{"scope": {"groups"}})
	body := resp.Body.String()
	if resp.Code != http.StatusOK || !strings.Contains(body, "请至少选择一个分组") {
		t.Fatalf("code=%d body=%s", resp.Code, body)
	}
	if !strings.Contains(body, "动物") {
		t.Fatalf("groups should be listed for retry: %s", body)
	}
}

func TestRecallStartEmptyScopeDoesNotStart(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "apple", "n. 苹果") // 熟练度 5
	resp := tc.postForm(fmt.Sprintf("/recall/%d/start", tc.notebook.ID),
		url.Values{"scope": {"proficiency"}, "max": {"2"}})
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "这个范围里还没有单词") {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestRecallGroupDeletedMidRoundShowsEmptyScope(t *testing.T) {
	tc := newAuthedTestClient(t)
	apple, _ := tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "apple"})
	g, _ := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	_ = tc.store.SetGroupWords(context.Background(), tc.user.ID, g.ID, []int64{apple.ID})
	resp := tc.postForm(fmt.Sprintf("/recall/%d/start", tc.notebook.ID),
		url.Values{"scope": {"groups"}, "group_id": {fmt.Sprint(g.ID)}})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("start code=%d", resp.Code)
	}
	if err := tc.store.DeleteWordGroup(context.Background(), tc.user.ID, g.ID); err != nil {
		t.Fatal(err)
	}
	next := tc.postForm(fmt.Sprintf("/recall/%d/next", tc.notebook.ID), url.Values{})
	if next.Code != http.StatusOK {
		t.Fatalf("next code=%d body=%s", next.Code, next.Body.String())
	}
	body := next.Body.String()
	if !strings.Contains(body, "这个范围里还没有单词") || !strings.Contains(body, "换个范围") {
		t.Fatalf("empty scope missing: %s", body)
	}
}
