package main

import (
	"embed"
	"encoding/base64"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/api"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/broadcast"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/scheduler"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/wa"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

//go:embed web/*
var webFS embed.FS

func main() {
	cfg := config.Load()

	st, err := store.Open(cfg.DashboardDB)
	if err != nil {
		log.Fatalf("[main] open store: %v", err)
	}
	defer st.Close()

	wac := wa.NewClient(cfg.WhatsAppBaseURL, cfg.WhatsAppUser, cfg.WhatsAppPassword)
	sched := scheduler.New(st, wac)
	if err := sched.Start(); err != nil {
		log.Fatalf("[main] start scheduler: %v", err)
	}

	bcaster := broadcast.New(st, wac)
	// Broadcast yg statusnya "running" di DB itu pasti dari proses sebelumnya
	// yg crash/restart (worker state in-memory hilang). Tandai cancelled
	// supaya UI tidak misleading. User bisa create broadcast baru kalau perlu.
	if err := bcaster.Resume(); err != nil {
		log.Printf("[main] resume broadcasts: %v", err)
	}

	// Cleanup ticker — hapus log lama supaya dashboard.db tidak terus membesar.
	// Aman di-skip kalau LogRetentionDays <= 0 (user matiin fitur).
	if cfg.LogRetentionDays > 0 {
		go startCleanupLoop(st, cfg.LogRetentionDays, cfg.CleanupIntervalHours)
		log.Printf("[main] log retention enabled: %d days, cleanup every %d hour(s)", cfg.LogRetentionDays, cfg.CleanupIntervalHours)
	} else {
		log.Printf("[main] log retention disabled (DASHBOARD_LOG_RETENTION_DAYS=0)")
	}

	app := fiber.New(fiber.Config{
		AppName:               "WhatsApp Dashboard",
		DisableStartupMessage: false,
	})
	app.Use(recover.New())
	app.Use(logger.New())
	app.Use(cors.New())

	// Optional dashboard basic auth (separate from upstream WA basic auth)
	if cfg.DashboardBasicAuth != "" {
		parts := strings.SplitN(cfg.DashboardBasicAuth, ":", 2)
		if len(parts) == 2 {
			user, pass := parts[0], parts[1]
			app.Use(func(c *fiber.Ctx) error {
				u, p, ok := parseBasic(c.Get("Authorization"))
				if !ok || u != user || p != pass {
					c.Set("WWW-Authenticate", `Basic realm="dashboard"`)
					return c.Status(401).SendString("unauthorized")
				}
				return c.Next()
			})
		}
	}

	// API
	h := &api.Handlers{
		Store:            st,
		WA:               wac,
		Scheduler:        sched,
		Broadcaster:      bcaster,
		DefaultTZ:        cfg.DefaultTimezone,
		LogRetentionDays: cfg.LogRetentionDays,
	}
	h.Register(app)

	// Static UI -- the HTML is embedded in the binary via go:embed.
	// We force no-cache on every response from this handler so that after
	// rebuilding the image, browsers always pick up the new JS/HTML
	// immediately (no more stale-cache "404 saat tambah device" pitfall).
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("[main] embed web subtree: %v", err)
	}
	app.Use("/", func(c *fiber.Ctx) error {
		c.Set("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Set("Pragma", "no-cache")
		c.Set("Expires", "0")
		return c.Next()
	}, filesystem.New(filesystem.Config{
		Root:         http.FS(sub),
		Index:        "index.html",
		NotFoundFile: "index.html",
	}))

	addr := cfg.DashboardHost + ":" + cfg.DashboardPort
	log.Printf("[main] dashboard listening on http://%s", addr)
	log.Printf("[main] proxying to WhatsApp API at %s", cfg.WhatsAppBaseURL)
	if err := app.Listen(addr); err != nil {
		log.Fatalf("[main] listen: %v", err)
	}
}

// startCleanupLoop runs di goroutine: jalankan cleanup sekali setelah 1
// menit (supaya tidak block startup) lalu setiap intervalHours.
func startCleanupLoop(st *store.Store, retentionDays, intervalHours int) {
	time.Sleep(1 * time.Minute)
	for {
		runCleanupOnce(st, retentionDays)
		time.Sleep(time.Duration(intervalHours) * time.Hour)
	}
}

func runCleanupOnce(st *store.Store, retentionDays int) {
	stats, err := st.CleanupOldLogs(retentionDays)
	if err != nil {
		log.Printf("[cleanup] error: %v", err)
		return
	}
	total := stats.DeletedScheduleLogs + stats.DeletedBroadcasts + stats.DeletedBroadcastRecipients
	if total == 0 {
		log.Printf("[cleanup] nothing to delete (cutoff: %s)", stats.CutoffUTC.Format(time.RFC3339))
		return
	}
	log.Printf("[cleanup] deleted %d schedule_logs + %d broadcasts + %d broadcast_recipients (cutoff: %s)",
		stats.DeletedScheduleLogs, stats.DeletedBroadcasts, stats.DeletedBroadcastRecipients,
		stats.CutoffUTC.Format(time.RFC3339))
	// Reclaim disk space lewat VACUUM kalau penghapusan signifikan
	if total > 500 {
		if err := st.VacuumIfNeeded(); err != nil {
			log.Printf("[cleanup] vacuum failed (non-fatal): %v", err)
		} else {
			log.Printf("[cleanup] vacuum done")
		}
	}
}

func parseBasic(h string) (string, string, bool) {
	const prefix = "Basic "
	if !strings.HasPrefix(h, prefix) {
		return "", "", false
	}
	enc := strings.TrimPrefix(h, prefix)
	dec, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(dec), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
