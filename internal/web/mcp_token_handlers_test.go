package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/yaofeng/kids-study-words/internal/auth"
	"github.com/yaofeng/kids-study-words/internal/store"
)

var mcpTokenRE = regexp.MustCompile(`mcp_[A-Za-z0-9_-]+`)

func TestGenerateMCPTokenShowsOnceAndStoresHash(t *testing.T) {
	tc := newAuthedTestClient(t)
	resp := tc.postForm("/settings/mcp-token/generate", url.Values{})
	if resp.Code != http.StatusOK || resp.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("code=%d cache=%q", resp.Code, resp.Header().Get("Cache-Control"))
	}
	body := resp.Body.String()
	if !strings.Contains(body, "令牌只显示这一次") {
		t.Fatalf("warning missing: %s", body)
	}
	raw := mcpTokenRE.FindString(body)
	if raw == "" {
		t.Fatalf("token missing: %s", body)
	}
	u, err := tc.store.UserByMCPToken(context.Background(), auth.HashToken(raw))
	if err != nil || u.ID != tc.user.ID {
		t.Fatalf("stored token: %+v %v", u, err)
	}
	page := tc.get("/settings").Body.String()
	if strings.Contains(page, raw) {
		t.Fatal("settings page must not show the token again")
	}
}

func TestRegenerateInvalidatesOldToken(t *testing.T) {
	tc := newAuthedTestClient(t)
	body := tc.postForm("/settings/mcp-token/generate", url.Values{}).Body.String()
	old := mcpTokenRE.FindString(body)
	tc.postForm("/settings/mcp-token/generate", url.Values{})
	if _, err := tc.store.UserByMCPToken(context.Background(), auth.HashToken(old)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old token still valid: %v", err)
	}
}

func TestRevokeMCPToken(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.postForm("/settings/mcp-token/generate", url.Values{})
	resp := tc.postForm("/settings/mcp-token/revoke", url.Values{})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d", resp.Code)
	}
	if _, err := tc.store.MCPTokenByUser(context.Background(), tc.user.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("token still stored: %v", err)
	}
}

func TestChangePasswordRevokesMCPToken(t *testing.T) {
	tc := newAuthedTestClient(t)
	body := tc.postForm("/settings/mcp-token/generate", url.Values{}).Body.String()
	raw := mcpTokenRE.FindString(body)
	resp := tc.postForm("/settings/password", FormValues(
		"current", "secret1", "new", "secret2", "confirm", "secret2"))
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	if _, err := tc.store.UserByMCPToken(context.Background(), auth.HashToken(raw)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("token should be revoked: %v", err)
	}
}
