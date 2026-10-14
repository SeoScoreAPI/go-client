# Changelog

## 1.0.0 (2026-10-14)

First release.

- Every endpoint of the Python and Node.js clients (1.7.1): audits, batch, compare,
  competitive audit, history, monitors, accessibility, exports, AI readability,
  trackers, conversion, quick wins, backlinks, llms.txt, GEO, site crawls, AI
  citations and Deep Site Audits.
- `context.Context` on every call; functional options (`WithAPIKey`, `WithBaseURL`,
  `WithHTTPClient`, `WithTimeout`, `WithRetries`, `WithMaxBackoff`, `WithUserAgent`);
  the key from `SEOSCORE_API_KEY` when none is given.
- Typed results for the main calls, each keeping the full response in `Raw`.
- `*APIError` with `errors.Is` kinds: `ErrRateLimited`, `ErrQuotaExceeded`,
  `ErrUnauthorized`, `ErrPlanRequired`, `ErrInvalidRequest`, `ErrNotFound`, `ErrServer`.
- Automatic retries of rate limits (honouring `Retry-After`) and temporary server
  errors, with capped backoff.
- `AuditAll` (batches of 10), `Crawl` and `DeepAudit` start-and-wait helpers.
- No dependencies outside the standard library.
