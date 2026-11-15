package store

import (
	"context"
	"errors"
	"testing"
)

func TestCreateUserAddsAnotherAccount(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	first, _, err := st.CreateFirstUser(ctx, "parent", "hash1", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	second, nb, err := st.CreateUser(ctx, "kid", "hash2", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || second.Username != "kid" || second.PasswordHash != "hash2" {
		t.Fatalf("bad user: %+v", second)
	}
	if nb.UserID != second.ID || nb.Name != "默认生词本" || !nb.IsDefault {
		t.Fatalf("bad notebook: %+v", nb)
	}
	if n, err := st.CountUsers(ctx); err != nil || n != 2 {
		t.Fatalf("count = %d, %v", n, err)
	}
	got, err := st.UserByUsername(ctx, "kid")
	if err != nil || got.ID != second.ID {
		t.Fatalf("user by name = %+v, %v", got, err)
	}
}

func TestUpdatePasswordRevokesMCPToken(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, _, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	_, _ = st.ReplaceMCPToken(ctx, u.ID, "hash1")
	if err := st.UpdatePassword(ctx, u.ID, "newhash"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.UserByID(ctx, u.ID)
	if got.PasswordHash != "newhash" {
		t.Fatalf("hash=%q", got.PasswordHash)
	}
	if _, err := st.UserByMCPToken(ctx, "hash1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("token should be revoked: %v", err)
	}
}

func TestCreateUserRejectsDuplicateUsername(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	if _, _, err := st.CreateFirstUser(ctx, "parent", "hash1", "默认生词本"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateUser(ctx, "parent", "hash2", "默认生词本"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
	if n, err := st.CountUsers(ctx); err != nil || n != 1 {
		t.Fatalf("count = %d, %v", n, err)
	}
}
