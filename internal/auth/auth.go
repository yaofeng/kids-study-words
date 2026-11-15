// Package auth provides password hashing, token generation, and login
// rate limiting.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

// HashPassword returns a bcrypt hash with cost 12.
func HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// CheckPassword compares a plaintext password against a bcrypt hash in
// constant time.
func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// NewPrefixedToken returns a fresh n-byte token with prefix prepended, along
// with the storage hash of the full raw token.
func NewPrefixedToken(prefix string, n int) (raw, hash string, err error) {
	raw, err = randomString(n)
	if err != nil {
		return "", "", err
	}
	raw = prefix + raw
	return raw, HashToken(raw), nil
}

// NewToken returns a fresh 32-byte session token and its storage hash.
func NewToken() (raw, hash string, err error) {
	raw, err = randomString(32)
	if err != nil {
		return "", "", err
	}
	return raw, HashToken(raw), nil
}

// HashToken returns the hex SHA-256 of a token, which is what the database
// stores.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// NewCSRFToken returns a fresh CSRF token.
func NewCSRFToken() (string, error) { return randomString(32) }

// SecureEqual compares two strings without leaking timing information.
func SecureEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// ValidUsername accepts 1–32 runes after trimming, with no control characters.
func ValidUsername(username string) bool {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > 32 {
		return false
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// ValidPassword accepts 6–72 bytes, the range bcrypt handles.
func ValidPassword(pw string) bool {
	return len(pw) >= 6 && len(pw) <= 72
}

// LoginLimiter is a sliding-window failure counter keyed by an arbitrary
// string such as a client IP.
type LoginLimiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu       sync.Mutex
	failures map[string][]time.Time
}

// NewLoginLimiter allows limit failures per window.
func NewLoginLimiter(limit int, window time.Duration, now func() time.Time) *LoginLimiter {
	return &LoginLimiter{
		limit:    limit,
		window:   window,
		now:      now,
		failures: make(map[string][]time.Time),
	}
}

// prune drops failures that fall outside the window. Caller holds mu.
func (l *LoginLimiter) prune(key string) []time.Time {
	cutoff := l.now().Add(-l.window)
	kept := l.failures[key][:0]
	for _, t := range l.failures[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.failures, key)
		return nil
	}
	l.failures[key] = kept
	return kept
}

// Allow reports whether another attempt for key is permitted.
func (l *LoginLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key)) < l.limit
}

// Fail records a failed attempt for key.
func (l *LoginLimiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures[key] = append(l.prune(key), l.now())
}

// Reset clears failures for key after a successful login.
func (l *LoginLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

// ErrRateLimited is returned by callers when the limiter blocks an attempt.
var ErrRateLimited = errors.New("too many attempts")
