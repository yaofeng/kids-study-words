package mcpserver

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yaofeng/kids-study-words/internal/auth"
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
	ctx := context.Background()
	u2, _, _ := env.Store.CreateUser(ctx, "child", "hash", "孩子的本")
	raw, hash, _ := auth.NewPrefixedToken("mcp_", 32)
	_, _ = env.Store.ReplaceMCPToken(ctx, u2.ID, hash)
	sess, err := mcp.NewClient(&mcp.Implementation{Name: "test2", Version: "1"}, nil).
		Connect(ctx, &mcp.StreamableClientTransport{
			Endpoint:             env.URL,
			HTTPClient:           &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: raw}},
			DisableStandaloneSSE: true,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: "list_notebooks"})
	if err != nil {
		t.Fatal(err)
	}
	out := decodeOut[ListNotebooksOutput](t, res)
	if len(out.Notebooks) != 1 || out.Notebooks[0].Name != "孩子的本" {
		t.Fatalf("isolation broken: %+v", out)
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
