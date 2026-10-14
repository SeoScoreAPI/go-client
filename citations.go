package seoscoreapi

import (
	"context"
	"net/http"
	"strings"
)

// AI citation tracking (Starter plan or higher). A check is one prompt on one
// engine, sampled once. Every plan has a monthly allowance of checks; top-up checks
// are spent only after it.

// StarterPromptOptions are optional settings for CitationStarterPrompts.
type StarterPromptOptions struct {
	Brand       string
	Competitors []string // up to 5
	Audience    string
	Features    []string // up to 6
	UseCases    []string // up to 6
	PerCategory int      // 1-10; the API's default is 2
	Limit       int      // 1-60
}

// CitationStarterPrompts builds a starter set of prompts to track from fixed
// templates. It works on every plan, is not metered and spends no check.
func (c *Client) CitationStarterPrompts(ctx context.Context, topic string, opts *StarterPromptOptions) (Object, error) {
	if opts == nil {
		opts = &StarterPromptOptions{}
	}
	q := values("topic", topic, "brand", opts.Brand, "competitors", strings.Join(opts.Competitors, ","),
		"audience", opts.Audience, "features", strings.Join(opts.Features, ","),
		"use_cases", strings.Join(opts.UseCases, ","))
	if opts.PerCategory > 0 {
		q.Set("per_category", itoa(opts.PerCategory))
	}
	if opts.Limit > 0 {
		q.Set("limit", itoa(opts.Limit))
	}
	var out Object
	return out, c.getJSON(ctx, "/citations/prompts/starter", q, &out)
}

// CitationSuggestPrompts suggests prompts to track, read off what the site says it does.
func (c *Client) CitationSuggestPrompts(ctx context.Context, domain string) (Object, error) {
	var out Object
	return out, c.getJSON(ctx, "/citations/prompts/suggest", values("domain", domain), &out)
}

// CitationTracker is the request of CreateCitationTracker and UpdateCitationTracker.
// Only the fields you set are sent.
type CitationTracker struct {
	Brand       string   `json:"brand,omitempty"` // required when creating
	Domains     []string `json:"domains,omitempty"`
	Prompts     []string `json:"prompts,omitempty"`
	Competitors []string `json:"competitors,omitempty"`
	Engines     []string `json:"engines,omitempty"` // chatgpt, gemini, perplexity, claude
	Samples     int      `json:"samples,omitempty"`
	Cadence     string   `json:"cadence,omitempty"` // daily, weekly, monthly or manual
	WebhookURL  string   `json:"webhook_url,omitempty"`
	// Starter builds the prompts from a topic when Prompts is empty (create only),
	// for example {"topic": "...", "audience": "..."}.
	Starter map[string]any `json:"starter,omitempty"`
}

// CreateCitationTracker starts tracking a brand's visibility in AI answers.
func (c *Client) CreateCitationTracker(ctx context.Context, t CitationTracker) (Object, error) {
	var out Object
	return out, c.sendJSON(ctx, http.MethodPost, "/citations/trackers", t, &out)
}

// ListCitationTrackers lists the key's trackers and the month's check usage
// ({"trackers": [...], "usage": {...}}).
func (c *Client) ListCitationTrackers(ctx context.Context) (Object, error) {
	var out Object
	return out, c.getJSON(ctx, "/citations/trackers", nil, &out)
}

// GetCitationTracker reads one tracker.
func (c *Client) GetCitationTracker(ctx context.Context, trackerID int64) (Object, error) {
	var out Object
	return out, c.getJSON(ctx, "/citations/trackers/"+pathID(trackerID), nil, &out)
}

// UpdateCitationTracker changes a tracker's prompts, engines, competitors,
// samples, cadence or webhook URL; only the fields you set change.
func (c *Client) UpdateCitationTracker(ctx context.Context, trackerID int64, t CitationTracker) (Object, error) {
	var out Object
	return out, c.sendJSON(ctx, http.MethodPatch, "/citations/trackers/"+pathID(trackerID), t, &out)
}

// DeleteCitationTracker stops tracking a brand. Run history is kept until its
// retention expires.
func (c *Client) DeleteCitationTracker(ctx context.Context, trackerID int64) (Object, error) {
	var out Object
	return out, c.sendJSON(ctx, http.MethodDelete, "/citations/trackers/"+pathID(trackerID), nil, &out)
}

// RunCitationTracker runs a tracker now and returns the run summary. Every prompt
// x engine x sample is one check, all needed up front: when the checks available
// are short it is refused (ErrQuotaExceeded) and nothing is spent. The request
// stays open while the engines answer.
func (c *Client) RunCitationTracker(ctx context.Context, trackerID int64) (Object, error) {
	var out Object
	return out, c.sendJSON(ctx, http.MethodPost, "/citations/trackers/"+pathID(trackerID)+"/run", nil, &out)
}

// GetCitationRun returns a run's status and every per-prompt, per-engine result.
func (c *Client) GetCitationRun(ctx context.Context, runID int64) (Object, error) {
	var out Object
	return out, c.getJSON(ctx, "/citations/runs/"+pathID(runID), nil, &out)
}

// CitationTrackerHistory returns the mention rate, citation rate, share of voice
// and average position over time. days is 1-365; 0 uses 90.
func (c *Client) CitationTrackerHistory(ctx context.Context, trackerID int64, days int) (Object, error) {
	var out Object
	return out, c.getJSON(ctx, "/citations/trackers/"+pathID(trackerID)+"/history",
		values("days", itoa(intOr(days, 90))), &out)
}

// CitationCheck is the request of CitationCheckOnce.
type CitationCheck struct {
	Brand       string   `json:"brand"`
	Prompt      string   `json:"prompt"`
	Domains     []string `json:"domains,omitempty"`
	Engines     []string `json:"engines,omitempty"`
	Samples     int      `json:"samples,omitempty"` // the API's default is 1
	Competitors []string `json:"competitors,omitempty"`
}

// CitationCheckOnce runs a one-off citation check with no tracker, metered per
// check (paid plans).
func (c *Client) CitationCheckOnce(ctx context.Context, check CitationCheck) (Object, error) {
	var out Object
	return out, c.sendJSON(ctx, http.MethodPost, "/citations/check", check, &out)
}

// CitationTopups lists the top-up packs for citation checks and this account's balance.
func (c *Client) CitationTopups(ctx context.Context) (Object, error) {
	var out Object
	return out, c.getJSON(ctx, "/citations/topups", nil, &out)
}

// CitationTopupCheckout starts a one-time Stripe Checkout for a top-up pack (the
// "key" of one of CitationTopups' packs) and returns the checkout URL to open.
// Nothing is charged by this call; the checks are added when the payment completes.
func (c *Client) CitationTopupCheckout(ctx context.Context, pack string) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	err := c.sendJSON(ctx, http.MethodPost, "/citations/topups/checkout", map[string]string{"pack": pack}, &out)
	return out.URL, err
}

// CitationAutoReload reads the auto-reload setting for citation checks and this
// month's use. It is read-only by design: auto-reload authorizes charges, so it is
// changed only in the dashboard, never with an API key.
func (c *Client) CitationAutoReload(ctx context.Context) (Object, error) {
	var out Object
	return out, c.getJSON(ctx, "/citations/auto-reload", nil, &out)
}
