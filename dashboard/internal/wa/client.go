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
	"sync"
	"time"
)

// Client is a thin HTTP client for the core go-whatsapp-web-multidevice REST API.
//
// Fields are unexported + mutex-guarded because the core connection target
// can be changed at runtime from the dashboard's "Pengaturan" tab (see
// UpdateConfig) while requests from the scheduler/broadcaster/HTTP handlers
// are concurrently in flight against the same shared instance (dashboard/main.go
// constructs exactly one *Client and shares it everywhere).
type Client struct {
	mu       sync.RWMutex
	baseURL  string
	user     string
	password string
	HTTP     *http.Client
}

func NewClient(baseURL, user, password string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		user:     user,
		password: password,
		HTTP:     &http.Client{Timeout: 60 * time.Second},
	}
}

// BaseURL returns the currently configured core base URL (thread-safe).
func (c *Client) BaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

// UpdateConfig hot-swaps the core connection target. Safe to call while
// requests are in flight: each request snapshots baseURL+user+password once
// via snapshot() before it starts, so in-flight requests simply finish
// against whatever values they already captured — only requests started
// after this call see the new values.
func (c *Client) UpdateConfig(baseURL, user, password string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURL = strings.TrimRight(baseURL, "/")
	c.user = user
	c.password = password
}

// snapshot reads baseURL/user/password under a single lock so a request
// never mixes a pre-update base URL with post-update credentials (or vice
// versa) if UpdateConfig lands mid-request.
func (c *Client) snapshot() (baseURL, user, password string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL, c.user, c.password
}

// Generic response envelope used by the core API.
type Response struct {
	Status  int             `json:"status"`
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Results json.RawMessage `json:"results"`
}

func (c *Client) do(req *http.Request, deviceID, user, password string) (*Response, error) {
	if user != "" || password != "" {
		req.SetBasicAuth(user, password)
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
	baseURL, user, password := c.snapshot()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/devices", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID, user, password)
}

// Status proxies GET /devices/:id/status (multi-device) or /app/status (single).
func (c *Client) DeviceStatus(deviceID string) (*Response, error) {
	baseURL, user, password := c.snapshot()
	var u string
	if deviceID != "" {
		u = fmt.Sprintf("%s/devices/%s/status", baseURL, url.PathEscape(deviceID))
	} else {
		u = baseURL + "/app/status"
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID, user, password)
}

// --- Device management ----------------------------------------------------

// CreateDevice proxies POST /devices with {"device_id": "<name>"}.
// authDeviceID is the X-Device-Id to send for middleware auth (typically the
// currently-selected device). Required when 2+ devices already exist;
// safe to pass empty when bootstrapping the first device.
func (c *Client) CreateDevice(newDeviceID, authDeviceID string) (*Response, error) {
	baseURL, user, password := c.snapshot()
	body, err := json.Marshal(map[string]string{"device_id": newDeviceID})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/devices", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, authDeviceID, user, password)
}

// DeleteDevice proxies DELETE /devices/:id. Sends the same id as
// X-Device-Id (the device being deleted exists; middleware passes).
func (c *Client) DeleteDevice(deviceID string) (*Response, error) {
	baseURL, user, password := c.snapshot()
	u := fmt.Sprintf("%s/devices/%s", baseURL, url.PathEscape(deviceID))
	req, err := http.NewRequest(http.MethodDelete, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID, user, password)
}

// Login proxies GET /app/login with X-Device-Id header. Returns the response
// envelope which contains { qr_link, qr_duration, device_id }.
func (c *Client) Login(deviceID string) (*Response, error) {
	baseURL, user, password := c.snapshot()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/app/login", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID, user, password)
}

// LoginWithCode proxies GET /app/login-with-code?phone=<phone> with X-Device-Id header.
// Returns the response envelope which contains { pair_code, device_id }.
func (c *Client) LoginWithCode(deviceID, phone string) (*Response, error) {
	baseURL, user, password := c.snapshot()
	u := fmt.Sprintf("%s/app/login-with-code?phone=%s", baseURL, url.QueryEscape(phone))
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID, user, password)
}

// Logout proxies POST /devices/:id/logout.
func (c *Client) Logout(deviceID string) (*Response, error) {
	baseURL, user, password := c.snapshot()
	u := fmt.Sprintf("%s/devices/%s/logout", baseURL, url.PathEscape(deviceID))
	req, err := http.NewRequest(http.MethodPost, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID, user, password)
}

// Reconnect proxies POST /devices/:id/reconnect.
func (c *Client) Reconnect(deviceID string) (*Response, error) {
	baseURL, user, password := c.snapshot()
	u := fmt.Sprintf("%s/devices/%s/reconnect", baseURL, url.PathEscape(deviceID))
	req, err := http.NewRequest(http.MethodPost, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID, user, password)
}

// FetchStatic downloads an arbitrary static asset from the core (used to
// proxy QR images so the browser can fetch them through the dashboard even
// when the core is bound to 127.0.0.1 only).
//
// Returns body bytes, content-type, and an error.
func (c *Client) FetchStatic(path string) ([]byte, string, error) {
	baseURL, user, password := c.snapshot()
	// path is expected to be like "/statics/qrcode/scan-qr-xxx.png" or just
	// "scan-qr-xxx.png" (we normalize below).
	if !strings.HasPrefix(path, "/") {
		path = "/statics/qrcode/" + path
	}
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, "", err
	}
	if user != "" || password != "" {
		req.SetBasicAuth(user, password)
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
	baseURL, user, password := c.snapshot()
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/send/message", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, deviceID, user, password)
}

