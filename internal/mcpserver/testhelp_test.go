package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yaofeng/kids-study-words/internal/auth"
	"github.com/yaofeng/kids-study-words/internal/dict"
	"github.com/yaofeng/kids-study-words/internal/store"
)

type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (b bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(clone)
}

type testEnv struct {
	Store    *store.Store
	User     store.User
	Notebook store.Notebook
	Token    string
	URL      string
	Dict     *fakeDict
	Session  *mcp.ClientSession
}

type fakeDict struct {
	entries  map[string]dict.Entry
	examples []dict.Sentence
}

func (f *fakeDict) Lookup(_ context.Context, word string) (dict.Entry, error) {
	e, ok := f.entries[strings.ToLower(word)]
	if !ok {
		return dict.Entry{}, dict.ErrNotFound
	}
	return e, nil
}

func (f *fakeDict) Examples(_ context.Context, _, _ string, limit int) ([]dict.Sentence, error) {
	out := f.examples
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWithLocation(t, time.UTC)
}

func newTestEnvWithLocation(t *testing.T, loc *time.Location) *testEnv {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	u, nb, err := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	raw, hash, err := auth.NewPrefixedToken("mcp_", 32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReplaceMCPToken(ctx, u.ID, hash); err != nil {
		t.Fatal(err)
	}
	d := &fakeDict{entries: map[string]dict.Entry{}}
	h := NewHandler(Config{Store: st, Dict: d, Location: loc, Version: "test", Now: time.Now})
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).
		Connect(ctx, &mcp.StreamableClientTransport{
			Endpoint:             ts.URL,
			HTTPClient:           &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: raw}},
			DisableStandaloneSSE: true,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return &testEnv{Store: st, User: u, Notebook: nb, Token: raw, URL: ts.URL, Dict: d, Session: session}
}

func (e *testEnv) call(t *testing.T, name string, args any) *mcp.CallToolResult {
	t.Helper()
	res, err := e.Session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func decodeOut[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool error: %s", toolText(res))
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return out
}

func toolText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}
