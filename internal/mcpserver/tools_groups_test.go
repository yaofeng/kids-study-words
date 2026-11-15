package mcpserver

import (
	"context"
	"slices"
	"testing"

	"github.com/yaofeng/kids-study-words/internal/store"
)

func TestMCPGroupCRUD(t *testing.T) {
	env := newTestEnv(t)
	created := decodeOut[CreateGroupOutput](t, env.call(t, "create_group", map[string]any{
		"notebook_id": env.Notebook.ID, "name": "动物",
	}))
	if created.Group.Name != "动物" || created.Group.NotebookID != env.Notebook.ID {
		t.Fatalf("created=%+v", created)
	}
	listed := decodeOut[ListGroupsOutput](t, env.call(t, "list_groups", map[string]any{"notebook_id": env.Notebook.ID}))
	if len(listed.Groups) != 1 {
		t.Fatalf("listed=%+v", listed)
	}
	renamed := decodeOut[RenameGroupOutput](t, env.call(t, "rename_group", map[string]any{
		"group_id": created.Group.ID, "name": "宠物",
	}))
	if renamed.Group.Name != "宠物" {
		t.Fatalf("renamed=%+v", renamed)
	}
	if res := env.call(t, "create_group", map[string]any{"notebook_id": env.Notebook.ID, "name": "宠物"}); !res.IsError {
		t.Fatal("duplicate group should error")
	}
	deleted := decodeOut[DeleteGroupOutput](t, env.call(t, "delete_group", map[string]any{"group_id": created.Group.ID}))
	if !deleted.Deleted || deleted.Name != "宠物" {
		t.Fatalf("deleted=%+v", deleted)
	}
}

func TestMCPAddAndRemoveGroupWords(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	apple, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "apple"})
	pear, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "pear"})
	// cat 存在于生词本中但不在分组里，用来验证 not_present 分类。
	if _, err := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "cat"}); err != nil {
		t.Fatal(err)
	}
	g, _ := env.Store.CreateWordGroup(ctx, env.Notebook.ID, "水果")
	_ = env.Store.SetGroupWords(ctx, env.User.ID, g.ID, []int64{pear.ID})

	added := decodeOut[AddGroupWordsOutput](t, env.call(t, "add_group_words", map[string]any{
		"group_id": g.ID, "words": []string{"Apple", "pear", "ghost"},
	}))
	if !slices.Equal(added.Added, []string{"Apple"}) ||
		!slices.Equal(added.AlreadyPresent, []string{"pear"}) ||
		!slices.Equal(added.NotFound, []string{"ghost"}) {
		t.Fatalf("added=%+v", added)
	}
	ids, _ := env.Store.GroupWordIDs(ctx, env.User.ID, g.ID)
	if !slices.Equal(ids, []int64{apple.ID, pear.ID}) {
		t.Fatalf("ids=%v", ids)
	}

	removed := decodeOut[RemoveGroupWordsOutput](t, env.call(t, "remove_group_words", map[string]any{
		"group_id": g.ID, "words": []string{"apple", "cat", "ghost"},
	}))
	if !slices.Equal(removed.Removed, []string{"apple"}) ||
		!slices.Equal(removed.NotPresent, []string{"cat"}) ||
		!slices.Equal(removed.NotFound, []string{"ghost"}) {
		t.Fatalf("removed=%+v", removed)
	}
}

func TestMCPGetGroupPaginates(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	var ids []int64
	for _, name := range []string{"apple", "pear", "cat"} {
		w, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: name})
		ids = append(ids, w.ID)
	}
	g, _ := env.Store.CreateWordGroup(ctx, env.Notebook.ID, "动物")
	_ = env.Store.SetGroupWords(ctx, env.User.ID, g.ID, ids)

	out := decodeOut[GetGroupOutput](t, env.call(t, "get_group", map[string]any{
		"group_id": g.ID, "limit": 2, "offset": 0,
	}))
	if out.Total != 3 || len(out.Words) != 2 || !out.HasMore || out.NextOffset != 2 {
		t.Fatalf("out=%+v", out)
	}
}

func TestMCPGroupToolsRejectForeignGroup(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	other, _ := env.Store.CreateNotebook(ctx, env.User.ID, "另一本")
	g, _ := env.Store.CreateWordGroup(ctx, other.ID, "动物")
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{"get_group", map[string]any{"group_id": 99999}},
		{"rename_group", map[string]any{"group_id": 99999, "name": "x"}},
		{"delete_group", map[string]any{"group_id": 99999}},
		{"add_group_words", map[string]any{"group_id": 99999, "words": []string{"apple"}}},
		{"remove_group_words", map[string]any{"group_id": 99999, "words": []string{"apple"}}},
	} {
		if res := env.call(t, call.name, call.args); !res.IsError {
			t.Errorf("%s should reject missing group", call.name)
		}
	}
	// 该组属于当前用户，但不在请求指定的生词本里时，list_words 必须拒绝。
	apple, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "apple"})
	_ = apple
	if res := env.call(t, "list_words", map[string]any{"notebook_id": env.Notebook.ID, "group_id": g.ID}); !res.IsError {
		t.Fatal("cross-notebook group filter should error")
	}
}
