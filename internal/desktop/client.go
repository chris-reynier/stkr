package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Client timeouts. Status and health checks are short; actions that may copy
// a prompt, export files or wait for an AI plan get longer.
const (
	ProbeTimeout  = 500 * time.Millisecond
	StatusTimeout = 2 * time.Second
	ActionTimeout = 15 * time.Second
)

// Status mirrors GET /api/status (contract status-api.md v1).
type Status struct {
	Now           *NowStatus      `json:"now"`
	Contexts      []StatusContext `json:"contexts"`
	CloseDue      bool            `json:"close_due"`
	Update        *UpdateStatus   `json:"update"`
	AgentsWaiting int             `json:"agents_waiting"`
	IdeasNew      int             `json:"ideas_new"`
	Version       string          `json:"version"`
	HostedBy      string          `json:"hosted_by"`
}

// NowStatus is the context being tracked.
type NowStatus struct {
	ContextID   string    `json:"context_id"`
	Label       string    `json:"label"`
	Kind        string    `json:"kind"`
	Since       time.Time `json:"since"`
	ElapsedS    int64     `json:"elapsed_s"`
	IdleSuspect bool      `json:"idle_suspect"`
}

// StatusContext is a context the user can switch to.
type StatusContext struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
}

// UpdateStatus is the cached update check.
type UpdateStatus struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
}

// APIError is an error answer of the server.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("server answered %d", e.Status)
}

// Client calls the local STKR HTTP API. It sends the Host and Origin the
// server's guards accept.
type Client struct {
	port int
	http *http.Client
}

// NewClient returns a client for http://127.0.0.1:<port>.
func NewClient(port int) *Client {
	return &Client{port: port, http: &http.Client{
		// Never follow redirects: answers are JSON, a redirect means it is not STKR.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// Port is the port the client talks to.
func (c *Client) Port() int { return c.port }

// BaseURL is "http://127.0.0.1:<port>".
func (c *Client) BaseURL() string { return "http://127.0.0.1:" + strconv.Itoa(c.port) }

// URL returns the absolute URL of path (which starts with "/").
func (c *Client) URL(path string) string { return c.BaseURL() + path }

// errNotSTKR means something answered on the port but it is not a STKR server.
var errNotSTKR = errors.New("not a STKR server")

// Probe checks GET /healthz within ProbeTimeout. It returns nil when a STKR
// server answers, errNotSTKR when something else answers, and the network
// error when nothing listens.
func (c *Client) Probe(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	var h struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}
	status, body, err := c.do(ctx, http.MethodGet, "/healthz")
	if err != nil {
		return err
	}
	if status != http.StatusOK || json.Unmarshal(body, &h) != nil || !h.OK || h.Version == "" {
		return errNotSTKR
	}
	return nil
}

// Status fetches GET /api/status.
func (c *Client) Status(ctx context.Context) (Status, error) {
	ctx, cancel := context.WithTimeout(ctx, StatusTimeout)
	defer cancel()
	var st Status
	status, body, err := c.do(ctx, http.MethodGet, "/api/status")
	if err != nil {
		return st, err
	}
	if status != http.StatusOK {
		return st, apiError(status, body)
	}
	if err := json.Unmarshal(body, &st); err != nil {
		return st, fmt.Errorf("decode status: %w", err)
	}
	return st, nil
}

// Post calls a POST route and returns the JSON body of a 2xx answer. Other
// answers become an *APIError with the server's message.
func (c *Client) Post(ctx context.Context, path string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, ActionTimeout)
	defer cancel()
	status, body, err := c.do(ctx, http.MethodPost, path)
	if err != nil {
		return nil, err
	}
	if status < 200 || status > 299 {
		return nil, apiError(status, body)
	}
	out := map[string]any{}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, fmt.Errorf("decode answer: %w", err)
		}
	}
	return out, nil
}

func (c *Client) do(ctx context.Context, method, path string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.URL(path), nil)
	if err != nil {
		return 0, nil, err
	}
	req.Host = "127.0.0.1:" + strconv.Itoa(c.port)
	if method != http.MethodGet {
		req.Header.Set("Origin", c.BaseURL())
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

func apiError(status int, body []byte) error {
	var b struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &b)
	return &APIError{Status: status, Code: b.Error, Message: b.Message}
}
