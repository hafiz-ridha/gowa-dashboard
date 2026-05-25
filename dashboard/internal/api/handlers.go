package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/broadcast"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/scheduler"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/wa"

	"github.com/gofiber/fiber/v2"
)

type Handlers struct {
	Store            *store.Store
	WA               *wa.Client
	Scheduler        *scheduler.Scheduler
	Broadcaster      *broadcast.Broadcaster
	DefaultTZ        string
	LogRetentionDays int
}

func (h *Handlers) Register(app *fiber.App) {
	g := app.Group("/api")
	g.Get("/_health", h.health)                  // static version probe
	g.Get("/_health/upstream", h.healthUpstream) // live ping ke core utk badge "API Core Connected"
	g.Get("/_stats", h.stats)                    // DB row counts + retention config
	g.Post("/_cleanup", h.cleanupNow)            // manual trigger cleanup (idempotent)
	g.Get("/devices", h.listDevices)
	g.Post("/devices", h.createDevice)
	g.Delete("/devices/:id", h.deleteDevice)
	g.Get("/devices/:id/status", h.deviceStatus)
	g.Get("/devices/:id/login", h.deviceLogin)
	g.Get("/devices/:id/login-code", h.deviceLoginCode)
	g.Post("/devices/:id/logout", h.deviceLogout)
	g.Post("/devices/:id/reconnect", h.deviceReconnect)
	g.Get("/qr/:filename", h.qrImage)

	g.Post("/send", h.sendNow)

	g.Get("/schedules", h.listSchedules)
	g.Post("/schedules", h.createSchedule)
	// PENTING: route static (preview, export.xlsx, import) WAJIB dideklarasi
	// SEBELUM route parametric "/schedules/:id". Fiber router pada beberapa
	// build matching greedy — "export.xlsx" bisa dianggap sebagai value :id
	// dan masuk ke getSchedule (yang akan return 400 "invalid id") atau 404
	// kalau handler-nya filter strict. Urutan ini menjamin tidak shadowing.
	g.Post("/schedules/preview", h.previewSchedule)
	g.Get("/schedules/export.xlsx", h.exportSchedulesXLSX)
	g.Post("/schedules/import", h.importSchedulesXLSX)
	g.Get("/schedules/:id", h.getSchedule)
	g.Put("/schedules/:id", h.updateSchedule)
	g.Delete("/schedules/:id", h.deleteSchedule)
	g.Post("/schedules/:id/toggle", h.toggleSchedule)
	g.Post("/schedules/:id/run", h.runSchedule)
	g.Get("/schedules/:id/logs", h.scheduleLogs)
	g.Get("/logs", h.recentLogs)

	// Broadcast: kirim ke banyak nomor dgn random delay & anti-spam.
	g.Get("/broadcast", h.listBroadcasts)
	g.Post("/broadcast", h.createBroadcast)
	g.Post("/broadcast/preview", h.previewBroadcastRecipients)
	g.Get("/broadcast/:id", h.getBroadcast)
	g.Get("/broadcast/:id/recipients", h.getBroadcastRecipients)
	g.Post("/broadcast/:id/cancel", h.cancelBroadcast)
	g.Delete("/broadcast/:id", h.deleteBroadcast)

	// AI Reply: thin proxy to core /aireply/*. All endpoints require
	// X-Device-Id header (core resolves it to a JID and scopes config /
	// knowledgebase / chat-settings / logs per device).
	g.Get("/aireply/config", h.aiGetConfig)
	g.Put("/aireply/config", h.aiSaveConfig)
	g.Post("/aireply/config/test", h.aiTestConfig)
	g.Post("/aireply/documents", h.aiUploadDocument)
	g.Get("/aireply/documents", h.aiListDocuments)
	g.Delete("/aireply/documents/:id", h.aiDeleteDocument)
	g.Post("/aireply/documents/reindex", h.aiReindexDocuments)
	g.Get("/aireply/chat-settings", h.aiListChatSettings)
	g.Put("/aireply/chat-settings/:chat_jid", h.aiSetChatEnabled)
	g.Get("/aireply/logs", h.aiListLogs)

	// "Apply to all devices" — fan-out single config / chat toggle ke
	// SEMUA device yang sedang logged_in. Dashboard handler iterate
	// device list dari core lalu kirim PUT per-device. Hasilnya array
	// per-device {device_id, ok, error} sehingga UI bisa report partial
	// success (mis. 3 of 4 berhasil).
	g.Post("/aireply/config/apply-to-all", h.aiApplyConfigToAll)
	g.Post("/aireply/chat-settings/:chat_jid/apply-to-all", h.aiApplyChatToAll)

	// Multi-device health check — return audit per-device:
	// has_config, has_api_key, chat_toggle_count, recent_log_status.
	// User bisa pakai utk verify "kenapa device 2 tidak balas" tanpa
	// trial-and-error. Lihat handler-nya untuk detail field response.
	g.Get("/aireply/multi-device-health", h.aiMultiDeviceHealth)

	// Global pause untuk AI Reply (semua device + semua chat). State in-memory
	// di core, reset on container restart. Body POST pause: {"minutes": N}
	// dengan N <= 0 = indefinite.
	g.Post("/aireply/pause", h.aiPause)
	g.Post("/aireply/resume", h.aiResume)
	g.Get("/aireply/pause-status", h.aiPauseStatus)
}

// --- proxies --------------------------------------------------------------

// healthUpstream — live ping ke core. Frontend pakai utk badge status:
//
//   - 200 + {"ok":true}  -> "API Core Connected" (hijau)
//   - 200 + {"ok":false} -> "API Core Disconnected" (merah) dengan error message
//
// Selalu return HTTP 200 supaya browser tidak treat sebagai network error;
// status hidup/mati ada di body "ok" flag.
func (h *Handlers) healthUpstream(c *fiber.Ctx) error {
	start := time.Now()
	// Pakai endpoint paling murah di core. /app/devices butuh X-Device-Id
	// kalau ada >=2 device — tapi 400 dari core tetap berarti core hidup.
	// Forward whatever device id browser kasih kalau ada.
	deviceID := strings.TrimSpace(c.Get("X-Device-Id"))
	if deviceID == "" {
		deviceID = strings.TrimSpace(c.Query("device_id"))
	}
	_, err := h.WA.ListDevices(deviceID)
	latency := time.Since(start).Milliseconds()

	if err == nil {
		return c.JSON(fiber.Map{
			"ok":           true,
			"upstream_url": h.WA.BaseURL,
			"latency_ms":   latency,
			"checked_at":   time.Now().UTC().Format(time.RFC3339),
		})
	}
	// Core "400 missing device" is still "alive" - dashboard tetap connected,
	// cuma butuh device_id. Treat sebagai connected.
	msg := err.Error()
	if strings.Contains(msg, "-> 400:") || strings.Contains(msg, "-> 401:") {
		return c.JSON(fiber.Map{
			"ok":           true,
			"upstream_url": h.WA.BaseURL,
			"latency_ms":   latency,
			"checked_at":   time.Now().UTC().Format(time.RFC3339),
			"note":         "core alive (returned 4xx — login/device id required)",
		})
	}
	return c.JSON(fiber.Map{
		"ok":           false,
		"upstream_url": h.WA.BaseURL,
		"latency_ms":   latency,
		"checked_at":   time.Now().UTC().Format(time.RFC3339),
		"error":        msg,
	})
}

