package wa

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a thin HTTP client for the core go-whatsapp-web-multidevice REST API.
type Client struct {
	BaseURL  string
	User     string
	Password string
	HTTP     *http.Client
}

func NewClient(baseURL, user, password string) *Client {
	return &Client{
		BaseURL:  strings.TrimRight(baseURL, "/"),
		User:     user,
		Password: password,
		HTTP:     &http.Client{Timeout: 60 * time.Second},
	}
}

// Generic response envelope used by the core API.
type Response struct {
	Status  int             `json:"status"`
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Results json.RawMessage `json:"results"`
}

func (c *Client) do(req *http.Request, deviceID string) (*Response, error) {
	if c.User != "" || c.Password != "" {
		req.SetBasicAuth(c.User, c.Password)
	}
	if deviceID != "" {
		req.Header.Set("X-Device-Id", deviceID)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", req.Method, req.URL.String(), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		// Try to parse for nicer error message
		var env Response
		if json.Unmarshal(body, &env) == nil && env.Message != "" {
			return &env, fmt.Errorf("upstream %s %s -> %d: %s", req.Method, req.URL.Path, resp.StatusCode, env.Message)
		}
		return nil, fmt.Errorf("upstream %s %s -> %d: %s", req.Method, req.URL.Path, resp.StatusCode, string(body))
	}
	var env Response
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("invalid upstream response from %s %s: %w (body=%s)", req.Method, req.URL.Path, err, string(body))
	}
	return &env, nil
}

// ListDevices proxies GET /devices (NOT /app/devices). Bedanya:
//
//	/app/devices  → [{name, device}]                    (cuma 2 field, ambigu)
//	/devices      → [{id, jid, state, phone_number,
//	                  display_name, created_at}]        (rich, includes state)
//
// State field penting untuk UI badge "Connected/Disconnected" yang akurat —
// sebelumnya pakai is_connected/is_logged_in yang tidak ada di /app/devices,
// jadi UI selalu render fallback negatif (badge selalu merah).
//
// Core's device middleware requires X-Device-Id once there are 2+ devices
// (single-device mode auto-picks). Forward whatever the caller passes so
// the dashboard can use its currently-selected device as the auth context.
func (c *Client) ListDevices(deviceID string) (*Response, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/devices", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID)
}

// Status proxies GET /devices/:id/status (multi-device) or /app/status (single).
func (c *Client) DeviceStatus(deviceID string) (*Response, error) {
	var u string
	if deviceID != "" {
		u = fmt.Sprintf("%s/devices/%s/status", c.BaseURL, url.PathEscape(deviceID))
	} else {
		u = c.BaseURL + "/app/status"
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID)
}

// --- Device management ----------------------------------------------------

// CreateDevice proxies POST /devices with {"device_id": "<name>"}.
// authDeviceID is the X-Device-Id to send for middleware auth (typically the
// currently-selected device). Required when 2+ devices already exist;
// safe to pass empty when bootstrapping the first device.
func (c *Client) CreateDevice(newDeviceID, authDeviceID string) (*Response, error) {
	body, err := json.Marshal(map[string]string{"device_id": newDeviceID})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/devices", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, authDeviceID)
}

// DeleteDevice proxies DELETE /devices/:id. Sends the same id as
// X-Device-Id (the device being deleted exists; middleware passes).
func (c *Client) DeleteDevice(deviceID string) (*Response, error) {
	u := fmt.Sprintf("%s/devices/%s", c.BaseURL, url.PathEscape(deviceID))
	req, err := http.NewRequest(http.MethodDelete, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID)
}

// Login proxies GET /app/login with X-Device-Id header. Returns the response
// envelope which contains { qr_link, qr_duration, device_id }.
func (c *Client) Login(deviceID string) (*Response, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/app/login", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID)
}

// LoginWithCode proxies GET /app/login-with-code?phone=<phone> with X-Device-Id header.
// Returns the response envelope which contains { pair_code, device_id }.
func (c *Client) LoginWithCode(deviceID, phone string) (*Response, error) {
	u := fmt.Sprintf("%s/app/login-with-code?phone=%s", c.BaseURL, url.QueryEscape(phone))
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID)
}

// Logout proxies POST /devices/:id/logout.
func (c *Client) Logout(deviceID string) (*Response, error) {
	u := fmt.Sprintf("%s/devices/%s/logout", c.BaseURL, url.PathEscape(deviceID))
	req, err := http.NewRequest(http.MethodPost, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID)
}

// Reconnect proxies POST /devices/:id/reconnect.
func (c *Client) Reconnect(deviceID string) (*Response, error) {
	u := fmt.Sprintf("%s/devices/%s/reconnect", c.BaseURL, url.PathEscape(deviceID))
	req, err := http.NewRequest(http.MethodPost, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID)
}

// FetchStatic downloads an arbitrary static asset from the core (used to
// proxy QR images so the browser can fetch them through the dashboard even
// when the core is bound to 127.0.0.1 only).
//
// Returns body bytes, content-type, and an error.
func (c *Client) FetchStatic(path string) ([]byte, string, error) {
	// path is expected to be like "/statics/qrcode/scan-qr-xxx.png" or just
	// "scan-qr-xxx.png" (we normalize below).
	if !strings.HasPrefix(path, "/") {
		path = "/statics/qrcode/" + path
	}
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, "", err
	}
	if c.User != "" || c.Password != "" {
		req.SetBasicAuth(c.User, c.Password)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("upstream %d fetching %s", resp.StatusCode, path)
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/png"
	}
	return body, ct, nil
}

