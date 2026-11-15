package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMCPTokenLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, _, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	if _, err := st.MCPTokenByUser(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty: %v", err)
	}
	tok, err := st.ReplaceMCPToken(ctx, u.ID, "hash1")
	if err != nil || tok.UserID != u.ID || tok.LastUsedAt != nil {
		t.Fatalf("tok=%+v err=%v", tok, err)
	}
	got, err := st.MCPTokenByUser(ctx, u.ID)
	if err != nil || got.CreatedAt.IsZero() || got.LastUsedAt != nil {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if _, err := st.ReplaceMCPToken(ctx, u.ID, "hash2"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UserByMCPToken(ctx, "hash1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old hash should be gone: %v", err)
	}
	if got, err := st.UserByMCPToken(ctx, "hash2"); err != nil || got.ID != u.ID {
		t.Fatalf("new hash: %+v %v", got, err)
	}
	if err := st.RevokeMCPToken(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MCPTokenByUser(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after revoke: %v", err)
	}
}

func TestTouchMCPTokenThrottles(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, _, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	_, _ = st.ReplaceMCPToken(ctx, u.ID, "hash1")
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	if err := st.TouchMCPToken(ctx, u.ID, t0); err != nil {
		t.Fatal(err)
	}
	got, _ := st.MCPTokenByUser(ctx, u.ID)
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(t0) {
		t.Fatalf("last=%v", got.LastUsedAt)
	}
	if err := st.TouchMCPToken(ctx, u.ID, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, _ = st.MCPTokenByUser(ctx, u.ID)
	if !got.LastUsedAt.Equal(t0) {
		t.Fatalf("should throttle: %v", got.LastUsedAt)
	}
	if err := st.TouchMCPToken(ctx, u.ID, t0.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, _ = st.MCPTokenByUser(ctx, u.ID)
	if !got.LastUsedAt.Equal(t0.Add(6 * time.Minute)) {
		t.Fatalf("should update: %v", got.LastUsedAt)
	}
}
