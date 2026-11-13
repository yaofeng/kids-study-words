package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestCreateFirstUserAndDefaultNotebook(t *testing.T) {
	st := newTestStore(t)
	u, nb, err := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	if u.ID == 0 || u.Username != "parent" || u.PasswordHash != "hash" || u.CreatedAt.IsZero() {
		t.Fatalf("bad user: %+v", u)
	}
	if nb.Name != "默认生词本" || !nb.IsDefault || nb.UserID != u.ID {
		t.Fatalf("bad notebook: %+v", nb)
	}
	if _, _, err := st.CreateFirstUser(context.Background(), "other", "h", "x"); !errors.Is(err, ErrSetupComplete) {
		t.Fatalf("expected ErrSetupComplete, got %v", err)
	}
	n, err := st.CountUsers(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("count = %d, %v", n, err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	st := newTestStore(t)
	u, _, err := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := st.CreateSession(context.Background(), "th", u.ID, "csrf", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	s, err := st.SessionByToken(context.Background(), "th")
	if err != nil || s.UserID != u.ID || s.CSRFToken != "csrf" || s.TokenHash != "th" {
		t.Fatalf("got %+v %v", s, err)
	}
	u2, err := st.UserBySession(context.Background(), "th", now)
	if err != nil || u2.ID != u.ID {
		t.Fatalf("got %+v %v", u2, err)
	}
	if _, err := st.UserBySession(context.Background(), "th", now.Add(2*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session should be rejected: %v", err)
	}
	if err := st.DeleteSession(context.Background(), "th"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SessionByToken(context.Background(), "th"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDefaultNotebook(t *testing.T) {
	st := newTestStore(t)
	u, nb, err := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.DefaultNotebook(context.Background(), u.ID)
	if err != nil || got.ID != nb.ID || !got.IsDefault {
		t.Fatalf("got %+v %v", got, err)
	}
	if _, err := st.DefaultNotebook(context.Background(), u.ID+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUserLookupsAndPasswordUpdate(t *testing.T) {
	st := newTestStore(t)
	u, _, err := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := st.UserByUsername(context.Background(), "parent"); err != nil || got.ID != u.ID {
		t.Fatalf("got %+v %v", got, err)
	}
	if _, err := st.UserByUsername(context.Background(), "nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := st.UserByID(context.Background(), u.ID+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := st.UpdatePassword(context.Background(), u.ID, "newhash"); err != nil {
		t.Fatal(err)
	}
	got, err := st.UserByID(context.Background(), u.ID)
	if err != nil || got.PasswordHash != "newhash" {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestDeleteSessionsForUser(t *testing.T) {
	st := newTestStore(t)
	u, _, err := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, th := range []string{"a", "b", "c"} {
		if err := st.CreateSession(context.Background(), th, u.ID, "csrf", now, now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.DeleteSessionsForUserExcept(context.Background(), u.ID, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SessionByToken(context.Background(), "a"); err != nil {
		t.Fatalf("kept session should survive: %v", err)
	}
	for _, th := range []string{"b", "c"} {
		if _, err := st.SessionByToken(context.Background(), th); !errors.Is(err, ErrNotFound) {
			t.Fatalf("session %s should be gone: %v", th, err)
		}
	}
	if err := st.DeleteSessionsForUser(context.Background(), u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SessionByToken(context.Background(), "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("all sessions should be gone: %v", err)
	}
}

func TestDeleteExpiredSessions(t *testing.T) {
	st := newTestStore(t)
	u, _, err := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := st.CreateSession(context.Background(), "old", u.ID, "c1", now.Add(-2*time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(context.Background(), "live", u.ID, "c2", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteExpiredSessions(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SessionByToken(context.Background(), "old"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session should be deleted: %v", err)
	}
	if _, err := st.SessionByToken(context.Background(), "live"); err != nil {
		t.Fatalf("live session should survive: %v", err)
	}
}
