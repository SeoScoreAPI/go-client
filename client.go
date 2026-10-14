// Package seoscoreapi is the Go client for SEO Score API (https://seoscoreapi.com):
// SEO audits, AI readability, accessibility, GEO, site crawls, AI citation tracking
// and Deep Site Audits, as typed Go calls.
//
//	client := seoscoreapi.New() // reads SEOSCORE_API_KEY
//	result, err := client.Audit(ctx, "https://example.com", nil)
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Printf("Score: %.1f/100 (%s)\n", result.Score, result.Grade)
//
// Every call takes a context.Context. Rate-limited requests (429 with Retry-After)
// and temporary server errors are retried with capped backoff; other failures are
// returned as *APIError, which matches the sentinel errors (ErrRateLimited,
// ErrQuotaExceeded, ErrUnauthorized, ...) with errors.Is.
//
// Full API docs: https://seoscoreapi.com/docs
package seoscoreapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Version is this client's version, sent in the User-Agent header.
const Version = "1.0.0"

// DefaultBaseURL is the API's address. Deep Site Audits are served on the same host.
const DefaultBaseURL = "https://seoscoreapi.com"

// EngineURL is the legacy dedicated Deep Audit host. It still serves the same
// Deep Audit endpoints; pass it to WithDeepAuditBaseURL if you need it.
const EngineURL = "https://engine.seoscoreapi.com"

// APIKeyEnv is the environment variable New reads the API key from when no
// WithAPIKey option is given.
const APIKeyEnv = "SEOSCORE_API_KEY"

const userAgent = "seoscoreapi-go/" + Version

// Client calls SEO Score API. It is safe for concurrent use. Create one with New
// and reuse it.
type Client struct {
	apiKey      string
	baseURL     string
	deepBaseURL string
	http        *http.Client
	maxRetries  int
	maxBackoff  time.Duration
	userAgent   string
	sleep       func(ctx context.Context, d time.Duration) error
}

// Option configures a Client.
type Option func(*Client)

// WithAPIKey sets the API key. Without it, New reads SEOSCORE_API_KEY.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = strings.TrimSpace(key) } }

// WithBaseURL points the client at another API host (for tests or a proxy).
// Deep Site Audit calls follow it unless WithDeepAuditBaseURL is also given.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

