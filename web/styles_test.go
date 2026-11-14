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

func TestNegativeButtonsAreCoralGlass(t *testing.T) {
	data, err := os.ReadFile("static/input.css")
	if err != nil {
		t.Fatalf("read input.css: %v", err)
	}
	css := string(data)

	for _, selector := range []string{".btn-danger", ".btn-unknown"} {
		block := ruleBlock(css, selector)
		if block == "" {
			t.Errorf("input.css is missing the %s rule", selector)
			continue
		}
		if !strings.Contains(block, "rgba(255, 138, 115") {
			t.Errorf("%s should use a translucent coral fill, not a solid slab", selector)
		}
		if !strings.Contains(block, "#A93A22") {
			t.Errorf("%s should use coral-dark text for contrast", selector)
		}
		if strings.Contains(block, "background: #FF8A73") {
			t.Errorf("%s still uses the solid coral background", selector)
		}
	}
}

func TestStarsUseFlatGlassColors(t *testing.T) {
	data, err := os.ReadFile("static/input.css")
	if err != nil {
		t.Fatalf("read input.css: %v", err)
	}
	css := string(data)

	for _, selector := range []string{".star-full::before", ".star-empty::before", ".star-half::before"} {
		block := ruleBlock(css, selector)
		if block == "" {
			t.Errorf("input.css is missing the %s rule", selector)
			continue
		}
		if strings.Contains(block, "-webkit-text-stroke") {
			t.Errorf("%s still carries the old ink sticker outline", selector)
		}
	}
	if !strings.Contains(ruleBlock(css, ".star-empty::before"), "#a9dcff") {
		t.Error("empty stars should use the soft blue token, not the old gray")
	}
	if !strings.Contains(ruleBlock(css, ".star-half::before"), "#a9dcff") {
		t.Error("half stars should use the soft blue token, not the old gray")
	}
}

func TestViewportCoversSafeArea(t *testing.T) {
	layout, err := os.ReadFile("templates/layout.html")
	if err != nil {
		t.Fatalf("read layout.html: %v", err)
	}
	if !strings.Contains(string(layout), "viewport-fit=cover") {
		t.Error("layout.html must opt into the safe-area insets with viewport-fit=cover")
	}

	data, err := os.ReadFile("static/input.css")
	if err != nil {
		t.Fatalf("read input.css: %v", err)
	}
	main := ruleBlock(string(data), ".app-main")
	for _, want := range []string{
		"safe-area-inset-top",
		"safe-area-inset-left",
		"safe-area-inset-right",
		"safe-area-inset-bottom",
	} {
		if !strings.Contains(main, want) {
			t.Errorf(".app-main should pad for %s", want)
		}
	}
}

func TestButtonPressUsesScale(t *testing.T) {
	data, err := os.ReadFile("static/input.css")
	if err != nil {
		t.Fatalf("read input.css: %v", err)
	}
	css := string(data)

	active := ruleBlock(css, ".btn:active")
	if !strings.Contains(active, "scale(.98)") {
		t.Error(".btn:active should use a slight scale, per the glass spec")
	}
	if strings.Contains(active, "translate") {
		t.Error(".btn:active still uses the old sticker displacement")
	}

	marker := "@media (prefers-reduced-motion: reduce)"
	idx := strings.Index(css, marker)
	if idx < 0 {
		t.Fatal("input.css is missing the reduced-motion guard")
	}
	guard := css[idx:]
	if len(guard) > 240 {
		guard = guard[:240]
	}
	if !strings.Contains(guard, "transform: none") {
		t.Error("reduced motion should cancel the button press transform")
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
