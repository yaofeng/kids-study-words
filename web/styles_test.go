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

func TestIconsAreInlineSVG(t *testing.T) {
	partials, err := os.ReadFile("templates/partials.html")
	if err != nil {
		t.Fatalf("read partials.html: %v", err)
	}
	notebooks, err := os.ReadFile("templates/notebooks.html")
	if err != nil {
		t.Fatalf("read notebooks.html: %v", err)
	}

	for _, emoji := range []string{"🔎", "📒", "🃏", "📅", "⚙️", "⏱"} {
		if strings.Contains(string(partials), emoji) {
			t.Errorf("partials.html still uses the emoji %q; icons must be inline SVG", emoji)
		}
	}
	if strings.Contains(string(notebooks), "⚙️") {
		t.Error("notebooks.html still uses the gear emoji; use the inline SVG icon")
	}

	for _, name := range []string{
		"icon_search", "icon_book", "icon_cards", "icon_calendar", "icon_settings", "icon_timer",
	} {
		if !strings.Contains(string(partials), `{{define "`+name+`"}}`) {
			t.Errorf("partials.html is missing the %s SVG template", name)
		}
	}

	if got := strings.Count(string(partials), `class="nav-icon"`); got != 5 {
		t.Errorf("nav should use 5 inline SVG icons, got %d", got)
	}
	if !strings.Contains(string(notebooks), `{{template "icon_settings"}}`) {
		t.Error("the notebook settings button should use the inline SVG icon")
	}
}

func TestNavHitAreasAndSafeArea(t *testing.T) {
	data, err := os.ReadFile("static/input.css")
	if err != nil {
		t.Fatalf("read input.css: %v", err)
	}
	css := string(data)

	for _, want := range []string{
		"env(safe-area-inset-bottom)",
		"pointer: coarse",
		"min-height: 44px",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("input.css is missing %q", want)
		}
	}

	if !strings.Contains(ruleBlock(css, ".app-nav"), "backdrop-filter") {
		t.Error(".app-nav should use backdrop-filter")
	}
}
