package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
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
