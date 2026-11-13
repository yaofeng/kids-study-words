package web

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/yaofeng/kids-study-words/internal/ai"
	"github.com/yaofeng/kids-study-words/internal/dict"
	"github.com/yaofeng/kids-study-words/internal/store"
)

func TestExamplesPreferCacheThenAI(t *testing.T) {
	tc := newAuthedTestClient(t)
	fake := &fakeAI{enabled: true, model: "test-model", host: "ai.example.com", ex: []ai.Example{{EN: "I go home.", ZH: "我回家。"}}}
	tc.srv.ai = fake
	tc.srv.dict = fakeDict{
		entry:    dict.Entry{Word: "go", Translation: "v. 去"},
		examples: []dict.Sentence{{EN: "Go now.", ZH: "现在走。"}},
	}
	body := tc.get("/lookup/examples?word=go").Body.String()
	if !strings.Contains(body, "I go home.") || fake.calls != 1 {
		t.Fatalf("body=%s calls=%d", body, fake.calls)
	}
	tc.get("/lookup/examples?word=go")
	if fake.calls != 1 {
		t.Fatalf("cache miss, calls=%d", fake.calls)
	}
}

func TestExamplesFallBackToLocalWhenAIErrors(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.srv.ai = &fakeAI{enabled: true, model: "m", err: errors.New("boom")}
	tc.srv.dict = fakeDict{
		entry:    dict.Entry{Word: "go", Translation: "v. 去"},
		examples: []dict.Sentence{{EN: "Go now.", ZH: "现在走。"}},
	}
	body := tc.get("/lookup/examples?word=go").Body.String()
	if !strings.Contains(body, "Go now.") {
		t.Fatalf("local fallback missing: %s", body)
	}
}

func TestSettingsNeverRendersAPIKey(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.srv.ai = &fakeAI{enabled: true, model: "test-model", host: "ai.example.com"}
	body := tc.get("/settings").Body.String()
	if strings.Contains(body, "sk-secret") {
		t.Fatal("api key leaked")
	}
	if !strings.Contains(body, "test-model") {
		t.Fatal("model missing")
	}
}

func TestExamplesSkipAIWhenLocalIsEnough(t *testing.T) {
	tc := newAuthedTestClient(t)
	fake := &fakeAI{enabled: true, model: "m", ex: []ai.Example{{EN: "I go home.", ZH: "我回家。"}}}
	tc.srv.ai = fake
	tc.srv.dict = fakeDict{
		entry:    dict.Entry{Word: "go", Translation: "v. 去"},
		examples: []dict.Sentence{{EN: "Go now.", ZH: "现在走。"}, {EN: "Let's go.", ZH: "我们走吧。"}},
	}
	body := tc.get("/lookup/examples?word=go").Body.String()
	if fake.calls != 0 {
		t.Fatalf("AI should not be called when local has two sentences: calls=%d body=%s", fake.calls, body)
	}
	if !strings.Contains(body, "Go now.") {
		t.Fatalf("local examples missing: %s", body)
	}
}

func TestAddWordSnapshotsCacheOrLocalWithoutCallingAI(t *testing.T) {
	tc := newAuthedTestClient(t)
	fake := &fakeAI{enabled: true, model: "m", ex: []ai.Example{{EN: "I go home.", ZH: "我回家。"}}}
	tc.srv.ai = fake
	tc.srv.dict = fakeDict{
		entry:    dict.Entry{Word: "go", Translation: "v. 去"},
		examples: []dict.Sentence{{EN: "Go now.", ZH: "现在走。"}},
	}
	resp := tc.postForm("/words", url.Values{
		"word":        {"go"},
		"notebook_id": {strconv.FormatInt(tc.notebook.ID, 10)},
	})
	if resp.Code != 303 {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body)
	}
	if fake.calls != 0 {
		t.Fatalf("adding a word must not call AI, calls=%d", fake.calls)
	}
	words, err := tc.store.ListWords(context.Background(), tc.notebook.ID, "added")
	if err != nil || len(words) != 1 || len(words[0].Examples) != 1 || words[0].Examples[0].EN != "Go now." {
		t.Fatalf("words=%+v err=%v", words, err)
	}
}

func TestAddWordPrefersCachedExamples(t *testing.T) {
	tc := newAuthedTestClient(t)
	if err := tc.store.SaveExamples(context.Background(), "go", "m", []store.Example{{EN: "I went home.", ZH: "我回家了。", Source: "ai"}}); err != nil {
		t.Fatal(err)
	}
	tc.srv.dict = fakeDict{
		entry:    dict.Entry{Word: "go", Translation: "v. 去"},
		examples: []dict.Sentence{{EN: "Go now.", ZH: "现在走。"}},
	}
	resp := tc.postForm("/words", url.Values{
		"word":        {"go"},
		"notebook_id": {strconv.FormatInt(tc.notebook.ID, 10)},
	})
	if resp.Code != 303 {
		t.Fatalf("code=%d", resp.Code)
	}
	words, err := tc.store.ListWords(context.Background(), tc.notebook.ID, "added")
	if err != nil || len(words) != 1 || len(words[0].Examples) != 1 || words[0].Examples[0].EN != "I went home." {
		t.Fatalf("words=%+v err=%v", words, err)
	}
}
