package mcpserver

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yaofeng/kids-study-words/internal/store"
)

func TestMCPListNotebooks(t *testing.T) {
	env := newTestEnv(t)
	out := decodeOut[ListNotebooksOutput](t, env.call(t, "list_notebooks", nil))
	if len(out.Notebooks) != 1 || out.Notebooks[0].Name != "默认生词本" {
		t.Fatalf("out=%+v", out)
	}
}

func TestMCPRequiresBearerToken(t *testing.T) {
	env := newTestEnv(t)
	for _, token := range []string{"", "wrong"} {
		status := postMCPStatus(t, env.URL, token)
		if status != http.StatusUnauthorized {
			t.Fatalf("token %q status=%d", token, status)
		}
	}
}

func TestMCPRejectsRevokedToken(t *testing.T) {
	env := newTestEnv(t)
	if err := env.Store.RevokeMCPToken(context.Background(), env.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Session.ListTools(context.Background(), nil); err == nil {
		t.Fatal("revoked token should fail")
	}
}

func TestMCPUserIsolation(t *testing.T) {
	env := newTestEnv(t)
	other := env.addUser(t, "child", "孩子的本")
	out := decodeOut[ListNotebooksOutput](t, other.call(t, "list_notebooks", nil))
	if len(out.Notebooks) != 1 || out.Notebooks[0].Name != "孩子的本" {
		t.Fatalf("isolation broken: %+v", out)
	}
}

func TestMCPCrossAccountToolIsolation(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	aWord, err := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "apple"})
	if err != nil {
		t.Fatal(err)
	}
	aGroup, err := env.Store.CreateWordGroup(ctx, env.Notebook.ID, "动物")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := env.Store.AnswerWord(ctx, env.Notebook.ID, aWord.ID, true,
		time.Date(2026, 11, 15, 2, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	other := env.addUser(t, "child", "孩子的本")
	// 工具加载生词本和分组时都按令牌账号校验，所以用 A 的 ID 调用必须返回
	// 工具错误，而不是读到 A 的数据。
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{"list_words", map[string]any{"notebook_id": env.Notebook.ID}},
		{"get_group", map[string]any{"group_id": aGroup.ID}},
		{"add_group_words", map[string]any{"group_id": aGroup.ID, "words": []string{"apple"}}},
		{"query_recall_logs", map[string]any{"notebook_id": env.Notebook.ID}},
		{"query_recall_logs", map[string]any{"group_id": aGroup.ID}},
	} {
		if res := other.call(t, call.name, call.args); !res.IsError {
			t.Errorf("%s must reject another account's IDs", call.name)
		}
	}
	// 不带过滤时也只能看到本账号的记录，A 的记录不可见。
	out := decodeOut[QueryRecallLogsOutput](t, other.call(t, "query_recall_logs", nil))
	if out.Total != 0 || len(out.Logs) != 0 {
		t.Fatalf("cross-account recall logs leaked: %+v", out)
	}
}

func postMCPStatus(t *testing.T, url, token string) int {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"x","version":"1"}}}`
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}
