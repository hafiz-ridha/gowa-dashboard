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

// ListDevices proxies GET /app/devices.
func (c *Client) ListDevices() (*Response, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/app/devices", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, "")
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
func (c *Client) CreateDevice(deviceID string) (*Response, error) {
	body, err := json.Marshal(map[string]string{"device_id": deviceID})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/devices", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, "")
}

// DeleteDevice proxies DELETE /devices/:id.
func (c *Client) DeleteDevice(deviceID string) (*Response, error) {
	u := fmt.Sprintf("%s/devices/%s", c.BaseURL, url.PathEscape(deviceID))
	req, err := http.NewRequest(http.MethodDelete, u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, "")
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
