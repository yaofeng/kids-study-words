package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func postRegister(t *testing.T, srv *Server, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestLoginPageShowsRegisterButton(t *testing.T) {
	tc := newAuthedTestClient(t)
	rec := httptest.NewRecorder()
	tc.srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `href="/register"`) || !strings.Contains(body, "注册新用户") {
		t.Fatalf("register button missing: %s", body)
	}
}

func TestRegisterPageAsksForExistingAccount(t *testing.T) {
	tc := newAuthedTestClient(t)
	rec := httptest.NewRecorder()
	tc.srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/register", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`action="/register"`,
		`name="authorizer"`,
		`name="authorizer_password"`,
		`name="username"`,
		`name="password"`,
		`name="confirm"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in register page: %s", want, body)
		}
	}
}

func TestRegisterCreatesAccount(t *testing.T) {
	tc := newAuthedTestClient(t)
	rec := postRegister(t, tc.srv, FormValues(
		"authorizer", "parent",
		"authorizer_password", "secret1",
		"username", "kid",
		"password", "secret9",
		"confirm", "secret9",
	))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("code=%d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}

	ctx := context.Background()
	if n, err := tc.store.CountUsers(ctx); err != nil || n != 2 {
		t.Fatalf("count = %d, %v", n, err)
	}
	kid, err := tc.store.UserByUsername(ctx, "kid")
	if err != nil {
		t.Fatal(err)
	}
	nb, err := tc.store.DefaultNotebook(ctx, kid.ID)
	if err != nil || nb.UserID != kid.ID || nb.ID == tc.notebook.ID {
		t.Fatalf("kid notebook = %+v, %v", nb, err)
	}

	loginForm := url.Values{"username": {"kid"}, "password": {"secret9"}}
	login := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(loginForm.Encode()))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login.Header.Set("Origin", "http://example.com")
	rec = httptest.NewRecorder()
	tc.srv.Handler().ServeHTTP(rec, login)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/lookup" {
		t.Fatalf("new account cannot log in: code=%d body=%s", rec.Code, rec.Body)
	}
}

func TestRegisterRequiresCorrectExistingPassword(t *testing.T) {
	tc := newAuthedTestClient(t)
	rec := postRegister(t, tc.srv, FormValues(
		"authorizer", "parent",
		"authorizer_password", "wrong",
		"username", "kid",
		"password", "secret9",
		"confirm", "secret9",
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "现有账号的用户名或密码不对") {
		t.Fatalf("body=%s", rec.Body)
	}
	if n, _ := tc.store.CountUsers(context.Background()); n != 1 {
		t.Fatalf("account should not be created, count=%d", n)
	}
}

func TestRegisterRejectsDuplicateUsername(t *testing.T) {
	tc := newAuthedTestClient(t)
	rec := postRegister(t, tc.srv, FormValues(
		"authorizer", "parent",
		"authorizer_password", "secret1",
		"username", "parent",
		"password", "secret9",
		"confirm", "secret9",
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "已经有人用了") {
		t.Fatalf("body=%s", rec.Body)
	}
	if n, _ := tc.store.CountUsers(context.Background()); n != 1 {
		t.Fatalf("duplicate should not be created, count=%d", n)
	}
}

func TestRegisterRejectsMismatchedPasswords(t *testing.T) {
	tc := newAuthedTestClient(t)
	rec := postRegister(t, tc.srv, FormValues(
		"authorizer", "parent",
		"authorizer_password", "secret1",
		"username", "kid",
		"password", "secret9",
		"confirm", "secret8",
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "不一致") {
		t.Fatalf("body=%s", rec.Body)
	}
	if n, _ := tc.store.CountUsers(context.Background()); n != 1 {
		t.Fatalf("mismatch should not create an account, count=%d", n)
	}
}

func TestRegisterRedirectsToSetupWithoutUsers(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/register", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/setup" {
		t.Fatalf("code=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestRegisterPageRedirectsLoggedInUser(t *testing.T) {
	tc := newAuthedTestClient(t)
	resp := tc.get("/register")
	if resp.Code != http.StatusSeeOther || resp.Header().Get("Location") != "/lookup" {
		t.Fatalf("code=%d location=%q", resp.Code, resp.Header().Get("Location"))
	}
}

func TestRegisterRejectsCrossOriginPost(t *testing.T) {
	tc := newAuthedTestClient(t)
	form := FormValues(
		"authorizer", "parent",
		"authorizer_password", "secret1",
		"username", "kid",
		"password", "secret9",
		"confirm", "secret9",
	)
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	tc.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin registration should be rejected, code=%d", rec.Code)
	}
}

func TestRegisterRateLimited(t *testing.T) {
	tc := newAuthedTestClient(t)
	var last *httptest.ResponseRecorder
	for i := 0; i < 6; i++ {
		last = postRegister(t, tc.srv, FormValues(
			"authorizer", "parent",
			"authorizer_password", "wrong",
			"username", "kid",
			"password", "secret9",
			"confirm", "secret9",
		))
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("code=%d body=%s", last.Code, last.Body)
	}
	if !strings.Contains(last.Body.String(), "请稍后再试") {
		t.Fatalf("body=%s", last.Body)
	}
}