// WithDeepAuditBaseURL sends the Deep Site Audit calls to another host, such as EngineURL.
func WithDeepAuditBaseURL(u string) Option {
	return func(c *Client) { c.deepBaseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient uses your own *http.Client (transport, proxy, TLS settings).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithTimeout sets the timeout of each HTTP request (default 120s: an audit of a
// slow, script-heavy page can take a while). A deadline on the context still applies.
// Apply it after WithHTTPClient if you use both.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		h := *c.http
		h.Timeout = d
		c.http = &h
	}
}

// WithRetries sets how many times a retryable failure is retried (default 3; 0 turns
// retries off). See Client for what is retried.
func WithRetries(n int) Option {
	return func(c *Client) {
		if n < 0 {
			n = 0
		}
		c.maxRetries = n
	}
}

// WithMaxBackoff caps the wait between two attempts, including a server's
// Retry-After (default 60s). A longer Retry-After is returned as an error instead.
func WithMaxBackoff(d time.Duration) Option { return func(c *Client) { c.maxBackoff = d } }

// WithUserAgent appends your application's name to the User-Agent header,
// for example "myapp/2.1".
func WithUserAgent(product string) Option {
	return func(c *Client) {
		if product = strings.TrimSpace(product); product != "" {
			c.userAgent = userAgent + " " + product
		}
	}
}

// New returns a Client. The API key comes from WithAPIKey or, failing that, from
// the SEOSCORE_API_KEY environment variable. Signup, Verify, ReportURL and
// GenerateLlmsTxt work without one.
//
// Retries: a 429 rate limit (a per-minute limit or a full queue; the request was
// refused, so nothing was counted), waiting as long as its Retry-After says, a 502, 503 or 504, and
// a network error are retried with exponential backoff and jitter, up to
// WithRetries times. POST and PATCH requests, which can start work, are retried
// only on a 429 rate limit or a 503. A quota that is used up (ErrQuotaExceeded) is
// never retried.
func New(opts ...Option) *Client {
	c := &Client{
		apiKey:     strings.TrimSpace(os.Getenv(APIKeyEnv)),
		baseURL:    DefaultBaseURL,
		http:       &http.Client{Timeout: 120 * time.Second},
		maxRetries: 3,
		maxBackoff: 60 * time.Second,
		userAgent:  userAgent,
		sleep:      sleepCtx,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) deepBase() string {
	if c.deepBaseURL != "" {
		return c.deepBaseURL
	}
	return c.baseURL
}

// request describes one API call.
type request struct {
	method string
	base   string // defaults to the client's base URL
	path   string
	query  url.Values
	body   any
	auth   bool // send X-API-Key (required unless optionalAuth)
	// optionalAuth sends the key when there is one and never fails without it.
	optionalAuth bool
}

// do sends the request with retries and returns the response body.
func (c *Client) do(ctx context.Context, r request) ([]byte, http.Header, error) {
	if r.auth && !r.optionalAuth && c.apiKey == "" {
		return nil, nil, ErrNoAPIKey
	}
	var payload []byte
	if r.body != nil {
		var err error
		if payload, err = json.Marshal(r.body); err != nil {
			return nil, nil, fmt.Errorf("seoscoreapi: encoding request: %w", err)
		}
	}
	base := r.base
	if base == "" {
		base = c.baseURL
	}
	target := base + r.path
	if len(r.query) > 0 {
		target += "?" + r.query.Encode()
	}
	starts := r.method == http.MethodPost || r.method == http.MethodPatch

	for attempt := 0; ; attempt++ {
		body, header, err := c.once(ctx, r, target, payload)
		if err == nil {
			return body, header, nil
		}
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		wait, retry := c.retryable(err, starts, attempt)
		if !retry || attempt >= c.maxRetries {
			return nil, nil, err
		}
		if err := c.sleep(ctx, wait); err != nil {
			return nil, nil, err
		}
	}
}

func (c *Client) once(ctx context.Context, r request, target string, payload []byte) ([]byte, http.Header, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, target, reader)
	if err != nil {
		return nil, nil, fmt.Errorf("seoscoreapi: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if (r.auth || r.optionalAuth) && c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, &netError{err: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, &netError{err: err}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return body, resp.Header, nil
	}
	return nil, nil, newAPIError(resp, body)
}

// retryable says whether err is worth another attempt, and after how long.
func (c *Client) retryable(err error, starts bool, attempt int) (time.Duration, bool) {
	backoff := time.Duration(1<<attempt) * time.Second
	backoff += time.Duration(rand.Int63n(int64(backoff) / 2))
	if backoff > c.maxBackoff {
		backoff = c.maxBackoff
	}
	var ne *netError
	if errors.As(err, &ne) {
		return backoff, !starts
	}
	var ae *APIError
	if !errors.As(err, &ae) {
		return 0, false
	}
	switch {
	case ae.StatusCode == http.StatusTooManyRequests && ae.RetryAfter > 0:
		if ae.RetryAfter > c.maxBackoff {
			return 0, false
		}
		return ae.RetryAfter, true
	case errors.Is(ae, ErrRateLimited):
		// A per-minute limit without Retry-After: the window is a minute long.
		wait := time.Duration(attempt+1) * 15 * time.Second
		if wait > c.maxBackoff {
			wait = c.maxBackoff
		}
		return wait, true
	case ae.StatusCode == http.StatusServiceUnavailable:
		if ae.RetryAfter > 0 && ae.RetryAfter <= c.maxBackoff {
			return ae.RetryAfter, true
		}
		return backoff, true
	case ae.StatusCode == http.StatusBadGateway || ae.StatusCode == http.StatusGatewayTimeout:
		return backoff, !starts
	}
	return 0, false
}

type netError struct{ err error }

func (e *netError) Error() string { return "seoscoreapi: " + e.err.Error() }
func (e *netError) Unwrap() error { return e.err }

// getJSON, sendJSON: a call whose answer is decoded into out.
func (c *Client) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	return c.call(ctx, request{method: http.MethodGet, path: path, query: q, auth: true}, out)
}

func (c *Client) sendJSON(ctx context.Context, method, path string, body, out any) error {
	return c.call(ctx, request{method: method, path: path, body: body, auth: true}, out)
}

func (c *Client) call(ctx context.Context, r request, out any) error {
	body, _, err := c.do(ctx, r)
	if err != nil {
		return err
	}
	return decode(body, out)
}

func decode(body []byte, out any) error {
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("seoscoreapi: decoding response: %w", err)
	}
	if r, ok := out.(rawSetter); ok {
		r.setRaw(body)
	}
	return nil
}

// rawSetter is implemented by result types that keep the full response.
type rawSetter interface{ setRaw([]byte) }

// raw is embedded in typed results: Raw holds the whole response, so a field this
// client does not model yet is still there.
type raw struct {
	// Raw is the complete JSON response as the API sent it.
	Raw json.RawMessage `json:"-"`
}

func (r *raw) setRaw(b []byte) { r.Raw = append(json.RawMessage(nil), b...) }

// Object is a JSON object returned by an endpoint this client does not model
// field by field.
type Object map[string]any

func values(pairs ...string) url.Values {
	q := url.Values{}
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			q.Set(pairs[i], pairs[i+1])
		}
	}
	return q
}

func itoa(n int) string { return strconv.Itoa(n) }

func pathID(id int64) string { return strconv.FormatInt(id, 10) }

// escape is a path segment, escaped.
func escape(s string) string { return url.PathEscape(s) }
