package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yaofeng/kids-study-words/internal/auth"
	"github.com/yaofeng/kids-study-words/internal/store"
)

// newTestServer builds a Server backed by a throwaway SQLite database.
func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv, err := New(Options{
		Store:    st,
		Location: time.UTC,
		Version:  "test",
		Now:      time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv, st
}

// testClient is an authenticated browser stand-in reused by later tasks.
type testClient struct {
	t        *testing.T
	srv      *Server
	store    *store.Store
	user     store.User
	notebook store.Notebook
	cookie   *http.Cookie
	csrf     string
}

// newAuthedTestClient runs first-time setup and returns a logged-in client.
func newAuthedTestClient(t *testing.T) *testClient {
	t.Helper()
	srv, st := newTestServer(t)

	form := url.Values{"username": {"parent"}, "password": {"secret1"}, "confirm": {"secret1"}}
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("setup failed: code=%d body=%s", rec.Code, rec.Body)
	}

	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("setup did not set a session cookie")
	}

	ctx := context.Background()
	sess, err := st.SessionByToken(ctx, auth.HashToken(cookie.Value))
	if err != nil {
		t.Fatal(err)
	}
	user, err := st.UserByUsername(ctx, "parent")
	if err != nil {
		t.Fatal(err)
	}
	nb, err := st.DefaultNotebook(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	return &testClient{
		t:        t,
		srv:      srv,
		store:    st,
		user:     user,
		notebook: nb,
		cookie:   cookie,
		csrf:     sess.CSRFToken,
	}
}

func (tc *testClient) do(req *http.Request) *httptest.ResponseRecorder {
	tc.t.Helper()
	req.Header.Set("Origin", "http://example.com")
	if tc.cookie != nil {
		req.AddCookie(tc.cookie)
	}
	rec := httptest.NewRecorder()
	tc.srv.Handler().ServeHTTP(rec, req)
	return rec
}

func (tc *testClient) get(path string) *httptest.ResponseRecorder {
	tc.t.Helper()
	return tc.do(httptest.NewRequest(http.MethodGet, path, nil))
}

func (tc *testClient) postForm(path string, form url.Values) *httptest.ResponseRecorder {
	tc.t.Helper()
	form.Set("csrf_token", tc.csrf)
	return tc.postFormNoCSRF(path, form)
}

func (tc *testClient) postFormNoCSRF(path string, form url.Values) *httptest.ResponseRecorder {
	tc.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return tc.do(req)
}

// FormValues flattens a url.Values form for tests.
func FormValues(kv ...string) url.Values {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v
}

// hashCookie returns the storage hash for a session cookie value.
func hashCookie(raw string) string { return auth.HashToken(raw) }

// seedWord inserts a word directly into the store for page tests.
func (tc *testClient) seedWord(notebookID int64, word, translation string) store.Word {
	tc.t.Helper()
	w, err := tc.store.AddWord(context.Background(), notebookID, store.Word{Word: word, Translation: translation})
	if err != nil {
		tc.t.Fatal(err)
	}
	return w
}