// stats — info untuk admin: jumlah row per tabel + retention config.
func (h *Handlers) stats(c *fiber.Ctx) error {
	counts, err := h.Store.CountRows()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"row_counts":             counts,
		"log_retention_days":     h.LogRetentionDays,
		"auto_cleanup_enabled":   h.LogRetentionDays > 0,
		"server_time":            time.Now().UTC().Format(time.RFC3339),
	})
}

// cleanupNow — trigger manual cleanup. Pakai retention dari config tapi
// user bisa override via query ?days=N kalau perlu pembersihan agresif.
func (h *Handlers) cleanupNow(c *fiber.Ctx) error {
	days := h.LogRetentionDays
	if q := strings.TrimSpace(c.Query("days")); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			days = n
		}
	}
	if days <= 0 {
		return c.Status(400).JSON(fiber.Map{"error": "retention not configured. Set DASHBOARD_LOG_RETENTION_DAYS or pass ?days=N"})
	}
	stats, err := h.Store.CleanupOldLogs(days)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	// Cuma run VACUUM kalau penghapusan signifikan, supaya request cepat.
	total := stats.DeletedScheduleLogs + stats.DeletedBroadcasts + stats.DeletedBroadcastRecipients
	if total > 500 {
		_ = h.Store.VacuumIfNeeded()
	}
	return c.JSON(fiber.Map{
		"results":          stats,
		"retention_days":   days,
		"vacuum_performed": total > 500,
	})
}

// health returns a small JSON probe listing the dashboard's own routes.
// Use it to verify you're talking to the rebuilt image (not a cached old one).
func (h *Handlers) health(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"ok":           true,
		"build":        "dashboard-v1.2-aireply",
		"upstream_url": h.WA.BaseURL,
		"routes": []string{
			"GET    /api/_health",
			"GET    /api/_health/upstream",
			"GET    /api/_stats",
			"POST   /api/_cleanup",
			"GET    /api/devices",
			"POST   /api/devices",
			"DELETE /api/devices/:id",
			"GET    /api/devices/:id/status",
			"GET    /api/devices/:id/login",
			"GET    /api/devices/:id/login-code",
			"POST   /api/devices/:id/logout",
			"POST   /api/devices/:id/reconnect",
			"GET    /api/qr/:filename",
			"POST   /api/send",
			"GET    /api/schedules",
			"POST   /api/schedules",
			"GET    /api/schedules/:id",
			"PUT    /api/schedules/:id",
			"DELETE /api/schedules/:id",
			"POST   /api/schedules/:id/toggle",
			"POST   /api/schedules/:id/run",
			"GET    /api/schedules/:id/logs",
			"POST   /api/schedules/preview",
			"GET    /api/schedules/export.xlsx",
			"POST   /api/schedules/import",
			"GET    /api/logs",
			"GET    /api/broadcast",
			"POST   /api/broadcast",
			"POST   /api/broadcast/preview",
			"GET    /api/broadcast/:id",
			"GET    /api/broadcast/:id/recipients",
			"POST   /api/broadcast/:id/cancel",
			"DELETE /api/broadcast/:id",
			"GET    /api/aireply/config",
			"PUT    /api/aireply/config",
			"POST   /api/aireply/config/test",
			"POST   /api/aireply/documents",
			"GET    /api/aireply/documents",
			"DELETE /api/aireply/documents/:id",
			"POST   /api/aireply/documents/reindex",
			"GET    /api/aireply/chat-settings",
			"PUT    /api/aireply/chat-settings/:chat_jid",
			"GET    /api/aireply/logs",
			"POST   /api/aireply/config/apply-to-all",
			"POST   /api/aireply/chat-settings/:chat_jid/apply-to-all",
			"GET    /api/aireply/multi-device-health",
			"POST   /api/aireply/pause",
			"POST   /api/aireply/resume",
			"GET    /api/aireply/pause-status",
		},
	})
}

func (h *Handlers) listDevices(c *fiber.Ctx) error {
	// Forward X-Device-Id (or device_id query) from the browser so core's
	// device middleware can authorise the list call. With 2+ registered
	// devices core REJECTS empty header (single-device auto-pick only).
	deviceID := strings.TrimSpace(c.Get("X-Device-Id"))
	if deviceID == "" {
		deviceID = strings.TrimSpace(c.Query("device_id"))
	}
	resp, err := h.WA.ListDevices(deviceID)
	if err != nil {
		return c.Status(502).JSON(fiber.Map{"error": err.Error()})
	}
	// Enrich tiap device dgn flag akurat dari /devices/:id/status.
	// Core's /devices list cuma kasih `state` string yang derived dari
	// whatsmeow.IsLoggedIn() — yang return true selama kredensial pair
	// tersimpan, TANPA peduli WS aktif atau tidak. Akibatnya device paired
	// tapi offline (network drop, server restart) tetap "logged_in" → UI
	// salah hijau "Connected".
	//
	// Endpoint /devices/:id/status di core return is_connected & is_logged_in
	// terpisah & akurat. Kita N+1 call di sini (paralel) supaya frontend
	// tidak perlu polling per device. Biaya: 1 extra round-trip per device,
	// dilakukan paralel — total latency = max(per-device status latency)
	// bukan sum. Untuk <20 device, masih < 500ms biasanya.
	if enriched, ok := enrichDeviceListWithStatus(h.WA, resp.Results); ok {
		resp.Results = enriched
	}
	return c.JSON(resp)
}

