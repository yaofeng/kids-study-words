package assets

import (
	"os"
	"strings"
	"testing"
)

func ruleBlock(css, selector string) string {
	marker := selector + " {"
	start := strings.Index(css, marker)
	if start < 0 {
		return ""
	}
	start += len(marker)
	end := strings.Index(css[start:], "}")
	if end < 0 {
		return ""
	}
	return css[start : start+end]
}

func TestGlassContract(t *testing.T) {
	data, err := os.ReadFile("static/input.css")
	if err != nil {
		t.Fatalf("read input.css: %v", err)
	}
	css := string(data)

	for _, want := range []string{
		"backdrop-filter",
		"@supports not",
		"prefers-reduced-transparency",
		"prefers-reduced-motion",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("input.css is missing %q", want)
		}
	}

	for _, selector := range []string{".card", ".record-row"} {
		if strings.Contains(ruleBlock(css, selector), "backdrop-filter") {
			t.Errorf("%s must stay blur-free for long-list performance", selector)
		}
	}

	for _, selector := range []string{".app-nav", ".input", ".auth-card"} {
		if !strings.Contains(ruleBlock(css, selector), "backdrop-filter") {
			t.Errorf("%s should use backdrop-filter", selector)
		}
	}
}
