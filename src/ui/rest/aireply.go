package rest

import (
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	domain "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/aireply"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	aireplyUC "github.com/aldinokemal/go-whatsapp-web-multidevice/usecase/aireply"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
)

// AIReply wires the REST surface for the AI auto-reply feature.
type AIReply struct {
	Service *aireplyUC.Service
}

// InitRestAIReply registers all /aireply/* routes.
func InitRestAIReply(app fiber.Router, service *aireplyUC.Service) AIReply {
	r := AIReply{Service: service}
	app.Get("/aireply/config", r.GetConfig)
	app.Put("/aireply/config", r.SaveConfig)
	app.Post("/aireply/config/test", r.TestConfig)
	app.Post("/aireply/documents", r.UploadDocument)
	app.Get("/aireply/documents", r.ListDocuments)
	app.Delete("/aireply/documents/:id", r.DeleteDocument)
	app.Post("/aireply/documents/reindex", r.ReindexAll)
	app.Get("/aireply/chat-settings", r.ListChatSettings)
	app.Put("/aireply/chat-settings/:chat_jid", r.SetChatEnabled)
	app.Get("/aireply/logs", r.ListLogs)
	// Global pause toggle — affects all devices + all chats. State in-memory
	// only (resets on container restart, intentional safety).
	app.Post("/aireply/pause", r.Pause)
	app.Post("/aireply/resume", r.Resume)
	app.Get("/aireply/pause-status", r.PauseStatus)
	return r
}

// resolveDeviceID extracts the AI-scope device id (non-AD JID) from the
// request's device context. Returns "" if the request is not bound to a
// connected device.
func resolveDeviceID(c *fiber.Ctx) string {
	d := getDeviceFromCtx(c)
	if d == nil {
		return ""
	}
	return d.JID()
}

func mustDevice(c *fiber.Ctx) string {
	id := resolveDeviceID(c)
	if id == "" {
		panic(fiber.NewError(fiber.StatusBadRequest, "no connected device for this request (login & select a device first)"))
	}
	return id
}

func (h *AIReply) GetConfig(c *fiber.Ctx) error {
	cfg, err := h.Service.GetConfig(c.UserContext(), mustDevice(c))
	utils.PanicIfNeeded(err)
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Results: cfg})
}

func (h *AIReply) SaveConfig(c *fiber.Ctx) error {
	var req domain.AIConfigRequest
	utils.PanicIfNeeded(c.BodyParser(&req))
	utils.PanicIfNeeded(validations.ValidateAIConfigRequest(req))
	utils.PanicIfNeeded(h.Service.SaveConfig(c.UserContext(), mustDevice(c), req))
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "config saved"})
}

func (h *AIReply) TestConfig(c *fiber.Ctx) error {
	latency, sample, err := h.Service.TestConfig(c.UserContext(), mustDevice(c))
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(utils.ResponseData{
			Status: fiber.StatusBadGateway, Code: "ERROR", Message: err.Error(),
		})
	}
	return c.JSON(utils.ResponseData{
		Status: 200, Code: "SUCCESS",
		Results: fiber.Map{"latency_ms": latency, "model_response": sample},
	})
}

func (h *AIReply) UploadDocument(c *fiber.Ctx) error {
	file, err := c.FormFile("file")
	utils.PanicIfNeeded(err)
	utils.PanicIfNeeded(validations.ValidateKBUpload(file.Filename, file.Size))

	f, err := file.Open()
	utils.PanicIfNeeded(err)
	defer f.Close()
	data, err := io.ReadAll(f)
	utils.PanicIfNeeded(err)

	mime := file.Header.Get("Content-Type")
	doc, err := h.Service.UploadDocument(c.UserContext(), mustDevice(c), file.Filename, mime, data)
	utils.PanicIfNeeded(err)
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Results: doc})
}

func (h *AIReply) ListDocuments(c *fiber.Ctx) error {
	docs, err := h.Service.ListDocuments(c.UserContext(), mustDevice(c))
	utils.PanicIfNeeded(err)
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Results: docs})
}

func (h *AIReply) DeleteDocument(c *fiber.Ctx) error {
	id := strings.TrimSpace(c.Params("id"))
	if id == "" {
		return fiber.NewError(fiber.StatusBadRequest, "id required")
	}
	utils.PanicIfNeeded(h.Service.DeleteDocument(c.UserContext(), mustDevice(c), id))
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "deleted"})
}

func (h *AIReply) ReindexAll(c *fiber.Ctx) error {
	utils.PanicIfNeeded(h.Service.ReindexAll(c.UserContext(), mustDevice(c)))
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "reindexed"})
}

func (h *AIReply) ListChatSettings(c *fiber.Ctx) error {
	out, err := h.Service.ListChatSettings(c.UserContext(), mustDevice(c))
	utils.PanicIfNeeded(err)
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Results: out})
}

func (h *AIReply) SetChatEnabled(c *fiber.Ctx) error {
	jid := c.Params("chat_jid")
	utils.PanicIfNeeded(validations.ValidateChatJID(jid))
	var req domain.ChatSettingRequest
	utils.PanicIfNeeded(c.BodyParser(&req))
	utils.PanicIfNeeded(h.Service.SetChatEnabled(c.UserContext(), mustDevice(c), jid, req.Enabled))
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "updated"})
}

// Pause AI Reply globally. Body: {"minutes": 30}. minutes <= 0 = indefinite
// (until next container restart or explicit Resume). Returns deadline.
func (h *AIReply) Pause(c *fiber.Ctx) error {
	var req struct {
		Minutes int `json:"minutes"`
	}
	_ = c.BodyParser(&req)
	duration := time.Duration(req.Minutes) * time.Minute
	until := aireplyUC.Pause(duration)
	return c.JSON(utils.ResponseData{
		Status: 200, Code: "SUCCESS",
		Results: fiber.Map{
			"paused":           true,
			"paused_until":     until,
			"duration_minutes": req.Minutes,
		},
	})
}

func (h *AIReply) Resume(c *fiber.Ctx) error {
	aireplyUC.Resume()
	return c.JSON(utils.ResponseData{
		Status: 200, Code: "SUCCESS",
		Results: fiber.Map{"paused": false},
	})
}

func (h *AIReply) PauseStatus(c *fiber.Ctx) error {
	paused, until := aireplyUC.PauseStatus()
	res := fiber.Map{"paused": paused}
	if until != nil {
		res["paused_until"] = until
		res["remaining_seconds"] = int(time.Until(*until).Seconds())
	}
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Results: res})
}

func (h *AIReply) ListLogs(c *fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	logs, err := h.Service.ListLogs(c.UserContext(), domain.LogFilter{
		DeviceID: mustDevice(c),
		ChatJID:  strings.TrimSpace(c.Query("chat_jid")),
		Status:   strings.TrimSpace(c.Query("status")),
		Limit:    limit,
	})
	utils.PanicIfNeeded(err)
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Results: logs})
}
