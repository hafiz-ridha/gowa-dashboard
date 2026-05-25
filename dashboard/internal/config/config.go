package config

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	// Dashboard server
	DashboardHost string
	DashboardPort string
	DashboardDB   string

	// Upstream WhatsApp API (the core go-whatsapp-web-multidevice REST server)
	WhatsAppBaseURL  string
	WhatsAppUser     string
	WhatsAppPassword string

	// Default timezone for new schedules (e.g. "Asia/Jakarta", "UTC", "Local")
	DefaultTimezone string

	// Dashboard basic auth (optional). Format: "user:pass". If empty, dashboard is open.
	DashboardBasicAuth string

	// Auto-cleanup retention (days). Logs older than this akan dihapus
	// otomatis tiap 6 jam. 0 = disabled. Default 30.
	LogRetentionDays int

	// Cleanup interval in hours. Default 6 jam. Min 1.
	CleanupIntervalHours int
}

func Load() *Config {
	// Load .env if present. Absent .env is fine — Docker/compose typically pass
	// vars directly via environment, so we only log when one was actually loaded.
	if err := godotenv.Load(); err == nil {
		log.Printf("[config] loaded .env from working directory")
	} else {
		log.Printf("[config] no .env file found, using environment variables")
	}

	cfg := &Config{
		DashboardHost:        getenv("DASHBOARD_HOST", "0.0.0.0"),
		DashboardPort:        getenv("DASHBOARD_PORT", "8088"),
		DashboardDB:          getenv("DASHBOARD_DB", "dashboard.db"),
		WhatsAppBaseURL:      strings.TrimRight(getenv("WHATSAPP_API_URL", "http://localhost:3000"), "/"),
		WhatsAppUser:         os.Getenv("WHATSAPP_API_USER"),
		WhatsAppPassword:     os.Getenv("WHATSAPP_API_PASSWORD"),
		DefaultTimezone:      getenv("DASHBOARD_TZ", "Local"),
		DashboardBasicAuth:   os.Getenv("DASHBOARD_BASIC_AUTH"),
		LogRetentionDays:     getenvInt("DASHBOARD_LOG_RETENTION_DAYS", 30),
		CleanupIntervalHours: getenvInt("DASHBOARD_CLEANUP_INTERVAL_HOURS", 6),
	}
	if cfg.CleanupIntervalHours < 1 {
		cfg.CleanupIntervalHours = 1
	}
	return cfg
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
