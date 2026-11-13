package ai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAIClientParsesExamples(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		io.WriteString(w, `{"choices":[{"message":{"content":"[{\"en\":\"I go home.\",\"zh\":\"我回家。\"}]"}}]}`)
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "key", "test-model", 2*time.Second)
	ex, err := c.Examples(context.Background(), "go", "去", []string{"go", "went", "gone"})
	if err != nil || len(ex) != 1 || ex[0].ZH != "我回家。" || ex[0].EN != "I go home." {
		t.Fatalf("%+v %v", ex, err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path=%q", gotPath)
	}
	if gotAuth != "Bearer key" {
		t.Fatalf("auth=%q", gotAuth)
	}
}

func TestAIClientRejectsSentenceWithoutWordForm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"[{\"en\":\"I like cats.\",\"zh\":\"我喜欢猫。\"}]"}}]}`)
	}))
	defer srv.Close()
	c := New(srv.URL, "key", "m", time.Second)
	if _, err := c.Examples(context.Background(), "go", "去", []string{"go"}); err == nil {
		t.Fatal("expected error for sentence without the word")
	}
}

func TestAIClientDisabledWithoutConfiguration(t *testing.T) {
	cases := []*Client{
		New("", "key", "m", time.Second),
		New("http://example.com", "", "m", time.Second),
		New("http://example.com", "key", "", time.Second),
	}
	for i, c := range cases {
		if c.Enabled() {
			t.Errorf("case %d should be disabled", i)
		}
	}
	if New("http://localhost:9999/v1", "key", "m", time.Second).Host() != "localhost:9999" {
		t.Errorf("host=%q", New("http://localhost:9999/v1", "key", "m", time.Second).Host())
	}
}

func TestAIClientRejectsMalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[]}`)
	}))
	defer srv.Close()
	c := New(srv.URL, "key", "m", time.Second)
	if _, err := c.Examples(context.Background(), "go", "去", []string{"go"}); err == nil {
		t.Fatal("expected error for malformed response")
	}
}

func TestAIClientErrorDoesNotLeakKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "nope")
	}))
	defer srv.Close()
	c := New(srv.URL, "sk-secret-value", "m", time.Second)
	_, err := c.Examples(context.Background(), "go", "去", []string{"go"})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "sk-secret-value") {
		t.Fatalf("error leaks key: %v", err)
	}
}
