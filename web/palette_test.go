package assets

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var colorEntry = regexp.MustCompile(`['"]?([a-z][a-z-]*)['"]?\s*:\s*['"](#[0-9A-Fa-f]{6})['"]`)

func loadPalette(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile("../tailwind.config.js")
	if err != nil {
		t.Fatalf("read tailwind config: %v", err)
	}
	palette := map[string]string{}
	for _, match := range colorEntry.FindAllStringSubmatch(string(data), -1) {
		palette[match[1]] = match[2]
	}
	return palette
}

func linearChannel(value float64) float64 {
	if value <= 0.04045 {
		return value / 12.92
	}
	return math.Pow((value+0.055)/1.055, 2.4)
}

func relativeLuminance(hex string) float64 {
	hex = strings.TrimPrefix(hex, "#")
	red, _ := strconv.ParseUint(hex[0:2], 16, 8)
	green, _ := strconv.ParseUint(hex[2:4], 16, 8)
	blue, _ := strconv.ParseUint(hex[4:6], 16, 8)
	return 0.2126*linearChannel(float64(red)/255) +
		0.7152*linearChannel(float64(green)/255) +
		0.0722*linearChannel(float64(blue)/255)
}

func contrastRatio(foreground, background string) float64 {
	fg, bg := relativeLuminance(foreground), relativeLuminance(background)
	if fg < bg {
		fg, bg = bg, fg
	}
	return (fg + 0.05) / (bg + 0.05)
}

func TestPaletteContrast(t *testing.T) {
	palette := loadPalette(t)
	required := []string{
		"sky", "paper", "ink", "muted", "blue", "blue-deep", "blue-text",
		"blue-soft", "sun", "mint", "coral", "coral-dark", "lilac",
	}
	for _, name := range required {
		if _, ok := palette[name]; !ok {
			t.Fatalf("missing %q in the tailwind color palette", name)
		}
	}

	pairs := []struct {
		foreground string
		background string
		minimum    float64
	}{
		{"ink", "sky", 4.5},
		{"muted", "sky", 4.5},
		{"paper", "blue-deep", 4.5},
		{"blue-text", "blue-soft", 4.5},
		{"coral-dark", "paper", 4.5},
		{"ink", "coral", 4.5},
	}
	for _, pair := range pairs {
		got := contrastRatio(palette[pair.foreground], palette[pair.background])
		if got < pair.minimum {
			t.Errorf("%s on %s is %.2f:1, want at least %.1f:1", pair.foreground, pair.background, got, pair.minimum)
		}
	}
}
