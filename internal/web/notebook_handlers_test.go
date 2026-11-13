package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestCreateNotebookPage(t *testing.T) {
	tc := newAuthedTestClient(t)
	resp := tc.postForm("/notebooks", url.Values{"name": {"动物"}, "csrf_token": {tc.csrf}})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body)
	}
	body := tc.get("/notebooks").Body.String()
	if !strings.Contains(body, "动物") || !strings.Contains(body, "默认") {
		t.Fatalf("body missing notebook or default badge: %s", body)
	}
}

func TestCreateNotebookDuplicateShowsError(t *testing.T) {
	tc := newAuthedTestClient(t)
	if _, err := tc.store.CreateNotebook(context.Background(), tc.user.ID, "动物"); err != nil {
		t.Fatal(err)
	}
	resp := tc.postForm("/notebooks", url.Values{"name": {"动物"}})
	if resp.Code != http.StatusOK {
		t.Fatalf("code=%d", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), "同名") {
		t.Fatalf("expected duplicate message: %s", resp.Body)
	}
}

func TestCreateNotebookRejectsBlankName(t *testing.T) {
	tc := newAuthedTestClient(t)
	resp := tc.postForm("/notebooks", url.Values{"name": {"   "}})
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "名称") {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body)
	}
}

func TestSetDefaultAndDeleteNotebookPage(t *testing.T) {
	tc := newAuthedTestClient(t)
	nb, err := tc.store.CreateNotebook(context.Background(), tc.user.ID, "动物")
	if err != nil {
		t.Fatal(err)
	}
	resp := tc.postForm(fmt.Sprintf("/notebooks/%d/default", nb.ID), url.Values{})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("default code=%d body=%s", resp.Code, resp.Body)
	}
	if def, err := tc.store.DefaultNotebook(context.Background(), tc.user.ID); err != nil || def.ID != nb.ID {
		t.Fatalf("default=%+v err=%v", def, err)
	}

	resp = tc.postForm(fmt.Sprintf("/notebooks/%d/delete", nb.ID), url.Values{})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("delete code=%d body=%s", resp.Code, resp.Body)
	}
	if def, err := tc.store.DefaultNotebook(context.Background(), tc.user.ID); err != nil || def.ID != tc.notebook.ID {
		t.Fatalf("promoted=%+v err=%v", def, err)
	}
}

func TestDeleteLastNotebookShowsError(t *testing.T) {
	tc := newAuthedTestClient(t)
	resp := tc.postForm(fmt.Sprintf("/notebooks/%d/delete", tc.notebook.ID), url.Values{})
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "最后一个") {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body)
	}
}

func TestRenameNotebookPage(t *testing.T) {
	tc := newAuthedTestClient(t)
	nb, err := tc.store.CreateNotebook(context.Background(), tc.user.ID, "动物")
	if err != nil {
		t.Fatal(err)
	}
	resp := tc.postForm(fmt.Sprintf("/notebooks/%d/rename", nb.ID), url.Values{"name": {"小动物"}})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body)
	}
	got, err := tc.store.NotebookByID(context.Background(), tc.user.ID, nb.ID)
	if err != nil || got.Name != "小动物" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestNotebookRoutesRejectForeignNotebook(t *testing.T) {
	tc := newAuthedTestClient(t)
	// A second user cannot exist on this single-user instance, but a forged id
	// must still 404 rather than touching anything.
	resp := tc.postForm("/notebooks/99999/delete", url.Values{})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body)
	}
}
