package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "8080" || cfg.DataDir != "./data" || cfg.DictPath != "./data/dict.db" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.Location.String() != "Asia/Shanghai" {
		t.Fatalf("tz = %s", cfg.Location)
	}
	if cfg.CookieSecure {
		t.Fatal("cookie secure should default false")
	}
	if cfg.AIBaseURL != "" || cfg.AIAPIKey != "" || cfg.AIModel != "" {
		t.Fatalf("ai should default empty: %+v", cfg)
	}
}

func TestLoadRejectsBadTimezone(t *testing.T) {
	if _, err := Load(func(k string) string {
		if k == "TZ" {
			return "Mars/Base"
		}
		return ""
	}); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadReadsEnv(t *testing.T) {
	env := map[string]string{
		"PORT":          "9090",
		"DATA_DIR":      "/data",
		"DICT_PATH":     "/app/dict.db",
		"TZ":            "UTC",
		"COOKIE_SECURE": "true",
		"AI_BASE_URL":   "https://api.example.com/v1",
		"AI_API_KEY":    "secret",
		"AI_MODEL":      "model-x",
	}
	cfg, err := Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "9090" || cfg.DataDir != "/data" || cfg.DictPath != "/app/dict.db" {
		t.Fatalf("unexpected values: %+v", cfg)
	}
	if cfg.Location.String() != "UTC" || !cfg.CookieSecure {
		t.Fatalf("unexpected location/cookie: %+v", cfg)
	}
	if cfg.AIBaseURL != "https://api.example.com/v1" || cfg.AIAPIKey != "secret" || cfg.AIModel != "model-x" {
		t.Fatalf("unexpected ai: %+v", cfg)
	}
}

func TestLoadRejectsBadCookieSecure(t *testing.T) {
	if _, err := Load(func(k string) string {
		if k == "COOKIE_SECURE" {
			return "sometimes"
		}
		return ""
	}); err == nil {
		t.Fatal("expected error")
	}
}
