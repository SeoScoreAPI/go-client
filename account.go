package seoscoreapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

// SignupResult is the answer of Signup. No key is issued yet: the API emailed a
// 6-digit code; pass it to Verify.
type SignupResult struct {
	raw
	Status  string `json:"status"` // "verification_required"
	Message string `json:"message,omitempty"`
}

// Signup starts signup for a free API key: the API emails a 6-digit code to email
// (valid 10 minutes). It needs no key. An address that already has an active key
// gets a 409 *APIError.
func (c *Client) Signup(ctx context.Context, email string) (*SignupResult, error) {
	var out SignupResult
	err := c.call(ctx, request{method: http.MethodPost, path: "/signup", body: map[string]string{"email": email}}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Verify finishes signup: it exchanges the emailed code for the API key, which is
// shown only once, so store it. Five wrong codes void the signup.
func (c *Client) Verify(ctx context.Context, email, code string) (string, error) {
	var out struct {
		APIKey string `json:"api_key"`
	}
	body := map[string]string{"email": email, "code": strings.TrimSpace(code)}
	if err := c.call(ctx, request{method: http.MethodPost, path: "/verify", body: body}, &out); err != nil {
		return "", err
	}
	return out.APIKey, nil
}

// Limits are a plan's limits. A nil pointer means no limit of that kind.
type Limits struct {
	Daily         *int `json:"daily"`
	Monthly       *int `json:"monthly"`
	RPM           int  `json:"rpm"`
	GeoMonthly    *int `json:"geo_monthly,omitempty"`
	ADAMonthly    *int `json:"ada_monthly,omitempty"`
	ADAPerMinute  *int `json:"ada_per_minute,omitempty"`
	ADAConcurrent *int `json:"ada_concurrent,omitempty"`
}

// CitationUsage is the month's AI citation checks.
type CitationUsage struct {
	ChecksLimit          int `json:"checks_limit"`
	ChecksUsed           int `json:"checks_used"`
	ChecksRemaining      int `json:"checks_remaining"`
	TopupChecksRemaining int `json:"topup_checks_remaining"`
	ChecksAvailable      int `json:"checks_available"`
	Trackers             int `json:"trackers"`
	PromptsTracked       int `json:"prompts_tracked"`
	PromptsLimit         int `json:"prompts_limit"`
}

// Usage is the answer of Usage: the key's plan, what it has used and its limits.
type Usage struct {
	raw
	Tier              string         `json:"tier"`
	TierName          string         `json:"tier_name"`
	UsageToday        int            `json:"usage_today"`
	UsageThisMonth    int            `json:"usage_this_month"`
	UsageTotal        int            `json:"usage_total"`
	Limits            Limits         `json:"limits"`
	ADAUsageThisMonth int            `json:"ada_usage_this_month"`
	ADARemaining      *int           `json:"ada_remaining,omitempty"`
	Citations         *CitationUsage `json:"citations,omitempty"`
}

// Usage returns the key's plan, usage and limits. It is not metered.
func (c *Client) Usage(ctx context.Context) (*Usage, error) {
	var out Usage
	if err := c.getJSON(ctx, "/usage", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// MonitorOptions are optional settings for AddMonitor.
type MonitorOptions struct {
	Frequency string // "daily" (default) or "weekly"
	// WebhookURL receives a POST when the score drops by AlertThreshold points or
	// more. Slack incoming-webhook URLs get a formatted message.
	WebhookURL     string
	AlertThreshold int // points; 0 uses 5
}

// AddMonitor sets up score monitoring for a URL (paid plans).
func (c *Client) AddMonitor(ctx context.Context, pageURL string, opts *MonitorOptions) (Object, error) {
	if opts == nil {
		opts = &MonitorOptions{}
	}
	body := map[string]any{"url": pageURL, "frequency": orDefault(opts.Frequency, "daily"),
		"alert_threshold": intOr(opts.AlertThreshold, 5)}
	if opts.WebhookURL != "" {
		body["webhook_url"] = opts.WebhookURL
	}
	var out Object
	return out, c.sendJSON(ctx, http.MethodPost, "/monitors", body, &out)
}

// ListMonitors lists the key's active monitors.
func (c *Client) ListMonitors(ctx context.Context) ([]Object, error) {
	var out struct {
		Monitors []Object `json:"monitors"`
	}
	if err := c.getJSON(ctx, "/monitors", nil, &out); err != nil {
		return nil, err
	}
	return out.Monitors, nil
}

// RemoveMonitor removes the monitor for a URL.
func (c *Client) RemoveMonitor(ctx context.Context, pageURL string) (Object, error) {
	var out Object
	return out, c.sendJSON(ctx, http.MethodDelete, "/monitors", map[string]string{"url": pageURL}, &out)
}

// ScoreboardOptOut opts the key's audits out of (true) or back into (false) the
// public scoreboard.
func (c *Client) ScoreboardOptOut(ctx context.Context, optOut bool) (Object, error) {
	var out Object
	r := request{method: http.MethodPut, path: "/scoreboard/opt-out",
		query: values("opt_out", strconv.FormatBool(optOut)), auth: true}
	return out, c.call(ctx, r, &out)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func intOr(n, def int) int {
	if n == 0 {
		return def
	}
	return n
}
