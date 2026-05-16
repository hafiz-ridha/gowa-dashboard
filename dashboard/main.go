package main

import (
	"embed"
	"encoding/base64"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/api"
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
	h := &api.Handlers{Store: st, WA: wac, Scheduler: sched, DefaultTZ: cfg.DefaultTimezone}
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