type SendLinkRequest struct {
	Phone   string `json:"phone"`
	Link    string `json:"link"`
	Caption string `json:"caption,omitempty"`
}

func (c *Client) SendLink(deviceID string, p SendLinkRequest) (*Response, error) {
	baseURL, user, password := c.snapshot()
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/send/link", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, deviceID, user, password)
}

type SendLocationRequest struct {
	Phone     string `json:"phone"`
	Latitude  string `json:"latitude"`
	Longitude string `json:"longitude"`
}

func (c *Client) SendLocation(deviceID string, p SendLocationRequest) (*Response, error) {
	baseURL, user, password := c.snapshot()
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/send/location", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, deviceID, user, password)
}

// SendMedia uses multipart/form-data to send image/video/file/audio when only a URL is provided.
// kind = "image" | "video" | "file" | "audio"
func (c *Client) SendMediaURL(deviceID, kind, phone, mediaURL, caption string) (*Response, error) {
	baseURL, user, password := c.snapshot()
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
	req, err := http.NewRequest(http.MethodPost, baseURL+endpoint, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return c.do(req, deviceID, user, password)
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
	baseURL, user, password := c.snapshot()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/aireply/config", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID, user, password)
}

// SaveAIConfig accepts the raw JSON body so the dashboard handler can pass
// the user form through without re-marshalling (keeps schema drift in core).
func (c *Client) SaveAIConfig(deviceID string, body []byte) (*Response, error) {
	baseURL, user, password := c.snapshot()
	req, err := http.NewRequest(http.MethodPut, baseURL+"/aireply/config", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, deviceID, user, password)
}

func (c *Client) TestAIConfig(deviceID string) (*Response, error) {
	baseURL, user, password := c.snapshot()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/aireply/config/test", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID, user, password)
}

// UploadAIDocument streams the original multipart body straight to upstream
// so we don't re-parse the file (which could be up to AI_MAX_KB_FILE_SIZE,
// default 10MB). Caller passes the raw body reader + its Content-Type.
func (c *Client) UploadAIDocument(deviceID string, body io.Reader, contentType string) (*Response, error) {
	baseURL, user, password := c.snapshot()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/aireply/documents", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	return c.do(req, deviceID, user, password)
}

func (c *Client) ListAIDocuments(deviceID string) (*Response, error) {
	baseURL, user, password := c.snapshot()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/aireply/documents", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID, user, password)
}

func (c *Client) DeleteAIDocument(deviceID, id string) (*Response, error) {
	baseURL, user, password := c.snapshot()
	u := fmt.Sprintf("%s/aireply/documents/%s", baseURL, url.PathEscape(id))
	req, err := http.NewRequest(http.MethodDelete, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID, user, password)
}

func (c *Client) ReindexAIDocuments(deviceID string) (*Response, error) {
	baseURL, user, password := c.snapshot()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/aireply/documents/reindex", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID, user, password)
}

func (c *Client) ListAIChatSettings(deviceID string) (*Response, error) {
	baseURL, user, password := c.snapshot()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/aireply/chat-settings", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID, user, password)
}

func (c *Client) SetAIChatEnabled(deviceID, chatJID string, enabled bool) (*Response, error) {
	baseURL, user, password := c.snapshot()
	body, err := json.Marshal(map[string]bool{"enabled": enabled})
	if err != nil {
		return nil, err
	}
	// Core (Fiber 2.52) does not URL-decode :chat_jid in c.Params(), so a
	// percent-encoded "@" (%40) reaches the JID validator and is rejected
	// with "missing server". WhatsApp JIDs only contain digits, "@", and
	// "." (and ":" for AD-suffixed), all safe to embed raw in a path.
	u := fmt.Sprintf("%s/aireply/chat-settings/%s", baseURL, strings.ReplaceAll(url.PathEscape(chatJID), "%40", "@"))
	req, err := http.NewRequest(http.MethodPut, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, deviceID, user, password)
}

// PauseAIReply — global pause. minutes <= 0 = indefinite (until restart).
func (c *Client) PauseAIReply(deviceID string, minutes int) (*Response, error) {
	baseURL, user, password := c.snapshot()
	body, err := json.Marshal(map[string]int{"minutes": minutes})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/aireply/pause", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, deviceID, user, password)
}

func (c *Client) ResumeAIReply(deviceID string) (*Response, error) {
	baseURL, user, password := c.snapshot()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/aireply/resume", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID, user, password)
}

func (c *Client) GetAIPauseStatus(deviceID string) (*Response, error) {
	baseURL, user, password := c.snapshot()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/aireply/pause-status", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID, user, password)
}

func (c *Client) ListAILogs(deviceID, chatJID, status string, limit int) (*Response, error) {
	baseURL, user, password := c.snapshot()
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
	u := baseURL + "/aireply/logs"
	if enc := q.Encode(); enc != "" {
		u += "?" + enc
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, deviceID, user, password)
}
