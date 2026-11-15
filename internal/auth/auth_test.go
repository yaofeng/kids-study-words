package auth

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordAndTokens(t *testing.T) {
	h, err := HashPassword("secret1")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "secret1") {
		t.Fatal("correct password should match")
	}
	if CheckPassword(h, "wrong") {
		t.Fatal("wrong password should not match")
	}
	if h == "secret1" {
		t.Fatal("password must be hashed")
	}

	raw, hashed, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if raw == "" || hashed != HashToken(raw) {
		t.Fatalf("token mismatch: raw=%q hashed=%q", raw, hashed)
	}
	raw2, _, err := NewToken()
	if err != nil || raw2 == raw {
		t.Fatalf("tokens must be unique: %q %q %v", raw, raw2, err)
	}

	csrf, err := NewCSRFToken()
	if err != nil || len(csrf) < 20 {
		t.Fatalf("csrf = %q, %v", csrf, err)
	}
}

func TestNewPrefixedToken(t *testing.T) {
	raw, hash, err := NewPrefixedToken("mcp_", 32)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "mcp_") || len(raw) != 47 {
		t.Fatalf("raw=%q len=%d", raw, len(raw))
	}
	if hash != HashToken(raw) {
		t.Fatal("hash mismatch")
	}
	raw2, _, _ := NewPrefixedToken("mcp_", 32)
	if raw2 == raw {
		t.Fatal("tokens should differ")
	}
}

func TestLoginLimiter(t *testing.T) {
	now := time.Unix(0, 0)
	l := NewLoginLimiter(5, time.Minute, func() time.Time { return now })
	for i := 0; i < 5; i++ {
		if !l.Allow("ip") {
			t.Fatal("should allow")
		}
		l.Fail("ip")
	}
	if l.Allow("ip") {
		t.Fatal("should block after 5")
	}
	if !l.Allow("other") {
		t.Fatal("other keys should not be blocked")
	}
	now = now.Add(time.Minute)
	if !l.Allow("ip") {
		t.Fatal("should allow after window")
	}
	l.Fail("ip")
	l.Reset("ip")
	if !l.Allow("ip") {
		t.Fatal("reset should clear failures")
	}
}

func TestValidUsername(t *testing.T) {
	cases := map[string]bool{
		"parent":                true,
		"小明":                    true,
		"":                      false,
		"   ":                   false,
		" a ":                   true,
		strings.Repeat("a", 32): true,
		strings.Repeat("a", 33): false,
		"bad\nname":             false,
	}
	for name, want := range cases {
		if got := ValidUsername(name); got != want {
			t.Errorf("ValidUsername(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestValidPassword(t *testing.T) {
	cases := map[string]bool{
		"123456":                true,
		"12345":                 false,
		strings.Repeat("a", 72): true,
		strings.Repeat("a", 73): false,
		"":                      false,
	}
	for pw, want := range cases {
		if got := ValidPassword(pw); got != want {
			t.Errorf("ValidPassword(%d chars) = %v, want %v", len(pw), got, want)
		}
	}
}
