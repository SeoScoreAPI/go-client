package seoscoreapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// Check is one check in an audit category.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pass, warning, fail or info
	// Value is what was measured: a number, string, bool, list or object depending
	// on the check.
	Value          json.RawMessage `json:"value,omitempty"`
	Score          float64         `json:"score"`
	Recommendation string          `json:"recommendation,omitempty"`
}

// Category is one scored category of an audit (meta, technical, social,
// performance, accessibility).
type Category struct {
	Score  float64 `json:"score"`
	Checks []Check `json:"checks"`
}

// Priority is one fix, ordered by importance.
type Priority struct {
	Severity     string `json:"severity"` // high, medium or low
	Category     string `json:"category"`
	Issue        string `json:"issue"`
	Fix          string `json:"fix"`
	PlainEnglish string `json:"plain_english,omitempty"`
	WhoFixesIt   string `json:"who_fixes_it,omitempty"`
}

// CoreWebVitals is the core_web_vitals block of an audit. Read Status first:
// "not_requested" (the default), "ok" (measured) or "unavailable" (see Reason;
// Pending means it is still measuring, so audit the URL again to pick it up).
type CoreWebVitals struct {
	Status   string   `json:"status"`
	LCPms    *float64 `json:"lcp_ms"`
	INPms    *float64 `json:"inp_ms"`
	CLS      *float64 `json:"cls"`
	FCPms    *float64 `json:"fcp_ms"`
	TTFBms   *float64 `json:"ttfb_ms"`
	Checks   []Check  `json:"checks,omitempty"`
	Source   string   `json:"source,omitempty"` // field or lab
	Strategy string   `json:"strategy,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Hint     string   `json:"hint,omitempty"`
	Pending  bool     `json:"pending,omitempty"`
}

// AuditResult is the answer of Audit and one entry of a batch. Free keys see two
// checks per category and two priorities. SXO, AEO and AIO scores are present on
// paid plans. Raw holds the whole response.
type AuditResult struct {
	raw
	URL          string              `json:"url"`
	Score        float64             `json:"score"`
	Grade        string              `json:"grade"`
	ResponseCode int                 `json:"response_code,omitempty"`
	ResponseTime string              `json:"response_time,omitempty"`
	Audit        map[string]Category `json:"audit,omitempty"`
	Priorities   []Priority          `json:"priorities,omitempty"`
	// AIReadability, SXO, AEO, AIO and History are kept as JSON; decode them into
	// your own types or an Object when you need them.
	AIReadability json.RawMessage `json:"ai_readability,omitempty"`
	CoreWebVitals *CoreWebVitals  `json:"core_web_vitals,omitempty"`
	SXO           json.RawMessage `json:"sxo,omitempty"`
	AEO           json.RawMessage `json:"aeo,omitempty"`
	AIO           json.RawMessage `json:"aio,omitempty"`
	History       json.RawMessage `json:"history,omitempty"`
	// PlaceholderNotice is true when the URL was a reserved example domain
	// (example.com and the like): the first one is not scored or counted.
	PlaceholderNotice bool   `json:"placeholder_notice,omitempty"`
	Message           string `json:"message,omitempty"`
	// Error is set on a batch entry that could not be audited.
	Error string `json:"error,omitempty"`
}

// AuditOptions are optional settings for Audit and BatchAudit.
type AuditOptions struct {
	// CoreWebVitals adds measured LCP, INP, CLS, FCP and TTFB from Google PageSpeed
	// Insights. The first measurement of a URL adds about 10 seconds; it is then
	// cached for 24 hours.
	CoreWebVitals bool
}

// Audit runs an SEO audit on one URL (every plan; counts as one audit).
//
// Do not add cache-busting query parameters: each distinct URL is a separate audit
// that counts against your quota, and a changed URL may score differently.
func (c *Client) Audit(ctx context.Context, pageURL string, opts *AuditOptions) (*AuditResult, error) {
	q := values("url", pageURL)
	if opts != nil && opts.CoreWebVitals {
		q.Set("cwv", "true")
	}
	var out AuditResult
	if err := c.getJSON(ctx, "/audit", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BatchResult is the answer of BatchAudit: one result per URL, in order. A URL
// that failed has Error set.
type BatchResult struct {
	raw
	Count   int           `json:"count"`
	Results []AuditResult `json:"results"`
}

// MaxBatchURLs is the most URLs one BatchAudit request takes.
const MaxBatchURLs = 10

// BatchAudit audits up to 10 URLs in one request (paid plans). Use AuditAll for
// longer lists.
func (c *Client) BatchAudit(ctx context.Context, urls []string, opts *AuditOptions) (*BatchResult, error) {
	body := map[string]any{"urls": urls}
	if opts != nil && opts.CoreWebVitals {
		body["cwv"] = true
	}
	var out BatchResult
	if err := c.sendJSON(ctx, http.MethodPost, "/audit/batch", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AuditAll audits any number of URLs in batches of 10 (paid plans), one batch at a
// time, and returns one result per URL in order. It stops at the first failed
// batch and returns the results so far with the error.
func (c *Client) AuditAll(ctx context.Context, urls []string, opts *AuditOptions) ([]AuditResult, error) {
	results := make([]AuditResult, 0, len(urls))
	for start := 0; start < len(urls); start += MaxBatchURLs {
		end := start + MaxBatchURLs
		if end > len(urls) {
			end = len(urls)
		}
		batch, err := c.BatchAudit(ctx, urls[start:end], opts)
		if err != nil {
			return results, err
		}
		results = append(results, batch.Results...)
	}
	return results, nil
}

// Compare compares 2 to 5 URLs side by side with a structured diff (Basic plan or higher).
func (c *Client) Compare(ctx context.Context, urls []string) (Object, error) {
	var out Object
	return out, c.sendJSON(ctx, http.MethodPost, "/compare", map[string]any{"urls": urls}, &out)
}

// CompetitiveAudit runs a head-to-head audit of a page against a competitor for a
// keyword: gap score, per-check diffs and action items (Pro plan or higher).
func (c *Client) CompetitiveAudit(ctx context.Context, pageURL, competitorURL, keyword string) (Object, error) {
	body := map[string]any{"url": pageURL, "competitor_url": competitorURL, "keyword": keyword}
	var out Object
	return out, c.sendJSON(ctx, http.MethodPost, "/audit/competitive", body, &out)
}

// HistoryOptions are optional settings for History.
type HistoryOptions struct {
	Limit int     // points to return, 1-1000; the API's default is 100
	Since float64 // Unix seconds, inclusive lower bound
}

// History returns the audit timeseries and trend summary for a URL (Starter plan or higher).
func (c *Client) History(ctx context.Context, pageURL string, opts *HistoryOptions) (Object, error) {
	q := values("url", pageURL)
	if opts != nil {
		if opts.Limit > 0 {
			q.Set("limit", itoa(opts.Limit))
		}
		if opts.Since > 0 {
			q.Set("since", strconv.FormatFloat(opts.Since, 'f', -1, 64))
		}
	}
	var out Object
	return out, c.getJSON(ctx, "/history", q, &out)
}

// DomainHistory is one domain this key has audited.
type DomainHistory struct {
	Domain      string  `json:"domain"`
	LatestScore float64 `json:"latest_score"`
	LatestGrade string  `json:"latest_grade"`
	Trend30d    float64 `json:"trend_30d"`
}

// HistoryDomains lists every domain audited with this key, with its latest score
// and 30-day trend (Starter plan or higher).
func (c *Client) HistoryDomains(ctx context.Context) ([]DomainHistory, error) {
	var out struct {
		Domains []DomainHistory `json:"domains"`
	}
	if err := c.getJSON(ctx, "/history/domains", nil, &out); err != nil {
		return nil, err
	}
	return out.Domains, nil
}

// AccessibilityAudit runs an ADA / WCAG 2.1 AA audit (paid plans; its own monthly
// allowance, see Usage). include "trackers" adds the tracker and pixel inventory;
// pass "" for none.
func (c *Client) AccessibilityAudit(ctx context.Context, pageURL, include string) (Object, error) {
	var out Object
	return out, c.getJSON(ctx, "/audit/accessibility", values("url", pageURL, "include", include), &out)
}

// ExportOptions are optional settings for AuditExport.
type ExportOptions struct {
	Format string // "pdf" (default), "md" or "csv"
	// White-label, Pro and Ultra only (other plans get 403):
	BrandName  string
	BrandColor string // #rrggbb
	LogoURL    string // https PNG, JPEG or WebP, at most 300 KB
}

// AuditExport runs an audit and returns it as a report file's bytes (counts as one audit).
func (c *Client) AuditExport(ctx context.Context, pageURL string, opts *ExportOptions) ([]byte, error) {
	if opts == nil {
		opts = &ExportOptions{}
	}
	format := opts.Format
	if format == "" {
		format = "pdf"
	}
	q := values("url", pageURL, "format", format, "brand_name", opts.BrandName,
		"brand_color", opts.BrandColor, "logo_url", opts.LogoURL)
	body, _, err := c.do(ctx, request{method: http.MethodGet, path: "/audit/export", query: q, auth: true})
	return body, err
}

// AIReadability scores how well AI and LLM systems can consume a page (counts as one audit).
func (c *Client) AIReadability(ctx context.Context, pageURL string) (Object, error) {
	var out Object
	return out, c.getJSON(ctx, "/ai-readability", values("url", pageURL), &out)
}

// Trackers lists the third-party trackers and pixels a page loads, in a real
// browser (counts as one audit; a page that can't be loaded is not counted).
func (c *Client) Trackers(ctx context.Context, pageURL string) (Object, error) {
	var out Object
	return out, c.getJSON(ctx, "/trackers", values("url", pageURL), &out)
}

// Conversion scores how well a page turns a visitor into a lead or a sale.
// pageType is local_service, ecommerce, saas or general; "" detects it.
func (c *Client) Conversion(ctx context.Context, pageURL, pageType string) (Object, error) {
	var out Object
	return out, c.getJSON(ctx, "/conversion", values("url", pageURL, "page_type", pageType), &out)
}

// QuickWinsRequest is your own Search Console data for QuickWins: Rows or CSV.
type QuickWinsRequest struct {
	// Rows are Search Console rows: {"query", "page", "clicks", "impressions",
	// "position"}, or the Search Analytics API's own {"keys": [...], ...} shape.
	Rows []map[string]any `json:"rows,omitempty"`
	// CSV is the Performance export as text, header row included.
	CSV            string             `json:"csv,omitempty"`
	MinImpressions int                `json:"min_impressions,omitempty"`
	TargetPosition int                `json:"target_position,omitempty"` // 1-10
	Limit          int                `json:"limit,omitempty"`           // at most 200
	ExcludeTerms   []string           `json:"exclude_terms,omitempty"`
	CTRCurve       map[string]float64 `json:"ctr_curve,omitempty"`
}

// QuickWins finds the page-2 quick wins in your Search Console data (every plan;
// counts as one audit). Nothing is fetched from Search Console and nothing is stored.
func (c *Client) QuickWins(ctx context.Context, data QuickWinsRequest) (Object, error) {
	var out Object
	return out, c.sendJSON(ctx, http.MethodPost, "/quick-wins", data, &out)
}

// Backlinks returns observed backlinks for a domain, an audit-fed sample rather
// than a full index (Basic plan or higher). limit is 1-500; 0 uses 50.
func (c *Client) Backlinks(ctx context.Context, domain string, limit int) (Object, error) {
	if limit <= 0 {
		limit = 50
	}
	var out Object
	return out, c.getJSON(ctx, "/backlinks", values("domain", domain, "limit", itoa(limit)), &out)
}

// GenerateLlmsTxt returns an llms.txt file for a domain. It works without a key
// (basic version); with a key on Basic or higher it is the curated version.
func (c *Client) GenerateLlmsTxt(ctx context.Context, domain string) (string, error) {
	body, _, err := c.do(ctx, request{method: http.MethodGet, path: "/llms-generator",
		query: values("domain", domain), optionalAuth: true})
	return string(body), err
}

// ReportURL is the shareable report page for a domain. It makes no request.
func (c *Client) ReportURL(domain string) string {
	return c.baseURL + "/report/" + url.PathEscape(domain)
}
