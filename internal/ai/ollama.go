package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Ollama talks to an Ollama server over HTTP (POST /api/chat).
type Ollama struct {
	URL    string
	Model  string
	Client *http.Client
}

// NewOllama validates the URL (http or https with a host).
func NewOllama(rawURL, model string) (*Ollama, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, errors.New("ollama.url must be an http or https URL")
	}
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("ollama.model is empty")
	}
	return &Ollama{URL: strings.TrimRight(u.String(), "/"), Model: model}, nil
}

// Name implements Backend.
func (o *Ollama) Name() string { return "ollama" }

// Local is true when the server address is this computer. A remote Ollama
// is treated like any other cloud provider.
func (o *Ollama) Local() bool {
	u, err := url.Parse(o.URL)
	if err != nil {
		return false
	}
	h := u.Hostname()
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func (o *Ollama) client() *http.Client {
	if o.Client != nil {
		return o.Client
	}
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (o *Ollama) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, o.URL+path, rd)
	if err != nil {
		return nil, fmt.Errorf("%w: bad request", ErrUnreachable)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := o.client().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w: %w", ErrTimeout, ctx.Err())
		}
		return nil, fmt.Errorf("%w: cannot connect", ErrUnreachable)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxOutputBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w: %w", ErrTimeout, ctx.Err())
		}
		return nil, fmt.Errorf("%w: read failed", ErrUnreachable)
	}
	if len(data) > MaxOutputBytes {
		return nil, invalidf("response is larger than 1 MB")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: HTTP %d", ErrUnreachable, resp.StatusCode)
	}
	return data, nil
}

// Complete implements Backend.
func (o *Ollama) Complete(ctx context.Context, prompt string) (string, error) {
	req := map[string]any{
		"model":    o.Model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
		"format":   "json",
		"think":    false,
		"stream":   false,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	data, err := o.do(ctx, http.MethodPost, "/api/chat", body)
	if err != nil {
		return "", err
	}
	var resp struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(data, &resp); err != nil || resp.Message.Content == "" {
		return "", invalidf("no message content")
	}
	return resp.Message.Content, nil
}

// Probe implements Backend using GET /api/version.
func (o *Ollama) Probe(ctx context.Context) (string, error) {
	data, err := o.do(ctx, http.MethodGet, "/api/version", nil)
	if err != nil {
		return "", err
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return "", invalidf("unexpected version answer")
	}
	return v.Version, nil
}
