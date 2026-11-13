package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestNotebookDetailListsWords(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	tc.seedWord(tc.notebook.ID, "banana", "n. 香蕉")
	body := tc.get(fmt.Sprintf("/notebooks/%d", tc.notebook.ID)).Body.String()
	for _, want := range []string{"apple", "banana", "n. 苹果", "开始回忆"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestNotebookDetailSorts(t *testing.T) {
	tc := newAuthedTestClient(t)
	// Newest-added word first under the default sort.
	tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	tc.seedWord(tc.notebook.ID, "banana", "n. 香蕉")
	body := tc.get(fmt.Sprintf("/notebooks/%d", tc.notebook.ID)).Body.String()
	if strings.Index(body, "banana") > strings.Index(body, "apple") {
		t.Fatal("default sort should be newest first")
	}
}

func TestWordEditAndDelete(t *testing.T) {
	tc := newAuthedTestClient(t)
	w := tc.seedWord(tc.notebook.ID, "apple", "old")
	resp := tc.postForm(fmt.Sprintf("/notebooks/%d/words/%d/edit", tc.notebook.ID, w.ID), url.Values{
		"translation":  {"new"},
		"csrf_token":   {tc.csrf},
		"example_en_1": {"An apple."},
		"example_zh_1": {"一个苹果。"},
		"example_en_2": {""},
		"example_zh_2": {""},
	})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("edit code=%d body=%s", resp.Code, resp.Body)
	}
	if body := tc.get(fmt.Sprintf("/notebooks/%d", tc.notebook.ID)).Body.String(); !strings.Contains(body, "new") {
		t.Fatal("new translation missing")
	}
	resp = tc.postForm(fmt.Sprintf("/notebooks/%d/words/%d/delete", tc.notebook.ID, w.ID), url.Values{})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("delete code=%d", resp.Code)
	}
	if body := tc.get(fmt.Sprintf("/notebooks/%d", tc.notebook.ID)).Body.String(); strings.Contains(body, "An apple.") {
		t.Fatal("word still listed")
	}
}

func TestWordEditRejectsHalfFilledExample(t *testing.T) {
	tc := newAuthedTestClient(t)
	w := tc.seedWord(tc.notebook.ID, "apple", "old")
	resp := tc.postForm(fmt.Sprintf("/notebooks/%d/words/%d/edit", tc.notebook.ID, w.ID), url.Values{
		"translation":  {"new"},
		"example_en_1": {"An apple."},
		"example_zh_1": {""},
	})
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "英文和中文") {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body)
	}
}

func TestWordRoutesRejectCrossNotebookWord(t *testing.T) {
	tc := newAuthedTestClient(t)
	other, err := tc.store.CreateNotebook(context.Background(), tc.user.ID, "另一本")
	if err != nil {
		t.Fatal(err)
	}
	w := tc.seedWord(other.ID, "pear", "n. 梨")
	resp := tc.get(fmt.Sprintf("/notebooks/%d/words/%d/edit", tc.notebook.ID, w.ID))
	if resp.Code != http.StatusNotFound {
		t.Fatalf("code=%d", resp.Code)
	}
	resp = tc.postForm(fmt.Sprintf("/notebooks/%d/words/%d/delete", tc.notebook.ID, w.ID), url.Values{})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("delete code=%d", resp.Code)
	}
}

func TestWordEditShowsExistingExamples(t *testing.T) {
	tc := newAuthedTestClient(t)
	w := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	resp := tc.postForm(fmt.Sprintf("/notebooks/%d/words/%d/edit", tc.notebook.ID, w.ID), url.Values{
		"translation":  {"n. 苹果"},
		"example_en_1": {"An apple a day."},
		"example_zh_1": {"一天一个苹果。"},
	})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d", resp.Code)
	}
	body := tc.get(fmt.Sprintf("/notebooks/%d/words/%d/edit", tc.notebook.ID, w.ID)).Body.String()
	if !strings.Contains(body, "An apple a day.") {
		t.Fatalf("example missing from edit page: %s", body)
	}
}
