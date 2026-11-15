package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yaofeng/kids-study-words/internal/auth"
)

// bearerTransportForTest adds a bearer token to every MCP request.
type bearerTransportForTest struct {
	base  http.RoundTripper
	token string
}

func (b bearerTransportForTest) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	base := b.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

func TestWebMountsMCPWithoutSessionOrCSRF(t *testing.T) {
	tc := newAuthedTestClient(t)
	raw, hash, err := auth.NewPrefixedToken("mcp_", 32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tc.store.ReplaceMCPToken(context.Background(), tc.user.ID, hash); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(tc.srv.Handler())
	defer ts.Close()

	session, err := mcp.NewClient(&mcp.Implementation{Name: "mount", Version: "1"}, nil).
		Connect(context.Background(), &mcp.StreamableClientTransport{
			Endpoint:             ts.URL + "/mcp",
			HTTPClient:           &http.Client{Transport: bearerTransportForTest{token: raw}},
			DisableStandaloneSSE: true,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.ListTools(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	// No token must be 401, not a 303 redirect to /login.
	resp, err := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}
