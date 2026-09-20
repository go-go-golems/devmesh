package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Client is a small JSON client that always dials the devmesh Unix socket
// regardless of the request URL host.
type Client struct {
	socket string
	hc     *http.Client
}

// NewClient builds a client for socketPath with the given per-request timeout.
func NewClient(socketPath string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	dialer := &net.Dialer{Timeout: timeout}
	return &Client{
		socket: socketPath,
		hc: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return dialer.DialContext(ctx, "unix", socketPath)
				},
			},
		},
	}
}

// HTTPClient exposes the underlying *http.Client (for the raw doctor checks).
func (c *Client) HTTPClient() *http.Client { return c.hc }

// Do performs a JSON request. in is marshalled as the body when non-nil; out is
// decoded from the response when non-nil. Non-2xx responses are returned as an
// *Error carrying the API error envelope when present.
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	return c.do(ctx, method, path, "", in, out)
}

// DoAuth is Do with a bearer token for lease-authorized endpoints.
func (c *Client) DoAuth(ctx context.Context, method, path, token string, in, out any) error {
	return c.do(ctx, method, path, token, in, out)
}

func (c *Client) do(ctx context.Context, method, path, token string, in, out any) error {
	var body io.Reader
	if in != nil {
		buf, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://devmesh"+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("devmesh daemon unreachable at %s: %w", c.socket, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeError(resp.StatusCode, data)
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Error is a decoded API error envelope.
type Error struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

func decodeError(status int, data []byte) error {
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &env); err == nil && env.Error.Code != "" {
		return &Error{Status: status, Code: env.Error.Code, Message: env.Error.Message}
	}
	return &Error{Status: status, Code: "internal_error", Message: string(data)}
}
