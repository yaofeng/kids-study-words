package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSetupCreatesUserAndDefaultNotebook(t *testing.T) {
	srv, st := newTestServer(t)
	form := url.Values{"username": {"parent"}, "password": {"secret1"}, "confirm": {"secret1"}}
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	u, err := st.UserByUsername(context.Background(), "parent")
	if err != nil {
		t.Fatal(err)
	}
	nb, err := st.DefaultNotebook(context.Background(), u.ID)
	if err != nil || !nb.IsDefault {
		t.Fatalf("default notebook: %+v %v", nb, err)
	}
	if got := rec.Header().Get("Location"); got != "/lookup" {
		t.Fatalf("location=%q", got)
	}
	var sessionSet bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" && c.HttpOnly {
			sessionSet = true
		}
	}
	if !sessionSet {
		t.Fatal("expected HttpOnly session cookie")
	}
}

func TestSetupRejectsMismatchedPasswords(t *testing.T) {
	srv, st := newTestServer(t)
	form := url.Values{"username": {"parent"}, "password": {"secret1"}, "confirm": {"secret2"}}
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "不一致") {
		t.Fatalf("expected mismatch message, got %s", rec.Body)
	}
	if n, _ := st.CountUsers(context.Background()); n != 0 {
		t.Fatalf("user should not be created, count=%d", n)
	}
}

func TestSetupDisabledAfterFirstUser(t *testing.T) {
	srv, st := newTestServer(t)
	if _, _, err := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本"); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestPostWithoutCSRFRejected(t *testing.T) {
	tc := newAuthedTestClient(t)
	resp := tc.postFormNoCSRF("/notebooks", url.Values{"name": {"动物"}})
	if resp.Code != http.StatusForbidden {
		t.Fatalf("code=%d", resp.Code)
	}
}

func TestPostWithCSRFPassesGate(t *testing.T) {
	tc := newAuthedTestClient(t)
	// No /notebooks route exists yet, so the gate lets the request through to
	// the router, which answers 405 (path exists for GET only) rather than 403.
	resp := tc.postForm("/notebooks", url.Values{"name": {"动物"}})
	if resp.Code == http.StatusForbidden {
		t.Fatalf("CSRF gate wrongly rejected a valid token: body=%s", resp.Body)
	}
}

func TestUnauthenticatedRedirectsToLogin(t *testing.T) {
	srv, st := newTestServer(t)
	if _, _, err := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本"); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/lookup", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("code=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestUnauthenticatedHtmxGets401(t *testing.T) {
	srv, st := newTestServer(t)
	if _, _, err := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/lookup", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("HX-Redirect") != "/login" {
		t.Fatalf("code=%d hx-redirect=%q", rec.Code, rec.Header().Get("HX-Redirect"))
	}
}

func TestSetupGateRedirectsToSetup(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/lookup", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/setup" {
		t.Fatalf("code=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestLoginAndLogout(t *testing.T) {
	tc := newAuthedTestClient(t)

	// A fresh client without the cookie logs in with the same credentials.
	form := url.Values{"username": {"parent"}, "password": {"secret1"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	tc.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/lookup" {
		t.Fatalf("code=%d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	var loginCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			loginCookie = c
		}
	}
	if loginCookie == nil {
		t.Fatal("login should set session cookie")
	}

	// Logout with the logged-in client destroys its session.
	resp := tc.postForm("/logout", url.Values{})
	if resp.Code != http.StatusSeeOther || resp.Header().Get("Location") != "/login" {
		t.Fatalf("logout code=%d location=%q", resp.Code, resp.Header().Get("Location"))
	}
	if _, err := tc.store.SessionByToken(context.Background(), hashCookie(tc.cookie.Value)); err == nil {
		t.Fatal("session should be deleted after logout")
	}
}

func TestLoginWrongPassword(t *testing.T) {
	tc := newAuthedTestClient(t)
	form := url.Values{"username": {"parent"}, "password": {"nope"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	tc.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "用户名或密码不对") {
		t.Fatalf("body=%s", rec.Body)
	}
}

func TestLoginRateLimited(t *testing.T) {
	tc := newAuthedTestClient(t)
	form := url.Values{"username": {"parent"}, "password": {"nope"}}
	var last *httptest.ResponseRecorder
	for i := 0; i < 6; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://example.com")
		last = httptest.NewRecorder()
		tc.srv.Handler().ServeHTTP(last, req)
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("code=%d body=%s", last.Code, last.Body)
	}
	if !strings.Contains(last.Body.String(), "请稍后再试") {
		t.Fatalf("body=%s", last.Body)
	}
}

func TestLoginPageRedirectsWhenLoggedIn(t *testing.T) {
	tc := newAuthedTestClient(t)
	resp := tc.get("/login")
	if resp.Code != http.StatusSeeOther || resp.Header().Get("Location") != "/lookup" {
		t.Fatalf("code=%d location=%q", resp.Code, resp.Header().Get("Location"))
	}
}

func TestLoginRedirectsToSetupWithoutUsers(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/setup" {
		t.Fatalf("code=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestRootRedirectsToLookup(t *testing.T) {
	tc := newAuthedTestClient(t)
	resp := tc.get("/")
	if resp.Code != http.StatusSeeOther || resp.Header().Get("Location") != "/lookup" {
		t.Fatalf("code=%d location=%q", resp.Code, resp.Header().Get("Location"))
	}
}
