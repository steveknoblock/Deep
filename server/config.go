package main

import (
	"log"
	"os"
)

// Config holds Deep's server configuration, loaded from environment
// variables. Deep has no data of its own to configure — everything it
// needs beyond its own listen port and UI path is Hatcheck's location.
type Config struct {
	Port        string // DEEP_PORT     (default: 8091)
	UIPath      string // DEEP_UI       (default: ./ui)
	HatcheckURL string // HATCHECK_URL  (default: http://localhost:8090)
}

// LoadConfig reads configuration from environment variables and logs the
// resolved values at startup.
func LoadConfig() Config {
	cfg := Config{
		Port:        envOr("DEEP_PORT", "8091"),
		UIPath:      envOr("DEEP_UI", "./ui"),
		HatcheckURL: envOr("HATCHECK_URL", "http://localhost:8090"),
	}

	log.Printf("config: port=%s ui=%s hatcheck=%s", cfg.Port, cfg.UIPath, cfg.HatcheckURL)

	return cfg
}

// envOr returns the value of the named environment variable, or the
// default if the variable is unset or empty.
func envOr(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
