package api

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/scheduler"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/wa"

	"github.com/gofiber/fiber/v2"
)

type Handlers struct {
	Store     *store.Store
	WA        *wa.Client
	Scheduler *scheduler.Scheduler
	DefaultTZ string
}

func (h *Handlers) Register(app *fiber.App) {
	g := app.Group("/api")
	g.Get("/_health", h.health) // version probe — useful for verifying you're on the new build
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
	g.Get("/schedules/:id", h.getSchedule)
	g.Put("/schedules/:id", h.updateSchedule)
	g.Delete("/schedules/:id", h.deleteSchedule)
	g.Post("/schedules/:id/toggle", h.toggleSchedule)
	g.Post("/schedules/:id/run", h.runSchedule)
	g.Get("/schedules/:id/logs", h.scheduleLogs)
	g.Post("/schedules/preview", h.previewSchedule)
	g.Get("/logs", h.recentLogs)
}

// --- proxies --------------------------------------------------------------

// health returns a small JSON probe listing the dashboard's own routes.
// Use it to verify you're talking to the rebuilt image (not a cached old one).
func (h *Handlers) health(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"ok":           true,
		"build":        "dashboard-v1.1-device-mgmt",
		"upstream_url": h.WA.BaseURL,
		"routes": []string{
			"GET    /api/_health",
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
			"GET    /api/logs",
		},
	})
}

func (h *Handlers) listDevices(c *fiber.Ctx) error {
	resp, err := h.WA.ListDevices()
	if err != nil {
		return c.Status(502).JSON(fiber.Map{"error": err.Error()})
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
	resp, err := h.WA.CreateDevice(req.DeviceID)
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

