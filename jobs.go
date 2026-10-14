package seoscoreapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Job states, for CrawlJob.Status and DeepAuditStatus.Status.
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

// ErrJobFailed is matched (errors.Is) by the *JobError a wait returns when the job
// itself failed.
var ErrJobFailed = errors.New("seoscoreapi: job failed")

// JobError is a crawl or Deep Site Audit that ended in "failed".
type JobError struct {
	JobID   string
	Message string
}

func (e *JobError) Error() string {
	return fmt.Sprintf("seoscoreapi: job %s failed: %s", e.JobID, e.Message)
}

// Is matches ErrJobFailed.
func (e *JobError) Is(target error) bool { return target == ErrJobFailed }

// WaitOptions control how a Wait or a start-and-wait call polls. Cancel or set a
// deadline on the context to stop waiting; the job itself keeps running.
type WaitOptions struct {
	// PollInterval is the wait between two polls (default 5s).
	PollInterval time.Duration
	// OnProgress, if set, is called with every status read.
	OnProgress func(status any)
}

func (w *WaitOptions) interval() time.Duration {
	if w == nil || w.PollInterval <= 0 {
		return 5 * time.Second
	}
	return w.PollInterval
}

func (w *WaitOptions) progress(status any) {
	if w != nil && w.OnProgress != nil {
		w.OnProgress(status)
	}
}

// ── Site crawl (Pro/Ultra, or a Deep Audit credit) ─────────────────────────────

// CrawlJob is a multi-page site crawl. Result is set once Status is "completed"
// (summary, issues, pages and links); Error once it is "failed".
type CrawlJob struct {
	raw
	JobID     string          `json:"job_id"`
	Status    string          `json:"status"`
	URL       string          `json:"url,omitempty"`
	MaxPages  int             `json:"max_pages,omitempty"`
	PagesDone int             `json:"pages_done,omitempty"`
	SiteMap   string          `json:"site_map,omitempty"` // shareable diagram page
	Poll      string          `json:"poll,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// StartCrawl starts a crawl of one site, following same-site links and the sitemap
// up to the plan's page cap. maxPages (1-100) asks for fewer; 0 uses the cap. It
// returns at once; poll with GetCrawl or block with WaitForCrawl. A failed crawl
// is not counted.
func (c *Client) StartCrawl(ctx context.Context, siteURL string, maxPages int) (*CrawlJob, error) {
	body := map[string]any{"url": siteURL}
	if maxPages > 0 {
		body["max_pages"] = maxPages
	}
	var out CrawlJob
	if err := c.sendJSON(ctx, http.MethodPost, "/crawl", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCrawl reads a crawl's status. Only the key that started it can read it.
func (c *Client) GetCrawl(ctx context.Context, jobID string) (*CrawlJob, error) {
	var out CrawlJob
	if err := c.getJSON(ctx, "/crawl/"+escape(jobID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WaitForCrawl polls a crawl until it completes and returns the completed job. A
// failed crawl returns a *JobError; a cancelled or expired ctx returns ctx.Err().
func (c *Client) WaitForCrawl(ctx context.Context, jobID string, wait *WaitOptions) (*CrawlJob, error) {
	for {
		job, err := c.GetCrawl(ctx, jobID)
		if err != nil {
			return nil, err
		}
		wait.progress(job)
		switch job.Status {
		case StatusCompleted:
			return job, nil
		case StatusFailed:
			return nil, &JobError{JobID: jobID, Message: orDefault(job.Error, "crawl failed")}
		}
		if err := c.sleep(ctx, wait.interval()); err != nil {
			return nil, err
		}
	}
}

// Crawl starts a crawl and waits for it to complete.
func (c *Client) Crawl(ctx context.Context, siteURL string, maxPages int, wait *WaitOptions) (*CrawlJob, error) {
	job, err := c.StartCrawl(ctx, siteURL, maxPages)
	if err != nil {
		return nil, err
	}
	return c.WaitForCrawl(ctx, job.JobID, wait)
}

// ── Deep Site Audit (Pro/Ultra, or credits) ────────────────────────────────────

// DeepAuditOptions are optional settings for StartDeepAudit and DeepAudit.
type DeepAuditOptions struct {
	// BusinessType tunes which checks apply: saas, local_service, ecommerce,
	// storefront, blog or publisher.
	BusinessType string `json:"business_type,omitempty"`
	// IsLocal forces the local-business checks (NAP, LocalBusiness schema).
	IsLocal *bool `json:"is_local,omitempty"`
	// WebhookURL receives one POST of {"job_id", "status", "result"} when the job
	// completes. It is a single unsigned attempt with no retries, and nothing is
	// sent for a failed job, so keep polling as the source of truth.
	WebhookURL string `json:"webhook_url,omitempty"`
}

// DeepAuditJob is the answer of StartDeepAudit.
type DeepAuditJob struct {
	raw
	JobID  string `json:"job_id"`
	Status string `json:"status"`
	Poll   string `json:"poll"`
}

// DeepAuditStatus is a Deep Site Audit job's state: QueuePosition and ETASeconds
// while queued, Progress and Stage while running, Result when completed, Error when
// failed.
type DeepAuditStatus struct {
	raw
	JobID         string          `json:"job_id"`
	Status        string          `json:"status"`
	Progress      float64         `json:"progress"`
	Stage         string          `json:"stage,omitempty"`
	QueuePosition int             `json:"queue_position,omitempty"`
	ETASeconds    float64         `json:"eta_seconds,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
	Error         string          `json:"error,omitempty"`
}

