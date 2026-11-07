package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// getEnvWithFallback checks the provided key, and if unset, checks its OpsPilot/Ongrid counterpart.
func getEnvWithFallback(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if strings.HasPrefix(key, "OPSPILOT_") {
		return os.Getenv("ONGRID_" + strings.TrimPrefix(key, "OPSPILOT_"))
	}
	if strings.HasPrefix(key, "ONGRID_") {
		return os.Getenv("OPSPILOT_" + strings.TrimPrefix(key, "ONGRID_"))
	}
	return ""
}

// EnvDuration reads a positive duration, or returns fallback.
func EnvDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(getEnvWithFallback(key))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

// EnvInt reads a positive integer, or returns fallback.
func EnvInt(key string, fallback int) int {
	raw := strings.TrimSpace(getEnvWithFallback(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// EnvBool accepts the Edge boolean aliases, or returns fallback.
func EnvBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(getEnvWithFallback(key)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "t", "yes", "y", "on", "enabled":
		return true
	case "0", "false", "f", "no", "n", "off", "disabled":
		return false
	default:
		return fallback
	}
}

// EnvOr reads an env var, returning def when unset/empty.
func EnvOr(key, def string) string {
	if v := getEnvWithFallback(key); v != "" {
		return v
	}
	return def
}
