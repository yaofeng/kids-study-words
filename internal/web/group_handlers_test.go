package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/yaofeng/kids-study-words/internal/store"
)

func TestNotebookPageShowsAndCreatesGroups(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	path := fmt.Sprintf("/notebooks/%d/groups", tc.notebook.ID)
	resp := tc.postForm(path, FormValues("name", "动物"))
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	body := tc.get(fmt.Sprintf("/notebooks/%d", tc.notebook.ID)).Body.String()
	for _, want := range []string{"分组", "动物", "新建分组"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
}

func TestCreateGroupDuplicateShowsError(t *testing.T) {
	tc := newAuthedTestClient(t)
	path := fmt.Sprintf("/notebooks/%d/groups", tc.notebook.ID)
	if resp := tc.postForm(path, FormValues("name", "动物")); resp.Code != http.StatusSeeOther {
		t.Fatalf("first code=%d", resp.Code)
	}
	resp := tc.postForm(path, FormValues("name", "动物"))
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "已经有同名的分组了") {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestNotebookPageShowsWordGroupBadges(t *testing.T) {
	tc := newAuthedTestClient(t)
	w := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	g, err := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	if err != nil {
		t.Fatal(err)
	}
	if err := tc.store.SetGroupWords(context.Background(), tc.user.ID, g.ID, []int64{w.ID}); err != nil {
		t.Fatal(err)
	}
	body := tc.get(fmt.Sprintf("/notebooks/%d", tc.notebook.ID)).Body.String()
	if !strings.Contains(body, "word-groups") || !strings.Contains(body, "动物") {
		t.Fatalf("badge missing: %s", body)
	}
}

func TestCreateGroupRejectsForeignNotebook(t *testing.T) {
	tc := newAuthedTestClient(t)
	resp := tc.postForm("/notebooks/99999/groups", FormValues("name", "动物"))
	if resp.Code != http.StatusNotFound {
		t.Fatalf("code=%d", resp.Code)
	}
}

func TestGroupPageShowsWordsAndSavesMembers(t *testing.T) {
	tc := newAuthedTestClient(t)
	a := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	b := tc.seedWord(tc.notebook.ID, "pear", "n. 梨")
	g, _ := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	path := fmt.Sprintf("/notebooks/%d/groups/%d", tc.notebook.ID, g.ID)

	body := tc.get(path).Body.String()
	if !strings.Contains(body, "apple") || !strings.Contains(body, "pear") || !strings.Contains(body, "保存") {
		t.Fatalf("page=%s", body)
	}
	// Same shadowing risk as the notebook page: the row heading must be the
	// word text, never a dumped store.Word struct.
	if !strings.Contains(body, `<span class="word-en">apple</span>`) {
		t.Fatalf("group word heading is not the word: %s", body)
	}
	resp := tc.postForm(path+"/words", url.Values{"word_id": {fmt.Sprint(a.ID), fmt.Sprint(b.ID)}})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	ids, _ := tc.store.GroupWordIDs(context.Background(), tc.user.ID, g.ID)
	if !slices.Equal(ids, []int64{a.ID, b.ID}) {
		t.Fatalf("ids=%v", ids)
	}
	body = tc.get(path).Body.String()
	if strings.Count(body, "checked") < 2 {
		t.Fatalf("checkboxes not checked: %s", body)
	}
}

func TestGroupPageRejectsForeignGroup(t *testing.T) {
	tc := newAuthedTestClient(t)
	other, _ := tc.store.CreateNotebook(context.Background(), tc.user.ID, "另一本")
	g, _ := tc.store.CreateWordGroup(context.Background(), other.ID, "动物")
	base := fmt.Sprintf("/notebooks/%d/groups/%d", tc.notebook.ID, g.ID)

	if resp := tc.get(base); resp.Code != http.StatusNotFound {
		t.Fatalf("GET %s code=%d", base, resp.Code)
	}
	for _, path := range []string{base + "/rename", base + "/delete", base + "/words"} {
		resp := tc.postForm(path, url.Values{})
		if resp.Code != http.StatusNotFound {
			t.Fatalf("POST %s code=%d", path, resp.Code)
		}
	}
}

func TestRenameGroupAndDuplicate(t *testing.T) {
	tc := newAuthedTestClient(t)
	g1, _ := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	if _, err := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "食物"); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/notebooks/%d/groups/%d/rename", tc.notebook.ID, g1.ID)
	resp := tc.postForm(path, FormValues("name", "食物"))
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "已经有同名的分组了") {
		t.Fatalf("duplicate code=%d body=%s", resp.Code, resp.Body.String())
	}
	resp = tc.postForm(path, FormValues("name", "宠物"))
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("rename code=%d", resp.Code)
	}
	got, _ := tc.store.WordGroupByID(context.Background(), tc.user.ID, g1.ID)
	if got.Name != "宠物" {
		t.Fatalf("name=%q", got.Name)
	}
}

func TestDeleteGroupKeepsWords(t *testing.T) {
	tc := newAuthedTestClient(t)
	w := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	g, _ := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	_ = tc.store.SetGroupWords(context.Background(), tc.user.ID, g.ID, []int64{w.ID})
	resp := tc.postForm(fmt.Sprintf("/notebooks/%d/groups/%d/delete", tc.notebook.ID, g.ID), url.Values{})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d", resp.Code)
	}
	if _, err := tc.store.WordByID(context.Background(), tc.notebook.ID, w.ID); err != nil {
		t.Fatalf("word should survive: %v", err)
	}
}

func TestSaveGroupWordsRejectsForeignWord(t *testing.T) {
	tc := newAuthedTestClient(t)
	other, _ := tc.store.CreateNotebook(context.Background(), tc.user.ID, "另一本")
	foreign, _ := tc.store.AddWord(context.Background(), other.ID, store.Word{Word: "dog"})
	g, _ := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	resp := tc.postForm(fmt.Sprintf("/notebooks/%d/groups/%d/words", tc.notebook.ID, g.ID),
		url.Values{"word_id": {fmt.Sprint(foreign.ID)}})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
}
