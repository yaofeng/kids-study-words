package mcpserver

import (
	"context"
	"testing"

	"github.com/yaofeng/kids-study-words/internal/dict"
	"github.com/yaofeng/kids-study-words/internal/store"
)

func TestMCPListWordsFilterSortPaginate(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	apple, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "apple", Proficiency: 3})
	pear, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "pear", Proficiency: 7})
	if _, err := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "cat", Proficiency: 9}); err != nil {
		t.Fatal(err)
	}
	g, _ := env.Store.CreateWordGroup(ctx, env.Notebook.ID, "动物")
	_ = env.Store.SetGroupWords(ctx, env.User.ID, g.ID, []int64{apple.ID, pear.ID})

	all := decodeOut[ListWordsOutput](t, env.call(t, "list_words", map[string]any{"notebook_id": env.Notebook.ID, "sort": "proficiency"}))
	if all.Total != 3 || all.Words[0].Word != "apple" || all.Words[2].Word != "cat" {
		t.Fatalf("all=%+v", all)
	}
	page := decodeOut[ListWordsOutput](t, env.call(t, "list_words", map[string]any{
		"notebook_id": env.Notebook.ID, "limit": 1, "offset": 1,
	}))
	if page.Total != 3 || len(page.Words) != 1 || !page.HasMore || page.NextOffset != 2 {
		t.Fatalf("page=%+v", page)
	}
	group := decodeOut[ListWordsOutput](t, env.call(t, "list_words", map[string]any{
		"notebook_id": env.Notebook.ID, "group_id": g.ID,
	}))
	if group.Total != 2 {
		t.Fatalf("group=%+v", group)
	}
}

func TestMCPAddAndRemoveWord(t *testing.T) {
	env := newTestEnv(t)
	env.Dict.entries["apple"] = dict.Entry{Word: "apple", Phonetic: "/ˈæpl/", Translation: "n. 苹果", Exchange: "p:apples"}
	env.Dict.examples = []dict.Sentence{{EN: "An apple.", ZH: "一个苹果。"}}

	added := decodeOut[AddWordOutput](t, env.call(t, "add_word", map[string]any{
		"notebook_id": env.Notebook.ID, "word": "Apple",
	}))
	if added.Word.Word != "apple" || added.Word.Phonetic != "/ˈæpl/" || len(added.Word.Examples) != 1 {
		t.Fatalf("added=%+v", added)
	}
	if res := env.call(t, "add_word", map[string]any{"notebook_id": env.Notebook.ID, "word": "apple"}); !res.IsError {
		t.Fatal("duplicate should error")
	}

	g, _ := env.Store.CreateWordGroup(context.Background(), env.Notebook.ID, "水果")
	_ = env.Store.SetGroupWords(context.Background(), env.User.ID, g.ID, []int64{added.Word.ID})
	removed := decodeOut[RemoveWordOutput](t, env.call(t, "remove_word", map[string]any{
		"notebook_id": env.Notebook.ID, "word": "APPLE",
	}))
	if !removed.Removed || removed.Word != "apple" || len(removed.Groups) != 1 {
		t.Fatalf("removed=%+v", removed)
	}
}

func TestMCPAddWordDictionaryMiss(t *testing.T) {
	env := newTestEnv(t)
	res := env.call(t, "add_word", map[string]any{"notebook_id": env.Notebook.ID, "word": "zzz"})
	if !res.IsError {
		t.Fatal("dictionary miss should error")
	}
}

func TestMCPWordToolsRejectForeignNotebook(t *testing.T) {
	env := newTestEnv(t)
	other, _ := env.Store.CreateNotebook(context.Background(), env.User.ID, "另一本")
	g, _ := env.Store.CreateWordGroup(context.Background(), other.ID, "动物")
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{"list_words", map[string]any{"notebook_id": env.Notebook.ID, "group_id": g.ID}},
		{"add_word", map[string]any{"notebook_id": 99999, "word": "apple"}},
		{"remove_word", map[string]any{"notebook_id": 99999, "word": "apple"}},
	} {
		if res := env.call(t, call.name, call.args); !res.IsError {
			t.Errorf("%s should reject foreign notebook", call.name)
		}
	}
}