func (h *Handlers) deviceStatus(c *fiber.Ctx) error {
	resp, err := h.WA.DeviceStatus(c.Params("id"))
	if err != nil {
		return c.Status(502).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(resp)
}

// --- Device management proxies -------------------------------------------

type createDeviceReq struct {
	DeviceID string `json:"device_id"`
}

func (h *Handlers) createDevice(c *fiber.Ctx) error {
	var req createDeviceReq
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if strings.TrimSpace(req.DeviceID) == "" {
		return c.Status(400).JSON(fiber.Map{"error": "device_id required"})
	}
	// authDeviceID = an existing device for middleware to authorise this
	// call (mandatory when 2+ devices already exist). For first-device
	// bootstrap browser sends nothing and core's single-device fallback
	// kicks in (0 devices -> error guidance comes through upstream).
	authDeviceID := strings.TrimSpace(c.Get("X-Device-Id"))
	if authDeviceID == "" {
		authDeviceID = strings.TrimSpace(c.Query("device_id"))
	}
	resp, err := h.WA.CreateDevice(req.DeviceID, authDeviceID)
	if err != nil {
		return c.Status(502).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(resp)
}

func (h *Handlers) deleteDevice(c *fiber.Ctx) error {
	resp, err := h.WA.DeleteDevice(c.Params("id"))
	if err != nil {
		return c.Status(502).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(resp)
}

// deviceLogin starts a QR login for the device and returns the QR image URL
// REWRITTEN to point to the dashboard's own /api/qr/... proxy endpoint, so
// the browser does not need direct access to the core's port.
func (h *Handlers) deviceLogin(c *fiber.Ctx) error {
	resp, err := h.WA.Login(c.Params("id"))
	if err != nil {
		return c.Status(502).JSON(fiber.Map{"error": err.Error()})
	}
	// Rewrite qr_link inside results JSON to a dashboard-relative URL.
	rewritten, err := rewriteQRLink(resp.Results)
	if err == nil {
		resp.Results = rewritten
	}
	return c.JSON(resp)
}

func (h *Handlers) deviceLoginCode(c *fiber.Ctx) error {
	phone := c.Query("phone")
	if phone == "" {
		return c.Status(400).JSON(fiber.Map{"error": "phone query parameter required"})
	}
	resp, err := h.WA.LoginWithCode(c.Params("id"), phone)
	if err != nil {
		return c.Status(502).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(resp)
}

func (h *Handlers) deviceLogout(c *fiber.Ctx) error {
	resp, err := h.WA.Logout(c.Params("id"))
	if err != nil {
		return c.Status(502).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(resp)
}

func (h *Handlers) deviceReconnect(c *fiber.Ctx) error {
	resp, err := h.WA.Reconnect(c.Params("id"))
	if err != nil {
		return c.Status(502).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(resp)
}

// qrImage proxies QR PNG bytes from the core. The filename is whatever
// the core stamped (typically scan-qr-<UUID>.png).
func (h *Handlers) qrImage(c *fiber.Ctx) error {
	fn := c.Params("filename")
	// Defense in depth: prevent path traversal even though Fiber strips slashes.
	if strings.ContainsAny(fn, "/\\") || strings.Contains(fn, "..") {
		return c.Status(400).SendString("invalid filename")
	}
	body, ct, err := h.WA.FetchStatic("/statics/qrcode/" + fn)
	if err != nil {
		return c.Status(502).SendString(err.Error())
	}
	c.Set("Content-Type", ct)
	c.Set("Cache-Control", "no-store")
	return c.Send(body)
}

// rewriteQRLink replaces the absolute qr_link URL with a dashboard-relative
// path so the browser can fetch the QR through this dashboard (works behind
// reverse proxy / when the core is on a private network).
func rewriteQRLink(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return raw, err
	}
	if link, ok := data["qr_link"].(string); ok && link != "" {
		// Find the filename part after /statics/qrcode/
		idx := strings.LastIndex(link, "/")
		if idx >= 0 && idx < len(link)-1 {
			data["qr_link"] = "/api/qr/" + link[idx+1:]
		}
	}
	return json.Marshal(data)
}

// --- send-now -------------------------------------------------------------

type sendNowReq struct {
	DeviceID    string `json:"device_id"`
	Recipient   string `json:"recipient"`
	MessageType string `json:"message_type"`
	Message     string `json:"message"`
	MediaURL    string `json:"media_url"`
	Caption     string `json:"caption"`
	Latitude    string `json:"latitude"`
	Longitude   string `json:"longitude"`
	LinkURL     string `json:"link_url"`
}

func (h *Handlers) sendNow(c *fiber.Ctx) error {
	var req sendNowReq
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if req.Recipient == "" {
		return c.Status(400).JSON(fiber.Map{"error": "recipient required"})
	}

	switch strings.ToLower(req.MessageType) {
	case "text":
		resp, err := h.WA.SendText(req.DeviceID, wa.SendTextRequest{Phone: req.Recipient, Message: req.Message})
		return jsonOrErr(c, resp, err)
	case "image", "video", "file", "audio":
		resp, err := h.WA.SendMediaURL(req.DeviceID, req.MessageType, req.Recipient, req.MediaURL, req.Caption)
		return jsonOrErr(c, resp, err)
	case "location":
		resp, err := h.WA.SendLocation(req.DeviceID, wa.SendLocationRequest{Phone: req.Recipient, Latitude: req.Latitude, Longitude: req.Longitude})
		return jsonOrErr(c, resp, err)
	case "link":
		resp, err := h.WA.SendLink(req.DeviceID, wa.SendLinkRequest{Phone: req.Recipient, Link: req.LinkURL, Caption: req.Caption})
		return jsonOrErr(c, resp, err)
	}
	return c.Status(400).JSON(fiber.Map{"error": "unknown message_type: " + req.MessageType})
}

func jsonOrErr(c *fiber.Ctx, resp *wa.Response, err error) error {
	if err != nil {
		// upstream error - bubble up the body too if we have it
		body := fiber.Map{"error": err.Error()}
		if resp != nil {
			body["upstream"] = resp
		}
		return c.Status(502).JSON(body)
	}
	return c.JSON(resp)
}

// --- schedules ------------------------------------------------------------

func (h *Handlers) listSchedules(c *fiber.Ctx) error {
	list, err := h.Store.ListSchedules()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"results": list})
}

func (h *Handlers) getSchedule(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	sc, err := h.Store.GetSchedule(id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"results": sc})
}

type scheduleReq struct {
	Name         string `json:"name"`
	DeviceID     string `json:"device_id"`
	Recipient    string `json:"recipient"`
	MessageType  string `json:"message_type"`
	Message      string `json:"message"`
	MediaURL     string `json:"media_url"`
	Caption      string `json:"caption"`
	Latitude     string `json:"latitude"`
	Longitude    string `json:"longitude"`
	LinkURL      string `json:"link_url"`
	ScheduleType string `json:"schedule_type"`
	RunAt        string `json:"run_at"`     // ISO-8601 in target tz, e.g. "2026-05-12T08:30"
	CronExpr     string `json:"cron_expr"`  // raw cron for type=cron, OR CSV days-of-week for type=weekly
	Timezone     string `json:"timezone"`
	Enabled      *bool  `json:"enabled"`
}

func (h *Handlers) createSchedule(c *fiber.Ctx) error {
	var req scheduleReq
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	sc, err := h.buildSchedule(&req, nil)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	id, err := h.Store.CreateSchedule(sc)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	sc.ID = id
	if err := h.Scheduler.Reload(id); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "saved but failed to register: " + err.Error(), "id": id})
	}
	fresh, _ := h.Store.GetSchedule(id)
	return c.Status(201).JSON(fiber.Map{"results": fresh})
}

func (h *Handlers) updateSchedule(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	existing, err := h.Store.GetSchedule(id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}
	var req scheduleReq
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	sc, err := h.buildSchedule(&req, existing)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	sc.ID = id
	if err := h.Store.UpdateSchedule(sc); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if err := h.Scheduler.Reload(id); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "saved but failed to register: " + err.Error()})
	}
	fresh, _ := h.Store.GetSchedule(id)
	return c.JSON(fiber.Map{"results": fresh})
}

func (h *Handlers) deleteSchedule(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	h.Scheduler.Remove(id)
	if err := h.Store.DeleteSchedule(id); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"results": "deleted"})
}

func (h *Handlers) toggleSchedule(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	existing, err := h.Store.GetSchedule(id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}
	newEnabled := !existing.Enabled
	if err := h.Store.SetEnabled(id, newEnabled); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if err := h.Scheduler.Reload(id); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	fresh, _ := h.Store.GetSchedule(id)
	return c.JSON(fiber.Map{"results": fresh})
}

