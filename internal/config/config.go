// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"strconv"
	"time"
)

// Config holds every runtime setting the server needs.
type Config struct {
	Port         string
	DataDir      string
	DictPath     string
	Location     *time.Location
	CookieSecure bool
	AIBaseURL    string
	AIAPIKey     string
	AIModel      string
}

// Load reads configuration through getenv, applying defaults. An invalid TZ
// or COOKIE_SECURE value is a startup error rather than a silent fallback.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Port:      valueOr(getenv, "PORT", "8080"),
		DataDir:   valueOr(getenv, "DATA_DIR", "./data"),
		DictPath:  valueOr(getenv, "DICT_PATH", "./data/dict.db"),
		AIBaseURL: getenv("AI_BASE_URL"),
		AIAPIKey:  getenv("AI_API_KEY"),
		AIModel:   getenv("AI_MODEL"),
	}

	tz := valueOr(getenv, "TZ", "Asia/Shanghai")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Config{}, fmt.Errorf("invalid TZ: %w", err)
	}
	cfg.Location = loc

	if raw := getenv("COOKIE_SECURE"); raw != "" {
		secure, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid COOKIE_SECURE: %w", err)
		}
		cfg.CookieSecure = secure
	}

	return cfg, nil
}

func valueOr(getenv func(string) string, key, fallback string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return fallback
}