// DeepAuditResult is a completed Deep Site Audit. Scores holds lai_score (the
// headline score, 0-5) and lai_grade; Sections, Findings (failing checks by
// severity, with evidence) and Coverage are kept as JSON. Raw is the whole result.
type DeepAuditResult struct {
	raw
	Scores   map[string]any  `json:"scores"`
	Sections json.RawMessage `json:"sections,omitempty"`
	Findings json.RawMessage `json:"findings,omitempty"`
	Coverage json.RawMessage `json:"coverage,omitempty"`
}

// StartDeepAudit starts a Deep Site Audit and returns the job at once. A full
// queue answers 429 with Retry-After, which the client retries within its backoff
// cap; DeepAudit waits for queue capacity as long as ctx allows.
func (c *Client) StartDeepAudit(ctx context.Context, siteURL string, opts *DeepAuditOptions) (*DeepAuditJob, error) {
	var out DeepAuditJob
	if err := c.call(ctx, c.deepStart(siteURL, opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) deepStart(siteURL string, opts *DeepAuditOptions) request {
	body := map[string]any{"url": siteURL}
	if opts != nil {
		if opts.BusinessType != "" {
			body["business_type"] = opts.BusinessType
		}
		if opts.IsLocal != nil {
			body["is_local"] = *opts.IsLocal
		}
		if opts.WebhookURL != "" {
			body["webhook_url"] = opts.WebhookURL
		}
	}
	return request{method: http.MethodPost, base: c.deepBase(), path: "/site-audit", body: body, auth: true}
}

// GetDeepAudit reads a Deep Site Audit job's status. Only the key that started it
// can read it.
func (c *Client) GetDeepAudit(ctx context.Context, jobID string) (*DeepAuditStatus, error) {
	var out DeepAuditStatus
	r := request{method: http.MethodGet, base: c.deepBase(), path: "/site-audit/" + escape(jobID), auth: true}
	if err := c.call(ctx, r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WaitForDeepAudit polls a Deep Site Audit until it completes and returns its
// result. While queued it waits the server's ETA (at most 15s at a time). A failed
// audit returns a *JobError; a cancelled or expired ctx returns ctx.Err().
func (c *Client) WaitForDeepAudit(ctx context.Context, jobID string, wait *WaitOptions) (*DeepAuditResult, error) {
	for {
		status, err := c.GetDeepAudit(ctx, jobID)
		if err != nil {
			return nil, err
		}
		wait.progress(status)
		switch status.Status {
		case StatusCompleted:
			var out DeepAuditResult
			if err := decode(status.Result, &out); err != nil {
				return nil, err
			}
			return &out, nil
		case StatusFailed:
			return nil, &JobError{JobID: jobID, Message: orDefault(status.Error, "audit failed")}
		}
		pause := wait.interval()
		if status.Status == StatusQueued && status.ETASeconds > 0 {
			pause = time.Duration(status.ETASeconds * float64(time.Second))
			if pause > 15*time.Second {
				pause = 15 * time.Second
			}
		}
		if err := c.sleep(ctx, pause); err != nil {
			return nil, err
		}
	}
}

// DeepAudit starts a Deep Site Audit, waits for queue capacity if the queue is
// full, then polls until the audit completes and returns its result. Bound it with
// a context deadline: a deep audit usually takes a few minutes.
//
//	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
//	defer cancel()
//	result, err := client.DeepAudit(ctx, "https://example.com", nil, nil)
func (c *Client) DeepAudit(ctx context.Context, siteURL string, opts *DeepAuditOptions, wait *WaitOptions) (*DeepAuditResult, error) {
	var job DeepAuditJob
	for {
		err := c.call(ctx, c.deepStart(siteURL, opts), &job)
		var ae *APIError
		if errors.As(err, &ae) && errors.Is(err, ErrRateLimited) && ae.RetryAfter > 0 {
			// The queue is full and stayed full past the client's backoff: keep waiting.
			if err := c.sleep(ctx, ae.RetryAfter); err != nil {
				return nil, err
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		break
	}
	return c.WaitForDeepAudit(ctx, job.JobID, wait)
}

// DeepAuditUsage is Deep Site Audits used and remaining this month.
type DeepAuditUsage struct {
	raw
	Tier      string `json:"tier"`
	SiteAudit struct {
		Used      int `json:"used"`
		Remaining int `json:"remaining"`
	} `json:"site_audit"`
}

// DeepAuditUsage returns this month's Deep Site Audit usage (separate from Usage).
func (c *Client) DeepAuditUsage(ctx context.Context) (*DeepAuditUsage, error) {
	base := c.deepBase()
	path := "/deep-audit/usage"
	if u, err := url.Parse(base); err == nil && strings.HasPrefix(u.Hostname(), "engine.") {
		path = "/usage" // the legacy engine host serves it at /usage
	}
	var out DeepAuditUsage
	if err := c.call(ctx, request{method: http.MethodGet, base: base, path: path, auth: true}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