func (h *Handlers) runSchedule(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if _, err := h.Store.GetSchedule(id); err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}
	if err := h.Scheduler.RunNow(id); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"results": "queued"})
}

func (h *Handlers) scheduleLogs(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	logs, err := h.Store.ListLogs(id, limit)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"results": logs})
}

func (h *Handlers) recentLogs(c *fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "100"))
	logs, err := h.Store.ListRecentLogs(limit)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"results": logs})
}

func (h *Handlers) previewSchedule(c *fiber.Ctx) error {
	var req scheduleReq
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	sc, err := h.buildSchedule(&req, nil)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	count, _ := strconv.Atoi(c.Query("count", "5"))
	if count <= 0 || count > 20 {
		count = 5
	}
	times, err := scheduler.PreviewNext(sc, count)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	// Format in the target tz for display
	loc, _ := time.LoadLocation(sc.Timezone)
	if loc == nil {
		loc = time.Local
	}
	out := make([]string, 0, len(times))
	for _, t := range times {
		out = append(out, t.In(loc).Format("2006-01-02 15:04:05 -0700"))
	}
	return c.JSON(fiber.Map{"results": out})
}

// --- helpers --------------------------------------------------------------

func parseID(c *fiber.Ctx) (int64, error) {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return 0, c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}
	return id, nil
}

// buildSchedule maps the JSON request to a Schedule, applying defaults and
// validating fields. If existing is non-nil, fields not provided in the
// request fall back to the existing values (for partial updates we still
// require the major fields though).
func (h *Handlers) buildSchedule(req *scheduleReq, existing *store.Schedule) (*store.Schedule, error) {
	sc := &store.Schedule{}
	if existing != nil {
		*sc = *existing
	}

	if req.Name != "" {
		sc.Name = req.Name
	}
	if req.DeviceID != "" {
		sc.DeviceID = req.DeviceID
	}
	if req.Recipient != "" {
		sc.Recipient = req.Recipient
	}
	if req.MessageType != "" {
		sc.MessageType = strings.ToLower(req.MessageType)
	}
	sc.Message = req.Message
	sc.MediaURL = req.MediaURL
	sc.Caption = req.Caption
	sc.Latitude = req.Latitude
	sc.Longitude = req.Longitude
	sc.LinkURL = req.LinkURL

	if req.ScheduleType != "" {
		sc.ScheduleType = strings.ToLower(req.ScheduleType)
	}
	sc.CronExpr = req.CronExpr

	if req.Timezone != "" {
		sc.Timezone = req.Timezone
	}
	if sc.Timezone == "" {
		sc.Timezone = h.DefaultTZ
	}

	if req.RunAt != "" {
		t, err := parseLocalTime(req.RunAt, sc.Timezone)
		if err != nil {
			return nil, fmt.Errorf("invalid run_at: %w", err)
		}
		sc.RunAt = &t
	}
	if req.Enabled != nil {
		sc.Enabled = *req.Enabled
	} else if existing == nil {
		sc.Enabled = true
	}

	// Validate
	if sc.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if sc.Recipient == "" {
		return nil, fmt.Errorf("recipient is required")
	}
	switch sc.MessageType {
	case "text":
		if sc.Message == "" {
			return nil, fmt.Errorf("message is required for message_type=text")
		}
	case "image", "video", "file", "audio":
		if sc.MediaURL == "" {
			return nil, fmt.Errorf("media_url is required for message_type=%s", sc.MessageType)
		}
	case "location":
		if sc.Latitude == "" || sc.Longitude == "" {
			return nil, fmt.Errorf("latitude and longitude are required for message_type=location")
		}
	case "link":
		if sc.LinkURL == "" {
			return nil, fmt.Errorf("link_url is required for message_type=link")
		}
	default:
		return nil, fmt.Errorf("unknown message_type %q", sc.MessageType)
	}

	switch sc.ScheduleType {
	case "once":
		if sc.RunAt == nil {
			return nil, fmt.Errorf("run_at is required for schedule_type=once")
		}
	case "daily", "weekly", "monthly", "yearly":
		if sc.RunAt == nil {
			return nil, fmt.Errorf("run_at is required (provides time-of-day) for schedule_type=%s", sc.ScheduleType)
		}
	case "cron":
		if sc.CronExpr == "" {
			return nil, fmt.Errorf("cron_expr is required for schedule_type=cron")
		}
	default:
		return nil, fmt.Errorf("unknown schedule_type %q", sc.ScheduleType)
	}
	return sc, nil
}

