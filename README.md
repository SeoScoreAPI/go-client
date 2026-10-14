# seoscoreapi-go

[![Go Reference](https://pkg.go.dev/badge/github.com/SeoScoreAPI/go-client.svg)](https://pkg.go.dev/github.com/SeoScoreAPI/go-client)

Go client for [SEO Score API](https://seoscoreapi.com): audit any URL for SEO issues with one call.
It has no dependencies outside the standard library.

## Install

```bash
go get github.com/SeoScoreAPI/go-client
```

Requires Go 1.21 or later.

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"log"

	seoscoreapi "github.com/SeoScoreAPI/go-client"
)

func main() {
	// Get a free API key (2 audits/day, no credit card) at https://seoscoreapi.com/#signup
	client := seoscoreapi.New() // reads SEOSCORE_API_KEY; or seoscoreapi.WithAPIKey("ssa_...")

	result, err := client.Audit(context.Background(), "https://example.com", nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Score: %.1f/100 (%s)\n", result.Score, result.Grade)
	for _, p := range result.Priorities {
		fmt.Printf("[%s] %s: %s\n", p.Severity, p.Issue, p.Fix)
	}
}
```

## The client

```go
client := seoscoreapi.New(
	seoscoreapi.WithAPIKey(os.Getenv("SEOSCORE_API_KEY")),
	seoscoreapi.WithTimeout(90*time.Second),     // per request; default 120s
	seoscoreapi.WithRetries(3),                  // default 3; 0 turns retries off
	seoscoreapi.WithMaxBackoff(60*time.Second),  // longest wait between attempts
	seoscoreapi.WithUserAgent("myapp/2.1"),      // appended to seoscoreapi-go/<version>
	seoscoreapi.WithHTTPClient(&http.Client{}),  // your own transport or proxy
)
```

A `Client` is safe for concurrent use: create one and share it. Every method takes a
`context.Context`, so a deadline or cancellation stops it, including the waits between
retries and polls.

**Retries.** A rate-limited request (429 for a per-minute limit or a full queue) is
retried after the server's `Retry-After`, so you don't need your own sleep loop. A 502,
503 or 504 and a network error are retried with exponential backoff and jitter. POST
requests, which can start work, are retried only on a 429 rate limit or a 503. A used-up
quota is never retried.

**Typed results.** The main calls (`Audit`, `BatchAudit`, `Usage`, crawls, Deep Site
Audits) return structs. Each one also keeps the complete response in `Raw`, so a field
this client doesn't model yet is still there. The other endpoints return
`seoscoreapi.Object` (a `map[string]any`).

## Errors

Failed calls return an `*seoscoreapi.APIError` with `StatusCode`, `Message` (the API's
explanation) and `RetryAfter`. Use `errors.Is` to check its kind:

```go
result, err := client.Audit(ctx, url, nil)
switch {
case errors.Is(err, seoscoreapi.ErrQuotaExceeded):
	// daily or monthly allowance used up: upgrade or wait for the reset
case errors.Is(err, seoscoreapi.ErrRateLimited):
	var apiErr *seoscoreapi.APIError
	errors.As(err, &apiErr)
	log.Printf("slow down, retry in %s", apiErr.RetryAfter)
case errors.Is(err, seoscoreapi.ErrUnauthorized):
	// missing, invalid or revoked key
case errors.Is(err, seoscoreapi.ErrPlanRequired):
	// the endpoint needs a higher plan or credits
case errors.Is(err, seoscoreapi.ErrInvalidRequest):
	// the URL or request was rejected; see the message
case err != nil:
	log.Fatal(err)
}
```

The other kinds are `ErrNotFound`, `ErrServer`, `ErrNoAPIKey` (no key given) and
`ErrJobFailed` (a crawl or Deep Site Audit that ended in `failed`).

## Methods

| Method | Description |
|---|---|
| `Signup(ctx, email)` | Start signup: the API emails a 6-digit code. Returns `{Status: "verification_required"}`, not a key |
| `Verify(ctx, email, code)` | Finish signup with the emailed code. Returns the API key (shown only once) |
| `Audit(ctx, url, opts)` | SEO audit of one URL. `&AuditOptions{CoreWebVitals: true}` adds measured Core Web Vitals |
| `BatchAudit(ctx, urls, opts)` | Up to 10 URLs in one request (paid) |
| `AuditAll(ctx, urls, opts)` | Any number of URLs, 10 per request, results in order (paid) |
| `Compare(ctx, urls)` | Compare 2–5 URLs with a structured diff (Basic+) |
| `CompetitiveAudit(ctx, url, competitorURL, keyword)` | Head-to-head audit with gap score (Pro+) |
| `History(ctx, url, opts)` / `HistoryDomains(ctx)` | Audit timeseries for a URL / every domain audited with this key (Starter+) |
| `Usage(ctx)` | Plan, usage and limits |
| `AddMonitor(ctx, url, opts)` / `ListMonitors(ctx)` / `RemoveMonitor(ctx, url)` | Score monitoring with optional Slack/webhook alerts (paid) |
| `ScoreboardOptOut(ctx, optOut)` | Opt in or out of the public scoreboard |
| `ReportURL(domain)` | Shareable report URL (no request) |
| `AccessibilityAudit(ctx, url, include)` | ADA / WCAG 2.1 AA audit (paid; own monthly allowance). `include` `"trackers"` adds the tracker inventory |
| `AuditExport(ctx, url, opts)` | Audit as a PDF / Markdown / CSV file (`[]byte`). White-label on Pro and Ultra |
| `AIReadability(ctx, url)` | How well AI/LLM systems can consume the page |
| `Trackers(ctx, url)` | Third-party trackers and pixels the page loads |
| `Conversion(ctx, url, pageType)` | Conversion score: headline, CTA, trust, forms, objections |
| `QuickWins(ctx, data)` | Page-2 quick wins from your own Search Console export (every plan) |
| `Backlinks(ctx, domain, limit)` | Observed backlinks, an audit-fed sample (Basic+) |
| `GenerateLlmsTxt(ctx, domain)` | llms.txt for a domain. Works without a key; curated version on Basic+ |
| `GeoAudit(ctx, url)` | GEO audit: visibility to LLMs (Basic+) |
| `GeoBrandProbe(ctx, probe)` | How often LLMs mention your brand (Basic+) |
| `AddGeoMonitor` / `ListGeoMonitors` / `RemoveGeoMonitor` / `GeoMonitorHistory` | GEO monitors (Basic+) |
| `StartCrawl(ctx, url, maxPages)` / `GetCrawl(ctx, jobID)` | Start / poll a multi-page site crawl (Pro/Ultra, or a Deep Audit credit) |
| `WaitForCrawl(ctx, jobID, wait)` / `Crawl(ctx, url, maxPages, wait)` | Poll a crawl to completion / start one and wait |
| `StartDeepAudit(ctx, url, opts)` / `GetDeepAudit(ctx, jobID)` | Start / poll a Deep Site Audit (Pro/Ultra, or credits) |
| `WaitForDeepAudit(ctx, jobID, wait)` / `DeepAudit(ctx, url, opts, wait)` | Poll a Deep Site Audit to completion / start one and wait |
| `DeepAuditUsage(ctx)` | Deep Site Audits used and remaining this month |
| `CitationStarterPrompts(ctx, topic, opts)` / `CitationSuggestPrompts(ctx, domain)` | Prompts to track for AI citations (not metered) |
| `CreateCitationTracker` / `ListCitationTrackers` / `GetCitationTracker` / `UpdateCitationTracker` / `DeleteCitationTracker` | Track a brand in AI answers (Starter+) |
| `RunCitationTracker(ctx, id)` / `GetCitationRun(ctx, runID)` / `CitationTrackerHistory(ctx, id, days)` | Run a tracker now / read a run / trends over time |
| `CitationCheckOnce(ctx, check)` | One-off citation check, metered per check (paid) |
| `CitationTopups(ctx)` / `CitationTopupCheckout(ctx, pack)` | Top-up packs and balance / Stripe Checkout URL for a pack |
| `CitationAutoReload(ctx)` | Read the auto-reload setting (read-only; change it on the dashboard) |

## Get an API key from code

Signup takes two calls. `Signup` emails a 6-digit code (valid for 10 minutes) and
`Verify` trades that code for the key. You can also sign up at
[seoscoreapi.com](https://seoscoreapi.com/#signup).

```go
client := seoscoreapi.New()
if _, err := client.Signup(ctx, "you@example.com"); err != nil {
	log.Fatal(err) // 400: address refused; 409: it already has an active key
}
fmt.Print("Code from the email: ")
var code string
fmt.Scanln(&code)
key, err := client.Verify(ctx, "you@example.com", code)
if err != nil {
	log.Fatal(err) // wrong or expired code; five wrong codes void the signup
}
fmt.Println(key) // save it: the key is shown only once
```

## Batch audits

```go
results, err := client.AuditAll(ctx, urls, nil) // 10 URLs per request, in order
for _, r := range results {
	if r.Error != "" {
		fmt.Println(r.URL, "failed:", r.Error)
		continue
	}
	fmt.Printf("%s %.1f %s\n", r.URL, r.Score, r.Grade)
}
```

Audit each page once per change rather than on a timer, and don't add cache-busting
query parameters: every distinct URL is a separate audit against your quota. Use
`AddMonitor` for scheduled re-checks and `History` for trends.

## Deep Site Audit

A Deep Site Audit runs asynchronously and usually takes a few minutes. `DeepAudit`
starts one, waits for queue capacity if the queue is full, polls until it finishes
(honouring the server's ETA while queued) and returns the result. Bound it with a
context deadline:

```go
ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
defer cancel()

result, err := client.DeepAudit(ctx, "https://example.com",
	&seoscoreapi.DeepAuditOptions{BusinessType: "saas"},
	&seoscoreapi.WaitOptions{
		PollInterval: 10 * time.Second,
		OnProgress: func(s any) {
			st := s.(*seoscoreapi.DeepAuditStatus)
			fmt.Printf("%s %.0f%% %s\n", st.Status, st.Progress, st.Stage)
		},
	})
if errors.Is(err, seoscoreapi.ErrJobFailed) {
	log.Fatal("the audit failed: ", err)
}
if err != nil {
	log.Fatal(err)
}
fmt.Println(result.Scores["lai_score"], result.Scores["lai_grade"]) // headline score, 0-5
```

To start one now and collect it later, call `StartDeepAudit`, keep the `JobID`, and use
`GetDeepAudit` or `WaitForDeepAudit`. If the context ends while waiting, the audit keeps
running on the server.

**Webhook.** `DeepAuditOptions.WebhookURL` gets one POST of `{"job_id", "status",
"result"}` when the job completes. It is a single unsigned attempt with no retries, and
nothing is sent for a failed job, so keep polling as the source of truth.

## Core Web Vitals (opt-in)

Audits are fast because they don't wait on Google PageSpeed Insights. Ask for measured
Core Web Vitals when you need them:

```go
result, err := client.Audit(ctx, "https://example.com", &seoscoreapi.AuditOptions{CoreWebVitals: true})
if err != nil {
	log.Fatal(err)
}
switch v := result.CoreWebVitals; {
case v.Status == "ok":
	fmt.Println(*v.LCPms, *v.INPms, *v.CLS, v.Source) // source: "field" or "lab"
case v.Pending:
	fmt.Println("still measuring; audit this URL again in a minute")
default:
	fmt.Println("no Core Web Vitals:", v.Reason)
}
```

The first measurement of a URL adds about 10 seconds; it is then cached for 24 hours.

## Examples

Runnable programs are in [`examples/`](examples):

- [`audit`](examples/audit): audit one URL and print the score and fixes
- [`batch`](examples/batch): audit a list of URLs from stdin into CSV
- [`deepaudit`](examples/deepaudit): run a Deep Site Audit with progress output

```bash
export SEOSCORE_API_KEY=ssa_...
go run ./examples/audit https://example.com
```

## Links

- [API documentation](https://seoscoreapi.com/docs)
- [Pricing](https://seoscoreapi.com/pricing)
- [Python client](https://github.com/SeoScoreAPI/python-client) · [Node.js client](https://github.com/SeoScoreAPI/node-client)

## License

MIT
