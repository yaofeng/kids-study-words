package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
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

	resp = tc.postForm(fmt.Sprintf("/notebooks/%d/delete", nb.ID), url.Values{"password": {"secret1"}})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("delete code=%d body=%s", resp.Code, resp.Body)
	}
	if def, err := tc.store.DefaultNotebook(context.Background(), tc.user.ID); err != nil || def.ID != tc.notebook.ID {
		t.Fatalf("promoted=%+v err=%v", def, err)
	}
}

func TestDeleteLastNotebookShowsError(t *testing.T) {
	tc := newAuthedTestClient(t)
	resp := tc.postForm(fmt.Sprintf("/notebooks/%d/delete", tc.notebook.ID), url.Values{"password": {"secret1"}})
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

func TestDeleteNotebookRequiresCurrentPassword(t *testing.T) {
	tc := newAuthedTestClient(t)
	nb, err := tc.store.CreateNotebook(context.Background(), tc.user.ID, "动物")
	if err != nil {
		t.Fatal(err)
	}
	for _, form := range []url.Values{
		{"csrf_token": {tc.csrf}},
		{"password": {"wrong-pass"}, "csrf_token": {tc.csrf}},
	} {
		resp := tc.postForm(fmt.Sprintf("/notebooks/%d/delete", nb.ID), form)
		if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "密码") {
			t.Fatalf("password check missing: code=%d body=%s", resp.Code, resp.Body)
		}
		if _, err := tc.store.NotebookByID(context.Background(), tc.user.ID, nb.ID); err != nil {
			t.Fatalf("notebook must survive a failed delete: %v", err)
		}
	}
}

func TestNotebooksPageHasBrowseButton(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	body := tc.get("/notebooks").Body.String()
	if !strings.Contains(body, "浏览") || !strings.Contains(body, fmt.Sprintf(`href="/notebooks/%d"`, tc.notebook.ID)) {
		t.Fatalf("browse button missing: %s", body)
	}
	if !strings.Contains(body, `name="password"`) {
		t.Fatalf("delete form should ask for the password: %s", body)
	}
}

func TestNotebookWordShowsThreeLines(t *testing.T) {
	tc := newAuthedTestClient(t)
	w := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	if _, _, err := tc.store.AnswerWord(context.Background(), tc.notebook.ID, w.ID, true, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	body := tc.get(fmt.Sprintf("/notebooks/%d", tc.notebook.ID)).Body.String()
	cardAt := strings.Index(body, "card word-card")
	if cardAt < 0 {
		t.Fatalf("word card missing: %s", body)
	}
	card := body[cardAt:]
	// The English heading must be the word itself. Embedding store.Word as a
	// field named Word shadows its Word string in templates, which printed the
	// whole struct here.
	if !strings.Contains(card, `<span class="word-en">apple</span>`) {
		t.Fatalf("word heading is not the word: %s", card)
	}
	wordAt := strings.Index(card, "apple")
	zhAt := strings.Index(card, "n. 苹果")
	metaAt := strings.Index(card, "加入于")
	starsAt := strings.Index(card, "熟练度")
	countAt := strings.Index(card, "回忆 1 次")
	if wordAt < 0 || zhAt < 0 || metaAt < 0 || starsAt < 0 || countAt < 0 {
		t.Fatalf("missing line content: %s", body)
	}
	if !(wordAt < zhAt && zhAt < metaAt && metaAt < starsAt && starsAt < countAt) {
		t.Fatalf("expected word → chinese → meta order: %d %d %d %d %d", wordAt, zhAt, metaAt, starsAt, countAt)
	}
}

func TestNotebookCardUsesSettingsMenu(t *testing.T) {
	tc := newAuthedTestClient(t)
	body := tc.get("/notebooks").Body.String()
	for _, want := range []string{
		"data-settings-toggle",
		`aria-label="生词本设置"`,
		"notebook-settings",
		"浏览",
		"重命名",
		"删除",
		`name="password"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in notebook card: %s", want, body)
		}
	}
	if strings.Contains(body, "notebook-actions") {
		t.Fatalf("old action row should be gone: %s", body)
	}
}

func TestNotebookDefaultBadgeFollowsName(t *testing.T) {
	tc := newAuthedTestClient(t)
	body := tc.get("/notebooks").Body.String()
	cardAt := strings.Index(body, "card notebook-card")
	if cardAt < 0 {
		t.Fatalf("notebook card missing: %s", body)
	}
	card := body[cardAt:]
	card = card[:strings.Index(card, "</li>")]
	lineAt := strings.Index(card, `class="notebook-line"`)
	if lineAt < 0 {
		t.Fatalf("notebook name row missing: %s", card)
	}
	line := card[lineAt:]
	line = line[:strings.Index(line, "</div>")]
	if !strings.Contains(line, `class="notebook-name"`) {
		t.Fatalf("notebook name should sit in the first line: %s", line)
	}
	if !strings.Contains(line, `class="badge badge-default">默认</span>`) {
		t.Fatalf("default badge should follow the name: %s", line)
	}
	if strings.Contains(card, "notebook-corner") {
		t.Fatalf("default badge should no longer be a corner sticker: %s", card)
	}
}

func TestNotebookWordCardUsesSwipeLayout(t *testing.T) {
	tc := newAuthedTestClient(t)
	w := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	body := tc.get(fmt.Sprintf("/notebooks/%d", tc.notebook.ID)).Body.String()
	for _, want := range []string{
		"word-swipe",
		"word-content",
		"word-side",
		fmt.Sprintf(`action="/notebooks/%d/words/%d/delete"`, tc.notebook.ID, w.ID),
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in word card: %s", want, body)
		}
	}
	if strings.Contains(body, "/edit") {
		t.Fatalf("edit entry should be removed from the word card: %s", body)
	}
}
