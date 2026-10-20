package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client calls the imapsync web API with an API key. It knows nothing about
// MCP; the tools in tools.go decide what to ask it.
type Client struct {
	base string // ends in /api, no trailing slash
	key  string
	http *http.Client
}

// NewClient accepts the site's address with or without /api, so the URL a
// user copies from the browser works as given.
func NewClient(baseURL, key string) *Client {
	base := strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(base, "/api") {
		base += "/api"
	}
	return &Client{
		base: base,
		key:  key,
		// Above the server's longest hold: GET /sync/{id}?wait= answers within
		// 120 seconds.
		http: &http.Client{Timeout: 150 * time.Second},
	}
}

// APIError is a non-2xx answer. The server's own message is kept, since it is
// written for a person and usually says what to change.
type APIError struct {
	Status int
	Detail string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("imapsync API answered %d: %s", e.Status, e.Detail)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Detail string `json:"detail"`
		}
		if json.Unmarshal(raw, &e) != nil || e.Detail == "" {
			e.Detail = strings.TrimSpace(string(raw))
		}
		return &APIError{Status: resp.StatusCode, Detail: e.Detail}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}