// parseLocalTime parses common datetime-local formats in the schedule's timezone.
func parseLocalTime(s, tz string) (time.Time, error) {
	loc := time.Local
	if tz != "" && !strings.EqualFold(tz, "Local") {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	layouts := []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		time.RFC3339,
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized datetime format %q", s)
}

// --- AI Reply proxies -----------------------------------------------------
// Device id is taken from the X-Device-Id header (consistent with core's
// expectation). Upload streams the multipart body verbatim — no re-parse.

func aiDeviceID(c *fiber.Ctx) (string, error) {
	id := strings.TrimSpace(c.Get("X-Device-Id"))
	if id == "" {
		// Fall back to query string so links can carry it too.
		id = strings.TrimSpace(c.Query("device_id"))
	}
	if id == "" {
		return "", c.Status(400).JSON(fiber.Map{"error": "X-Device-Id header required"})
	}
	return id, nil
}

func aiForward(c *fiber.Ctx, resp *wa.Response, err error) error {
	if err != nil {
		msg := err.Error()
		// Detect core's "Cannot {METHOD} /aireply/..." 404 yang artinya
		// AI Reply feature di core belum di-enable (AI_REPLY_ENABLED=false
		// atau service init gagal). Terjemahkan ke pesan actionable
		// supaya user nggak bingung disangka reverse-proxy issue.
		if strings.Contains(msg, "/aireply/") && strings.Contains(msg, " -> 404:") {
			return c.Status(503).JSON(fiber.Map{
				"error":          "AI Reply belum aktif di core. Set AI_REPLY_ENABLED=true di src/.env (atau pakai docker entrypoint default), lalu restart container core.",
				"code":           "AI_REPLY_DISABLED",
				"hint":           "docker compose -f docker-compose.aapanel.yml restart whatsapp_go",
				"upstream_error": msg,
			})
		}
		// AI_ENCRYPTION_KEY invalid (bukan hex / panjang salah). Sering kena
		// kalau user manually set passphrase di src/.env. Entrypoint baru
		// sudah validasi & regenerate kalau invalid, tapi container lama
		// atau env compose yang force-set bisa kena ini.
		if strings.Contains(msg, "AI_ENCRYPTION_KEY") && strings.Contains(msg, "hex") {
			return c.Status(500).JSON(fiber.Map{
				"error":          "AI_ENCRYPTION_KEY core tidak valid hex (harus 64 karakter 0-9a-f). Hapus baris AI_ENCRYPTION_KEY di src/.env supaya entrypoint Docker auto-generate, lalu rebuild & restart container core. Atau set manual: openssl rand -hex 32",
				"code":           "AI_ENCRYPTION_KEY_INVALID",
				"hint":           "docker compose -f docker-compose.aapanel.yml up -d --build whatsapp_go",
				"upstream_error": msg,
			})
		}
		body := fiber.Map{"error": msg}
		if resp != nil {
			body["upstream"] = resp
		}
		return c.Status(502).JSON(body)
	}
	return c.JSON(resp)
}

func (h *Handlers) aiGetConfig(c *fiber.Ctx) error {
	id, err := aiDeviceID(c)
	if err != nil {
		return nil
	}
	resp, e := h.WA.GetAIConfig(id)
	return aiForward(c, resp, e)
}

func (h *Handlers) aiSaveConfig(c *fiber.Ctx) error {
	id, err := aiDeviceID(c)
	if err != nil {
		return nil
	}
	resp, e := h.WA.SaveAIConfig(id, c.Body())
	return aiForward(c, resp, e)
}

func (h *Handlers) aiTestConfig(c *fiber.Ctx) error {
	id, err := aiDeviceID(c)
	if err != nil {
		return nil
	}
	resp, e := h.WA.TestAIConfig(id)
	return aiForward(c, resp, e)
}

func (h *Handlers) aiUploadDocument(c *fiber.Ctx) error {
	id, err := aiDeviceID(c)
	if err != nil {
		return nil
	}
	ct := c.Get("Content-Type")
	if !strings.HasPrefix(ct, "multipart/form-data") {
		return c.Status(400).JSON(fiber.Map{"error": "expected multipart/form-data"})
	}
	resp, e := h.WA.UploadAIDocument(id, bytes.NewReader(c.Body()), ct)
	return aiForward(c, resp, e)
}

func (h *Handlers) aiListDocuments(c *fiber.Ctx) error {
	id, err := aiDeviceID(c)
	if err != nil {
		return nil
	}
	resp, e := h.WA.ListAIDocuments(id)
	return aiForward(c, resp, e)
}

func (h *Handlers) aiDeleteDocument(c *fiber.Ctx) error {
	id, err := aiDeviceID(c)
	if err != nil {
		return nil
	}
	resp, e := h.WA.DeleteAIDocument(id, c.Params("id"))
	return aiForward(c, resp, e)
}

func (h *Handlers) aiReindexDocuments(c *fiber.Ctx) error {
	id, err := aiDeviceID(c)
	if err != nil {
		return nil
	}
	resp, e := h.WA.ReindexAIDocuments(id)
	return aiForward(c, resp, e)
}

func (h *Handlers) aiListChatSettings(c *fiber.Ctx) error {
	id, err := aiDeviceID(c)
	if err != nil {
		return nil
	}
	resp, e := h.WA.ListAIChatSettings(id)
	return aiForward(c, resp, e)
}

func (h *Handlers) aiSetChatEnabled(c *fiber.Ctx) error {
	id, err := aiDeviceID(c)
	if err != nil {
		return nil
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	// Fiber 2.52 returns path params verbatim — encoded "%40" stays as
	// "%40" instead of becoming "@". Decode here so the wa client gets a
	// clean JID; otherwise re-escaping double-encodes and core rejects.
	jid, err := url.PathUnescape(c.Params("chat_jid"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid chat_jid: " + err.Error()})
	}
	resp, e := h.WA.SetAIChatEnabled(id, jid, body.Enabled)
	return aiForward(c, resp, e)
}

func (h *Handlers) aiListLogs(c *fiber.Ctx) error {
	id, err := aiDeviceID(c)
	if err != nil {
		return nil
	}
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	resp, e := h.WA.ListAILogs(id, c.Query("chat_jid"), c.Query("status"), limit)
	return aiForward(c, resp, e)
}

// --- Apply-to-all helpers -------------------------------------------------

type applyResult struct {
	DeviceID string `json:"device_id"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
}

// listLoggedInDeviceIDs returns identifier yang dashboard pakai untuk
// X-Device-Id, untuk SEMUA device yang state-nya logged_in atau connected.
// sourceDeviceID dipakai sebagai auth context utk panggilan /app/devices.
//
// Core response shape (per src/domains/device/device.go):
//
//	{ "id": "<alias, map key>", "jid": "<WA JID>", "state": "logged_in",
//	  "phone_number": "...", "display_name": "...", "created_at": "..." }
//
// Penting: pakai `id` (alias) sebagai X-Device-Id, BUKAN `jid`. Core's
// DeviceManager.ResolveDevice cuma lookup `m.devices[id]` — map keyed by
// alias yang user assign waktu create device (dashboard SPA juga pakai
// `d.id` sebagai selectedDevice). Pakai JID akan dapat "device not found".
//
// Filter state: pakai field `state` string ("logged_in" / "connected" /
// "disconnected"). Field `is_logged_in` (boolean) TIDAK ADA di response.
//
// Defensive parsing: response bisa direct array atau wrapped {data:[]}.
func (h *Handlers) listLoggedInDeviceIDs(sourceDeviceID string) ([]string, error) {
	resp, err := h.WA.ListDevices(sourceDeviceID)
	if err != nil {
		return nil, err
	}
	// Enrich dgn /devices/:id/status — supaya is_logged_in & is_connected
	// flags ada dan filter di bawah pakai data akurat. Lihat enrichDeviceListWithStatus
	// untuk reasoning kenapa state field core lossy.
	if enriched, ok := enrichDeviceListWithStatus(h.WA, resp.Results); ok {
		resp.Results = enriched
	}
	devs, err := parseDeviceList(resp.Results)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(devs))
	skipped := 0
	for _, d := range devs {
		// Tentukan eligibility utk apply-to-all. AI config save = DB write,
		// tidak butuh WS aktif — jadi paired-offline device pun OK (config
		// dipakai saat device reconnect). Yang HARUS skip: device unpaired
		// atau yg session-nya hilang sama sekali.
		//
		// Core baru (post-fix) expose is_logged_in bool — pakai itu kalau ada.
		// Core lama cuma punya state string ("logged_in" / "connected" /
		// "disconnected") yang semantic-nya pre-fix ambigu (logged_in di core
		// lama bisa berarti paired-offline). Tetap include keduanya supaya
		// behavior konsisten.
		included := false
		// Path 1: explicit is_logged_in boolean (core baru).
		if v, ok := d["is_logged_in"]; ok {
			if b, isBool := v.(bool); isBool {
				if !b {
					skipped++
					continue
				}
				included = true
			}
		}
		// Path 2: fallback ke state string. logged_in & connected = include;
		// disconnected hanya skip kalau is_logged_in juga tidak ada (= core lama).
		if !included {
			if v, ok := d["state"].(string); ok && v != "" {
				if v != "logged_in" && v != "connected" {
					skipped++
					continue
				}
			}
		}

		// Identifier: pakai `id` (alias) — itu yang DeviceManager pakai
		// sebagai map key. SPA juga pakai ini sebagai selectedDevice.
		// Fallback `jid` kalau `id` kosong (kasus rare: device dibuat via
		// LoadExistingDevices dengan JID langsung sebagai key).
		var id string
		for _, k := range []string{"id", "device", "name", "jid"} {
			if v, ok := d[k].(string); ok && strings.TrimSpace(v) != "" {
				id = strings.TrimSpace(v)
				break
			}
		}
		if id == "" {
			skipped++
			continue
		}
		out = append(out, id)
	}
	log.Printf("[ai-apply] listLoggedInDeviceIDs: %d eligible, %d skipped (not logged_in / missing id)", len(out), skipped)
	return out, nil
}

// parseDeviceList tries multiple shapes — direct array or wrapped {data: [...]}.
func parseDeviceList(raw json.RawMessage) ([]map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	// Try direct array.
	var direct []map[string]any
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct, nil
	}
	// Try wrapped {data: [...]}.
	var wrapped struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Data != nil {
		return wrapped.Data, nil
	}
	// Try wrapped {devices: [...]} (some older core builds).
	var wrapped2 struct {
		Devices []map[string]any `json:"devices"`
	}
	if err := json.Unmarshal(raw, &wrapped2); err == nil && wrapped2.Devices != nil {
		return wrapped2.Devices, nil
	}
	return nil, fmt.Errorf("unexpected device list format (not array nor {data:[]} nor {devices:[]}). First 200 bytes: %s", string(raw[:min(len(raw), 200)]))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// extractAPIKeyFromBody ambil api_key dari JSON body config request, untuk
// pre-flight check. Return "" kalau field tidak ada atau kosong.
func extractAPIKeyFromBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	if v, ok := m["api_key"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// deviceHasAPIKey return true kalau device tsb sudah punya api_key tersimpan
// (core return masked key seperti "sk-v****abc"). Return false kalau "config
// not set" atau api_key field kosong/null.
//
// Best-effort: kalau core error 5xx atau timeout, return false untuk
// trigger error message yg meminta user input key — safer than assuming
// device punya key dan akhirnya silent fail.
func (h *Handlers) deviceHasAPIKey(deviceID string) bool {
	resp, err := h.WA.GetAIConfig(deviceID)
	if err != nil {
		log.Printf("[ai-apply] deviceHasAPIKey(%s): GetAIConfig error %v -> assume no key", deviceID, err)
		return false
	}
	if resp == nil || len(resp.Results) == 0 {
		return false
	}
	var cfg map[string]any
	if e := json.Unmarshal(resp.Results, &cfg); e != nil {
		return false
	}
	if v, ok := cfg["api_key"].(string); ok && strings.TrimSpace(v) != "" {
		return true
	}
	return false
}

// aiApplyConfigToAll — fan-out config + chat-settings ke semua device logged_in.
//
// Flow:
//  1. Pre-flight: cek setiap device. Kalau ada device yang BELUM punya
//     config (atau punya tapi api_key masih kosong) DAN body request
//     juga api_key kosong → return 400 dengan list device yang perlu key.
//     Tanpa ini, device baru disimpan dengan empty key → AI Reply silent
//     fail decrypt → device tidak balas (silent bug yang user laporkan).
//  2. Save config (body request) ke setiap device logged_in.
//  3. Read source device's chat-settings, replicate ke semua device.
//
// Step 3 PENTING — tanpa ini, device 2,3,dst punya config tapi tidak ada
// chat-settings row, jadi AI Reply silent skip (per core gating rules).
//
// Optional: ?with_chats=false untuk skip step 3 (cuma config). Default true.
func (h *Handlers) aiApplyConfigToAll(c *fiber.Ctx) error {
	sourceID, err := aiDeviceID(c)
	if err != nil {
		return nil
	}
	body := c.Body()
	if len(body) == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "config body required"})
	}

	deviceIDs, err := h.listLoggedInDeviceIDs(sourceID)
	if err != nil {
		return c.Status(502).JSON(fiber.Map{"error": "failed to list devices: " + err.Error()})
	}
	if len(deviceIDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "no logged-in devices found"})
	}

	// --- Step 1: pre-flight check api_key ---
	// Body adalah JSON config; ekstrak field api_key utk validasi.
	bodyAPIKey := extractAPIKeyFromBody(body)
	if bodyAPIKey == "" {
		// Kalau form kosong, identify device mana yang belum punya key tersimpan.
		// Device tsb akan rusak silent kalau di-save tanpa key.
		needsKey := []string{}
		for _, devID := range deviceIDs {
			if !h.deviceHasAPIKey(devID) {
				needsKey = append(needsKey, devID)
			}
		}
		if len(needsKey) > 0 {
			return c.Status(400).JSON(fiber.Map{
				"error": fmt.Sprintf(
					"%d device belum punya API key tersimpan: %s. Isi field 'API Key' di form dulu — kalau kosong, device-device ini akan punya config tanpa key dan AI Reply gagal silent.",
					len(needsKey), strings.Join(needsKey, ", ")),
				"code":                "AI_API_KEY_REQUIRED_FOR_NEW_DEVICES",
				"devices_needing_key": needsKey,
			})
		}
		log.Printf("[ai-apply] all %d devices already have api_key — empty body key OK (will preserve)", len(deviceIDs))
	}

	// --- Step 2: save config ke semua device ---
	configResults := make([]applyResult, 0, len(deviceIDs))
	configOK := 0
	for _, devID := range deviceIDs {
		_, e := h.WA.SaveAIConfig(devID, body)
		if e != nil {
			configResults = append(configResults, applyResult{DeviceID: devID, OK: false, Error: e.Error()})
			log.Printf("[ai-apply] save config to %s: FAIL %v", devID, e)
		} else {
			configResults = append(configResults, applyResult{DeviceID: devID, OK: true})
			configOK++
			log.Printf("[ai-apply] save config to %s: OK", devID)
		}
	}

	// --- Step 2-3: replicate chat-settings (default ON, skip with ?with_chats=false) ---
	withChats := c.Query("with_chats", "true") != "false"
	var chatStats fiber.Map
	if withChats {
		chatStats = h.replicateChatSettings(sourceID, deviceIDs)
	}

	return c.JSON(fiber.Map{
		"success_count":  configOK,
		"total":          len(configResults),
		"results":        configResults,
		"chat_sync":      chatStats,
		"with_chats":     withChats,
	})
}

// replicateChatSettings ambil source device's chat-settings, lalu apply
// setiap row ke semua device. Idempotent — kalau row sudah ada, core update.
// Skipped silently kalau source tidak punya chat-settings (mis. user baru
// configure config tanpa toggle chat apapun).
func (h *Handlers) replicateChatSettings(sourceID string, deviceIDs []string) fiber.Map {
	stats := fiber.Map{
		"source_chats":  0,
		"target_devices": len(deviceIDs),
		"applied_total": 0,
		"applied_ok":    0,
		"applied_fail":  0,
		"errors":        []fiber.Map{},
	}

	resp, err := h.WA.ListAIChatSettings(sourceID)
	if err != nil {
		stats["errors"] = append(stats["errors"].([]fiber.Map), fiber.Map{
			"phase": "list source chats",
			"error": err.Error(),
		})
		return stats
	}

	// Parse: core returns array of {chat_jid: "...", enabled: bool, ...}
	type chatRow struct {
		ChatJID string `json:"chat_jid"`
		Enabled bool   `json:"enabled"`
	}
	var chats []chatRow
	if err := json.Unmarshal(resp.Results, &chats); err != nil {
		// Try wrapped format
		var wrapped struct {
			Data []chatRow `json:"data"`
		}
		if err2 := json.Unmarshal(resp.Results, &wrapped); err2 == nil {
			chats = wrapped.Data
		}
	}
	stats["source_chats"] = len(chats)

	if len(chats) == 0 {
		log.Printf("[ai-apply] source %s has no chat-settings, skip replication", sourceID)
		return stats
	}

	appliedTotal := 0
	appliedOK := 0
	appliedFail := 0
	errs := []fiber.Map{}

	for _, row := range chats {
		if strings.TrimSpace(row.ChatJID) == "" {
			continue
		}
		for _, devID := range deviceIDs {
			appliedTotal++
			_, e := h.WA.SetAIChatEnabled(devID, row.ChatJID, row.Enabled)
			if e != nil {
				appliedFail++
				errs = append(errs, fiber.Map{
					"device":   devID,
					"chat_jid": row.ChatJID,
					"error":    e.Error(),
				})
				log.Printf("[ai-apply] set chat %s on %s: FAIL %v", row.ChatJID, devID, e)
			} else {
				appliedOK++
				log.Printf("[ai-apply] set chat %s on %s = %v: OK", row.ChatJID, devID, row.Enabled)
			}
		}
	}

	stats["applied_total"] = appliedTotal
	stats["applied_ok"] = appliedOK
	stats["applied_fail"] = appliedFail
	if len(errs) > 0 {
		stats["errors"] = errs
	}
	return stats
}

// aiApplyChatToAll — fan-out PUT /aireply/chat-settings/:chat_jid ke semua
// device logged_in. Body: {"enabled": bool}. Berguna mis. customer multi-channel
// yg chat ke 3 nomor sekaligus — semua nomor harus auto-reply.
func (h *Handlers) aiApplyChatToAll(c *fiber.Ctx) error {
	sourceID, err := aiDeviceID(c)
	if err != nil {
		return nil
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	chatJID, err := url.PathUnescape(c.Params("chat_jid"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid chat_jid: " + err.Error()})
	}

	deviceIDs, err := h.listLoggedInDeviceIDs(sourceID)
	if err != nil {
		return c.Status(502).JSON(fiber.Map{"error": "failed to list devices: " + err.Error()})
	}
	if len(deviceIDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "no logged-in devices found"})
	}

	results := make([]applyResult, 0, len(deviceIDs))
	successCount := 0
	for _, devID := range deviceIDs {
		_, e := h.WA.SetAIChatEnabled(devID, chatJID, body.Enabled)
		if e != nil {
			results = append(results, applyResult{DeviceID: devID, OK: false, Error: e.Error()})
		} else {
			results = append(results, applyResult{DeviceID: devID, OK: true})
			successCount++
		}
	}
	return c.JSON(fiber.Map{
		"success_count": successCount,
		"total":         len(results),
		"results":       results,
	})
}

// --- Pause / Resume proxy -------------------------------------------------

func (h *Handlers) aiPause(c *fiber.Ctx) error {
	id, err := aiDeviceID(c)
	if err != nil {
		return nil
	}
	var body struct {
		Minutes int `json:"minutes"`
	}
	_ = c.BodyParser(&body)
	resp, e := h.WA.PauseAIReply(id, body.Minutes)
	return aiForward(c, resp, e)
}

func (h *Handlers) aiResume(c *fiber.Ctx) error {
	id, err := aiDeviceID(c)
	if err != nil {
		return nil
	}
	resp, e := h.WA.ResumeAIReply(id)
	return aiForward(c, resp, e)
}

func (h *Handlers) aiPauseStatus(c *fiber.Ctx) error {
	id, err := aiDeviceID(c)
	if err != nil {
		return nil
	}
	resp, e := h.WA.GetAIPauseStatus(id)
	return aiForward(c, resp, e)
}

// aiMultiDeviceHealth — audit per-device readiness untuk AI Auto-Reply.
//
// Untuk setiap device logged_in, check:
//   - has_config       : ada row di ai_config?
//   - has_api_key      : api_key non-empty? (Core's Decrypt silently returns ""
//                        for empty bytes — silent fail!)
//   - has_embed_key    : embed_api_key set? (opsional, sebagian provider
//                        pakai api_key utama)
//   - chat_toggle_count: berapa chat sudah opt-in AI Reply
//   - status           : "ready" / "no_config" / "no_api_key" / "no_chats"
//
// User pakai endpoint ini utk verify SEMUA device benar-benar siap reply
// otomatis sebelum test, tanpa harus trial-and-error.
func (h *Handlers) aiMultiDeviceHealth(c *fiber.Ctx) error {
	sourceID, err := aiDeviceID(c)
	if err != nil {
		return nil
	}

	deviceIDs, err := h.listLoggedInDeviceIDs(sourceID)
	if err != nil {
		return c.Status(502).JSON(fiber.Map{"error": "failed to list devices: " + err.Error()})
	}

	type deviceHealth struct {
		DeviceID         string `json:"device_id"`
		HasConfig        bool   `json:"has_config"`
		HasAPIKey        bool   `json:"has_api_key"`
		HasEmbedKey      bool   `json:"has_embed_key"`
		Provider         string `json:"provider,omitempty"`
		Model            string `json:"model,omitempty"`
		ChatToggleCount  int    `json:"chat_toggle_count"`
		ChatEnabledCount int    `json:"chat_enabled_count"`
		Status           string `json:"status"`
		Hint             string `json:"hint,omitempty"`
	}

	results := make([]deviceHealth, 0, len(deviceIDs))
	readyCount := 0

	for _, devID := range deviceIDs {
		dh := deviceHealth{DeviceID: devID}

		// Cek config
		if cfgResp, e := h.WA.GetAIConfig(devID); e == nil && cfgResp != nil && len(cfgResp.Results) > 0 {
			var cfg map[string]any
			if json.Unmarshal(cfgResp.Results, &cfg) == nil {
				dh.HasConfig = true
				if v, ok := cfg["api_key"].(string); ok && strings.TrimSpace(v) != "" {
					dh.HasAPIKey = true
				}
				if v, ok := cfg["embed_api_key"].(string); ok && strings.TrimSpace(v) != "" {
					dh.HasEmbedKey = true
				}
				if v, ok := cfg["provider"].(string); ok {
					dh.Provider = v
				}
				if v, ok := cfg["model"].(string); ok {
					dh.Model = v
				}
			}
		}

		// Cek chat-toggle count
		if chatResp, e := h.WA.ListAIChatSettings(devID); e == nil && chatResp != nil && len(chatResp.Results) > 0 {
			var chats []map[string]any
			if err := json.Unmarshal(chatResp.Results, &chats); err != nil {
				// Coba wrapped
				var wrapped struct {
					Data []map[string]any `json:"data"`
				}
				if json.Unmarshal(chatResp.Results, &wrapped) == nil {
					chats = wrapped.Data
				}
			}
			dh.ChatToggleCount = len(chats)
			for _, ch := range chats {
				if b, ok := ch["enabled"].(bool); ok && b {
					dh.ChatEnabledCount++
				}
			}
		}

		// Derive status
		switch {
		case !dh.HasConfig:
			dh.Status = "no_config"
			dh.Hint = "Belum ada config AI. Buka tab AI Reply → Config, isi provider+model+API key, lalu klik 'Simpan ke Semua Device'."
		case !dh.HasAPIKey:
			dh.Status = "no_api_key"
			dh.Hint = "Config ada tapi API key kosong → AI Reply silent fail. Isi field 'API Key' di form, klik 'Simpan ke Semua Device' lagi."
		case dh.ChatEnabledCount == 0:
			dh.Status = "no_chats"
			dh.Hint = "Config OK tapi belum ada chat-toggle aktif. Tambah chat di sub-tab 'Chat Toggle' dengan checkbox 'Apply ke semua device' centang."
		default:
			dh.Status = "ready"
			readyCount++
		}

		results = append(results, dh)
	}

	overall := "all_ready"
	if readyCount == 0 {
		overall = "none_ready"
	} else if readyCount < len(results) {
		overall = "partial"
	}

	return c.JSON(fiber.Map{
		"overall":       overall,
		"ready_count":   readyCount,
		"total_devices": len(results),
		"results":       results,
	})
}

// --- Broadcast handlers ---------------------------------------------------

type broadcastReq struct {
	Name            string `json:"name"`
	DeviceID        string `json:"device_id"`
	MessageType     string `json:"message_type"` // text | image | video | file | audio
	Message         string `json:"message"`      // body (text type) atau caption (media type)
	MediaURL        string `json:"media_url"`
	Recipients      string `json:"recipients"` // raw input: comma/newline/space separated
	DelayMinMs      int    `json:"delay_min_ms"`
	DelayMaxMs      int    `json:"delay_max_ms"`
	BatchSize       int    `json:"batch_size"`
	BatchPauseMinMs int    `json:"batch_pause_min_ms"`
	BatchPauseMaxMs int    `json:"batch_pause_max_ms"`
	ShuffleOrder    bool   `json:"shuffle_order"`
	StartNow        bool   `json:"start_now"` // false = create draft only (jarang dipakai dr UI)
}

// previewBroadcastRecipients — parse recipients string dan kembalikan
// valid + invalid list TANPA membuat broadcast. Dipanggil saat user mengetik
// di textarea recipients supaya feedback live.
func (h *Handlers) previewBroadcastRecipients(c *fiber.Ctx) error {
	var req struct {
		Recipients string `json:"recipients"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	valid, invalid := broadcast.ParseRecipients(req.Recipients)
	// Untuk UI: kembalikan list normalized JID + list invalid token.
	jids := make([]string, 0, len(valid))
	for _, v := range valid {
		jids = append(jids, v.Recipient)
	}
	return c.JSON(fiber.Map{
		"valid_count":   len(jids),
		"valid":         jids,
		"invalid_count": len(invalid),
		"invalid":       invalid,
	})
}

func (h *Handlers) createBroadcast(c *fiber.Ctx) error {
	var req broadcastReq
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if strings.TrimSpace(req.DeviceID) == "" {
		return c.Status(400).JSON(fiber.Map{"error": "device_id required"})
	}
	mt := strings.ToLower(strings.TrimSpace(req.MessageType))
	if mt == "" {
		mt = "text"
	}
	switch mt {
	case "text":
		if strings.TrimSpace(req.Message) == "" {
			return c.Status(400).JSON(fiber.Map{"error": "message required for text broadcast"})
		}
	case "image", "video", "file", "document", "audio":
		if strings.TrimSpace(req.MediaURL) == "" {
			return c.Status(400).JSON(fiber.Map{"error": "media_url required for " + mt + " broadcast"})
		}
	default:
		return c.Status(400).JSON(fiber.Map{"error": "unsupported message_type: " + req.MessageType})
	}

	valid, invalid := broadcast.ParseRecipients(req.Recipients)
	if len(valid) == 0 {
		return c.Status(400).JSON(fiber.Map{
			"error":   "no valid recipients parsed",
			"invalid": invalid,
		})
	}

	// Sanity defaults untuk anti-spam. Kalau user kasih 0/0 risiko banned
	// tinggi — paksa minimal 3 detik supaya dashboard bukan jadi spam tool.
	delayMin := req.DelayMinMs
	delayMax := req.DelayMaxMs
	if delayMin < 3000 {
		delayMin = 3000
	}
	if delayMax < delayMin {
		delayMax = delayMin
	}

	bc := &store.Broadcast{
		Name:            strings.TrimSpace(req.Name),
		DeviceID:        req.DeviceID,
		MessageType:     mt,
		Message:         req.Message,
		MediaURL:        req.MediaURL,
		DelayMinMs:      delayMin,
		DelayMaxMs:      delayMax,
		BatchSize:       req.BatchSize,
		BatchPauseMinMs: req.BatchPauseMinMs,
		BatchPauseMaxMs: req.BatchPauseMaxMs,
		ShuffleOrder:    req.ShuffleOrder,
		Status:          "pending",
	}
	if bc.Name == "" {
		bc.Name = fmt.Sprintf("Broadcast %s", time.Now().Format("2006-01-02 15:04"))
	}

	id, err := h.Store.CreateBroadcast(bc, valid)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	// Default StartNow = true (UI biasanya langsung kirim setelah click "Start Broadcast").
	if !req.StartNow {
		// Buat draft tanpa start — return tanpa enqueue.
		fresh, _ := h.Store.GetBroadcast(id)
		return c.Status(201).JSON(fiber.Map{
			"results":         fresh,
			"invalid":         invalid,
			"draft":           true,
			"hint":            "set start_now=true to begin sending",
		})
	}

	if err := h.Broadcaster.Start(id); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error":   "broadcast saved but failed to start: " + err.Error(),
			"id":      id,
			"invalid": invalid,
		})
	}
	fresh, _ := h.Store.GetBroadcast(id)
	return c.Status(201).JSON(fiber.Map{
		"results": fresh,
		"invalid": invalid,
	})
}

func (h *Handlers) listBroadcasts(c *fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "100"))
	list, err := h.Store.ListBroadcasts(limit)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"results": list})
}

func (h *Handlers) getBroadcast(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	bc, err := h.Store.GetBroadcast(id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"results": bc,
		"running": h.Broadcaster.IsRunning(id),
	})
}

func (h *Handlers) getBroadcastRecipients(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	status := c.Query("status") // empty = all
	recs, err := h.Store.ListBroadcastRecipients(id, status)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"results": recs})
}

func (h *Handlers) cancelBroadcast(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := h.Broadcaster.Cancel(id); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	// Cancel async — broadcast worker akan mark finished sendiri begitu lihat ctx.Done().
	return c.JSON(fiber.Map{"results": "cancellation requested"})
}

func (h *Handlers) deleteBroadcast(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if h.Broadcaster.IsRunning(id) {
		return c.Status(409).JSON(fiber.Map{"error": "broadcast is running; cancel first"})
	}
	if err := h.Store.DeleteBroadcast(id); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"results": "deleted"})
}

