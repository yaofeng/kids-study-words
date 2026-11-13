package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/yaofeng/kids-study-words/internal/dict"
)

func TestLookupEnglishRendersResults(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.srv.dict = fakeDict{entry: dict.Entry{Word: "apple", Phonetic: "/ˈæpl/", Translation: "n. 苹果"}}
	resp := tc.get("/lookup/results?mode=en&q=app")
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), "apple") || !strings.Contains(resp.Body.String(), "苹果") {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestLookupChineseMode(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.srv.dict = fakeDict{entry: dict.Entry{Word: "apple", Translation: "n. 苹果"}}
	resp := tc.get("/lookup/results?mode=zh&q=" + url.QueryEscape("苹果"))
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), "apple") {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestLookupPageRendersResultsInline(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.srv.dict = fakeDict{entry: dict.Entry{Word: "apple", Translation: "n. 苹果"}}
	resp := tc.get("/lookup?mode=en&q=apple")
	body := resp.Body.String()
	if resp.Code != 200 || !strings.Contains(body, "apple") || !strings.Contains(body, "加入生词本") {
		t.Fatalf("code=%d body=%s", resp.Code, body)
	}
}

func TestLookupExamplesFragment(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.srv.dict = fakeDict{
		entry:    dict.Entry{Word: "apple"},
		examples: []dict.Sentence{{EN: "An apple a day.", ZH: "一天一个苹果。"}},
	}
	resp := tc.get("/lookup/examples?word=apple")
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), "An apple a day.") {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	resp = tc.get("/lookup/examples?word=missing")
	if !strings.Contains(resp.Body.String(), "暂无例句") {
		t.Fatalf("body=%s", resp.Body.String())
	}
}

func TestAddWordSnapshotsEntry(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.srv.dict = fakeDict{entry: dict.Entry{Word: "apple", Translation: "n. 苹果"}, examples: []dict.Sentence{{EN: "An apple.", ZH: "一个苹果。"}}}
	resp := tc.postForm("/words", url.Values{"notebook_id": {fmt.Sprint(tc.notebook.ID)}, "word": {"apple"}, "csrf_token": {tc.csrf}})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	w, err := tc.store.ListWords(context.Background(), tc.notebook.ID, "added")
	if err != nil || len(w) != 1 || w[0].Translation != "n. 苹果" || len(w[0].Examples) != 1 {
		t.Fatalf("got %+v %v", w, err)
	}
}

func TestAddWordDuplicateShowsMessage(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	tc.srv.dict = fakeDict{entry: dict.Entry{Word: "apple", Translation: "n. 苹果"}}
	resp := tc.postForm("/words", url.Values{
		"notebook_id": {fmt.Sprint(tc.notebook.ID)}, "word": {"apple"}, "csrf_token": {tc.csrf},
		"next": {"/lookup?mode=en&q=apple"},
	})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d", resp.Code)
	}
	body := tc.get("/lookup?mode=en&q=apple").Body.String()
	if !strings.Contains(body, "已在《") {
		t.Fatalf("flash missing: %s", body)
	}
	list, _ := tc.store.ListWords(context.Background(), tc.notebook.ID, "added")
	if len(list) != 1 {
		t.Fatalf("count=%d", len(list))
	}
}

func TestAddWordRejectsForeignNotebook(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.srv.dict = fakeDict{entry: dict.Entry{Word: "apple", Translation: "n. 苹果"}}
	resp := tc.postForm("/words", url.Values{"notebook_id": {"99999"}, "word": {"apple"}})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d", resp.Code)
	}
	list, _ := tc.store.ListWords(context.Background(), tc.notebook.ID, "added")
	if len(list) != 0 {
		t.Fatalf("word should not be added: %+v", list)
	}
}

func TestAddWordRejectsOffsiteNext(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.srv.dict = fakeDict{entry: dict.Entry{Word: "apple", Translation: "n. 苹果"}}
	resp := tc.postForm("/words", url.Values{
		"notebook_id": {fmt.Sprint(tc.notebook.ID)},
		"word":        {"apple"},
		"next":        {"https://evil.example/"},
	})
	if loc := resp.Header().Get("Location"); loc != "/lookup" {
		t.Fatalf("location=%q", loc)
	}
}
