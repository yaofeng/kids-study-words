package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/yaofeng/kids-study-words/internal/auth"
	"github.com/yaofeng/kids-study-words/internal/store"
)

func TestChangePasswordKeepsCurrentSessionAndDropsOthers(t *testing.T) {
	tc := newAuthedTestClient(t)
	_, otherHash, err := auth.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := tc.store.CreateSession(context.Background(), otherHash, tc.user.ID, "csrf2", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	resp := tc.postForm("/settings/password", url.Values{
		"current": {"secret1"}, "new": {"secret2"}, "confirm": {"secret2"},
	})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	if loc := resp.Header().Get("Location"); loc != "/settings" {
		t.Fatalf("location=%q", loc)
	}
	if body := tc.get("/settings").Body.String(); !strings.Contains(body, "密码已更新") {
		t.Fatalf("flash missing")
	}
	if _, err := tc.store.SessionByToken(context.Background(), otherHash); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("other session still present: %v", err)
	}
	if _, err := tc.store.SessionByToken(context.Background(), hashCookie(tc.cookie.Value)); err != nil {
		t.Fatalf("current session should survive: %v", err)
	}
	if !auth.CheckPassword(passwordHash(tc), "secret2") {
		t.Fatalf("new password should work")
	}
}

// passwordHash loads the stored hash through the user record.
func passwordHash(tc *testClient) string {
	tc.t.Helper()
	u, err := tc.store.UserByID(context.Background(), tc.user.ID)
	if err != nil {
		tc.t.Fatal(err)
	}
	return u.PasswordHash
}

func TestChangePasswordRejectsWrongCurrentPassword(t *testing.T) {
	tc := newAuthedTestClient(t)
	resp := tc.postForm("/settings/password", url.Values{
		"current": {"wrong"}, "new": {"secret2"}, "confirm": {"secret2"},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("code=%d", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), "当前密码不对") {
		t.Fatalf("expected error message, got %s", resp.Body)
	}
}

func TestChangePasswordRejectsShortOrMismatched(t *testing.T) {
	tc := newAuthedTestClient(t)
	cases := []url.Values{
		{"current": {"secret1"}, "new": {"abc"}, "confirm": {"abc"}},
		{"current": {"secret1"}, "new": {"secret2"}, "confirm": {"secret3"}},
	}
	for i, form := range cases {
		resp := tc.postForm("/settings/password", form)
		if resp.Code != http.StatusOK {
			t.Fatalf("case %d code=%d", i, resp.Code)
		}
		if !strings.Contains(resp.Body.String(), "新密码") {
			t.Fatalf("case %d missing validation message: %s", i, resp.Body)
		}
	}
}

func TestSettingsPageShowsAccountAndAbout(t *testing.T) {
	tc := newAuthedTestClient(t)
	body := tc.get("/settings").Body.String()
	for _, want := range []string{
		"parent", "修改密码", "退出登录", "关于",
		"https://github.com/yaofeng/kids-study-words", "MIT",
		"ECDICT", "Tatoeba", "htmx", "Tailwind", "Nunito",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("settings page missing %q", want)
		}
	}
}
