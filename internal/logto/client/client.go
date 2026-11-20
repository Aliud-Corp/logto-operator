// Package client is the only door to Logto's Management API. Kinds build their requests with the generated request
// builders of internal/logto/mgmt (routes and parameters) and their own wire types (bodies); this package adds the
// token, bounded retries, pagination and errors that never carry a request body.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ManagementResource is the API resource of the default tenant's Management API.
const ManagementResource = "https://default.logto.app/api"

// Config of a client. ClientSecret is used for the token request only and never appears in an error.
type Config struct {
	Endpoint      string // tenant endpoint
	TokenEndpoint string // where the token is obtained; the tenant endpoint when empty
	ClientID      string
	ClientSecret  string
	HTTP          *http.Client
	MaxRetries    int           // retries of a 5xx or 429 answer; 3 when zero
	RetryBase     time.Duration // first backoff, doubled each retry; 200ms when zero
	Timeout       time.Duration // per call; 30s when zero
}

// Client calls the Management API with a valid token.
type Client struct {
	cfg  Config
	http *http.Client

	mu     sync.Mutex
	token  string
	expiry time.Time
}

// New returns a client; no call is made until the first request.
func New(cfg Config) *Client {
	cfg.Endpoint = strings.TrimRight(cfg.Endpoint, "/")
	cfg.TokenEndpoint = strings.TrimRight(cfg.TokenEndpoint, "/")
	if cfg.TokenEndpoint == "" {
		cfg.TokenEndpoint = cfg.Endpoint
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryBase == 0 {
		cfg.RetryBase = 200 * time.Millisecond
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	h := cfg.HTTP
	if h == nil {
		h = &http.Client{}
	}
	return &Client{cfg: cfg, http: h}
}

// Endpoint is the server URL to give the generated request builders.
func (c *Client) Endpoint() string { return c.cfg.Endpoint }

// Token returns a valid access token, refreshing it ahead of its expiry.
func (c *Client) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.expiry) {
		return c.token, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}, "resource": {ManagementResource}, "scope": {"all"}}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenEndpoint+"/oidc/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.cfg.ClientID, c.cfg.ClientSecret)
	res, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request to %s: %w", c.cfg.TokenEndpoint, err)
	}
	defer func() { _ = res.Body.Close() }()
	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body)
	if res.StatusCode != http.StatusOK || body.AccessToken == "" {
		return "", &AuthError{TokenEndpoint: c.cfg.TokenEndpoint, ClientID: c.cfg.ClientID, Status: res.StatusCode, Code: body.Error}
	}
	ttl := time.Duration(body.ExpiresIn) * time.Second
	margin := min(30*time.Second, ttl/10)
	c.token, c.expiry = body.AccessToken, time.Now().Add(ttl-margin)
	return c.token, nil
}

func (c *Client) forgetToken() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = ""
}

// Do sends a request built by a generated request builder, with the token, and decodes a 2xx JSON answer into out
// (when out is not nil). A 5xx or 429 is retried with backoff when the body can be replayed; a 401 refreshes the
// token once. Any other non-2xx is an *APIError.
func (c *Client) Do(ctx context.Context, req *http.Request, out any) error {
	refreshed := false
	for attempt := 0; ; attempt++ {
		res, err := c.send(ctx, req)
		if err != nil {
			return err
		}
		body, readErr := io.ReadAll(io.LimitReader(res.Body, 32<<20))
		_ = res.Body.Close()
		if readErr != nil {
			return fmt.Errorf("%s %s: read answer: %w", req.Method, req.URL.Path, readErr)
		}
		switch {
		case res.StatusCode >= 200 && res.StatusCode < 300:
			if out == nil || len(bytes.TrimSpace(body)) == 0 {
				return nil
			}
			if err := json.Unmarshal(body, out); err != nil {
				return fmt.Errorf("%s %s: decode answer: %w", req.Method, req.URL.Path, err)
			}
			return nil
		case res.StatusCode == http.StatusUnauthorized && !refreshed:
			refreshed = true
			c.forgetToken()
		case (res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests) && attempt < c.cfg.MaxRetries && rewindable(req):
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.cfg.RetryBase << attempt):
			}
		default:
			return newAPIError(req, res.StatusCode, body)
		}
		if err := rewind(req); err != nil {
			return err
		}
	}
}

func (c *Client) send(ctx context.Context, req *http.Request) (*http.Response, error) {
	token, err := c.Token(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	r := req.Clone(ctx)
	r.Header.Set("Authorization", "Bearer "+token)
	if r.Body != nil && r.Header.Get("Content-Type") == "" {
		r.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(r)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("%s %s: %w", req.Method, req.URL.Path, err)
	}
	res.Body = cancelOnClose{ReadCloser: res.Body, cancel: cancel}
	return res, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

func rewindable(req *http.Request) bool { return req.Body == nil || req.GetBody != nil }

func rewind(req *http.Request) error {
	if req.Body == nil || req.GetBody == nil {
		return nil
	}
	b, err := req.GetBody()
	if err != nil {
		return err
	}
	req.Body = b
	return nil
}

// JSON encodes a wire value as a request body the client can replay on a retry.
func JSON(v any) (io.Reader, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}

// PageSize is the largest page Logto accepts.
const PageSize = 100

// List reads every page of a Logto list (page size 100, until a short page). build makes the request of one page,
// usually with a generated builder.
func List[T any](ctx context.Context, c *Client, build func(page, size int) (*http.Request, error)) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		req, err := build(page, PageSize)
		if err != nil {
			return nil, err
		}
		var items []T
		if err := c.Do(ctx, req, &items); err != nil {
			return nil, err
		}
		all = append(all, items...)
		if len(items) < PageSize {
			return all, nil
		}
	}
}

// APIError is a non-2xx answer of the Management API: the call and Logto's error code and message, never the body
// that was sent.
type APIError struct {
	Method, Path string
	Status       int
	Code         string
	Message      string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("%s %s: Logto answered %d", e.Method, e.Path, e.Status)
	if e.Code != "" {
		msg += " " + e.Code
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

func newAPIError(req *http.Request, status int, body []byte) *APIError {
	var b struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &b)
	if len(b.Message) > 300 {
		b.Message = b.Message[:300] + "..."
	}
	return &APIError{Method: req.Method, Path: req.URL.Path, Status: status, Code: b.Code, Message: b.Message}
}

// AuthError is a refused token request.
type AuthError struct {
	TokenEndpoint, ClientID string
	Status                  int
	Code                    string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("no Management API token for client %q at %s/oidc/token (%d %s): check the credentials",
		e.ClientID, e.TokenEndpoint, e.Status, e.Code)
}

// IsStatus reports whether err is an *APIError with that status.
func IsStatus(err error, status int) bool {
	var e *APIError
	return errors.As(err, &e) && e.Status == status
}
