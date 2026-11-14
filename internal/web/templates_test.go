package web

import (
	"strings"
	"testing"
)

func TestStarsLevels(t *testing.T) {
	if got := Stars(1); got[0].Fill != "half" || got[1].Fill != "empty" {
		t.Fatalf("%+v", got)
	}
	if got := Stars(5); got[0].Fill != "full" || got[1].Fill != "full" || got[2].Fill != "half" || got[3].Fill != "empty" {
		t.Fatalf("%+v", got)
	}
	if got := Stars(10); got[4].Fill != "full" {
		t.Fatalf("%+v", got)
	}
	if StarLabel(5) != "熟练度 2.5 / 5" {
		t.Fatalf("label=%s", StarLabel(5))
	}
	if StarLabel(1) != "熟练度 0.5 / 5" || StarLabel(4) != "熟练度 2.0 / 5" {
		t.Fatalf("labels=%s %s", StarLabel(1), StarLabel(4))
	}
	if got := Stars(5); len(got) != 5 {
		t.Fatalf("expected 5 stars, got %d", len(got))
	}
}

func TestStarsClampOutOfRange(t *testing.T) {
	if got := Stars(0); got[0].Fill != "empty" {
		t.Fatalf("%+v", got)
	}
	if got := Stars(99); got[4].Fill != "full" {
		t.Fatalf("%+v", got)
	}
}

func TestLayoutVersionsStaticAssets(t *testing.T) {
	tc := newAuthedTestClient(t)
	body := tc.get("/lookup").Body.String()
	for _, asset := range []string{"app.css?v=", "app.js?v=", "htmx.min.js?v="} {
		if !strings.Contains(body, asset) {
			t.Fatalf("asset %q should carry a content version: %s", asset, body)
		}
	}
}