// --- Send helpers ---------------------------------------------------------

type SendTextRequest struct {
	Phone           string   `json:"phone"`
	Message         string   `json:"message"`
	ReplyMessageID  string   `json:"reply_message_id,omitempty"`
	Mentions        []string `json:"mentions,omitempty"`
	IsForwarded     bool     `json:"is_forwarded,omitempty"`
	Duration        int      `json:"duration,omitempty"`
}

func (c *Client) SendText(deviceID string, payload SendTextRequest) (*Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/send/message", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, deviceID)
}

type SendLinkRequest struct {
	Phone   string `json:"phone"`
	Link    string `json:"link"`
	Caption string `json:"caption,omitempty"`
}

func (c *Client) SendLink(deviceID string, p SendLinkRequest) (*Response, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/send/link", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, deviceID)
}

type SendLocationRequest struct {
	Phone     string `json:"phone"`
	Latitude  string `json:"latitude"`
	Longitude string `json:"longitude"`
}

func (c *Client) SendLocation(deviceID string, p SendLocationRequest) (*Response, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/send/location", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, deviceID)
}

// SendMedia uses multipart/form-data to send image/video/file/audio when only a URL is provided.
// kind = "image" | "video" | "file" | "audio"
func (c *Client) SendMediaURL(deviceID, kind, phone, mediaURL, caption string) (*Response, error) {
	endpoint, urlField := mediaEndpoint(kind)
	if endpoint == "" {
		return nil, fmt.Errorf("unsupported media kind %q", kind)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("phone", phone)
	_ = mw.WriteField(urlField, mediaURL)
	if caption != "" && (kind == "image" || kind == "video" || kind == "file") {
		_ = mw.WriteField("caption", caption)
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+endpoint, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return c.do(req, deviceID)
}

func mediaEndpoint(kind string) (endpoint, urlField string) {
	switch strings.ToLower(kind) {
	case "image":
		return "/send/image", "image_url"
	case "video":
		return "/send/video", "video_url"
	case "file", "document":
		return "/send/file", "file_url"
	case "audio":
		return "/send/audio", "audio_url"
	}
	return "", ""
}

// --- AI Auto-Reply --------------------------------------------------------
// All endpoints are device-scoped via X-Device-Id header. The dashboard
// proxies these 1:1 so the dashboard UI can be the single control plane.

func (c *Client) GetAIConfig(deviceID string) (*Response, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/aireply/config", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID)
}

// SaveAIConfig accepts the raw JSON body so the dashboard handler can pass
// the user form through without re-marshalling (keeps schema drift in core).
func (c *Client) SaveAIConfig(deviceID string, body []byte) (*Response, error) {
	req, err := http.NewRequest(http.MethodPut, c.BaseURL+"/aireply/config", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, deviceID)
}

func (c *Client) TestAIConfig(deviceID string) (*Response, error) {
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/aireply/config/test", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID)
}

// UploadAIDocument streams the original multipart body straight to upstream
// so we don't re-parse the file (which could be up to AI_MAX_KB_FILE_SIZE,
// default 10MB). Caller passes the raw body reader + its Content-Type.
func (c *Client) UploadAIDocument(deviceID string, body io.Reader, contentType string) (*Response, error) {
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/aireply/documents", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	return c.do(req, deviceID)
}

func (c *Client) ListAIDocuments(deviceID string) (*Response, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/aireply/documents", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID)
}

func (c *Client) DeleteAIDocument(deviceID, id string) (*Response, error) {
	u := fmt.Sprintf("%s/aireply/documents/%s", c.BaseURL, url.PathEscape(id))
	req, err := http.NewRequest(http.MethodDelete, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID)
}

func (c *Client) ReindexAIDocuments(deviceID string) (*Response, error) {
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/aireply/documents/reindex", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID)
}

func (c *Client) ListAIChatSettings(deviceID string) (*Response, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/aireply/chat-settings", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID)
}

func (c *Client) SetAIChatEnabled(deviceID, chatJID string, enabled bool) (*Response, error) {
	body, err := json.Marshal(map[string]bool{"enabled": enabled})
	if err != nil {
		return nil, err
	}
	// Core (Fiber 2.52) does not URL-decode :chat_jid in c.Params(), so a
	// percent-encoded "@" (%40) reaches the JID validator and is rejected
	// with "missing server". WhatsApp JIDs only contain digits, "@", and
	// "." (and ":" for AD-suffixed), all safe to embed raw in a path.
	u := fmt.Sprintf("%s/aireply/chat-settings/%s", c.BaseURL, strings.ReplaceAll(url.PathEscape(chatJID), "%40", "@"))
	req, err := http.NewRequest(http.MethodPut, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, deviceID)
}

// PauseAIReply — global pause. minutes <= 0 = indefinite (until restart).
func (c *Client) PauseAIReply(deviceID string, minutes int) (*Response, error) {
	body, err := json.Marshal(map[string]int{"minutes": minutes})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/aireply/pause", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, deviceID)
}

func (c *Client) ResumeAIReply(deviceID string) (*Response, error) {
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/aireply/resume", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID)
}

func (c *Client) GetAIPauseStatus(deviceID string) (*Response, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/aireply/pause-status", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID)
}

func (c *Client) ListAILogs(deviceID, chatJID, status string, limit int) (*Response, error) {
	q := url.Values{}
	if chatJID != "" {
		q.Set("chat_jid", chatJID)
	}
	if status != "" {
		q.Set("status", status)
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	u := c.BaseURL + "/aireply/logs"
	if enc := q.Encode(); enc != "" {
		u += "?" + enc
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID)
}
