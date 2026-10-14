package seoscoreapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors. An *APIError matches the one for its kind with errors.Is:
//
//	if errors.Is(err, seoscoreapi.ErrQuotaExceeded) { ... }
var (
	// ErrNoAPIKey: the call needs an API key and the client has none.
	ErrNoAPIKey = errors.New("seoscoreapi: no API key (pass WithAPIKey or set SEOSCORE_API_KEY)")
	// ErrUnauthorized: 401 or 403, a missing, invalid or revoked key, or a feature
	// the key's plan does not include.
	ErrUnauthorized = errors.New("seoscoreapi: unauthorized")
	// ErrPlanRequired: 402, the endpoint needs a higher plan or credits.
	ErrPlanRequired = errors.New("seoscoreapi: plan upgrade or credits required")
	// ErrRateLimited: 429 with Retry-After, a per-minute limit or a full queue.
	// APIError.RetryAfter says when to try again.
	ErrRateLimited = errors.New("seoscoreapi: rate limited")
	// ErrQuotaExceeded: 429 without Retry-After, the daily or monthly allowance is used up.
	ErrQuotaExceeded = errors.New("seoscoreapi: quota exceeded")
	// ErrInvalidRequest: 400, 413 or 422, the request or the URL was rejected.
	ErrInvalidRequest = errors.New("seoscoreapi: invalid request")
	// ErrNotFound: 404, an unknown job or resource, or one that belongs to another key.
	ErrNotFound = errors.New("seoscoreapi: not found")
	// ErrServer: 5xx, the API failed or is temporarily unavailable.
	ErrServer = errors.New("seoscoreapi: server error")
)

// APIError is a non-2xx answer from the API.
type APIError struct {
	// StatusCode is the HTTP status.
	StatusCode int
	// Message is the API's explanation (its "detail" or "error" field).
	Message string
	// RetryAfter is the server's Retry-After, zero when it sent none.
	RetryAfter time.Duration
	// Body is the response body as sent.
	Body []byte
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	if e.RetryAfter > 0 {
		return fmt.Sprintf("seoscoreapi: %d: %s (retry after %s)", e.StatusCode, msg, e.RetryAfter)
	}
	return fmt.Sprintf("seoscoreapi: %d: %s", e.StatusCode, msg)
}

// Is matches the sentinel error for the error's kind.
func (e *APIError) Is(target error) bool { return e.kind() == target }

func (e *APIError) kind() error {
	switch s := e.StatusCode; {
	case s == http.StatusUnauthorized || s == http.StatusForbidden:
		return ErrUnauthorized
	case s == http.StatusPaymentRequired:
		return ErrPlanRequired
	case s == http.StatusTooManyRequests && e.RetryAfter > 0:
		return ErrRateLimited
	case s == http.StatusTooManyRequests:
		msg := strings.ToLower(e.Message)
		if strings.HasPrefix(msg, "rate limit") {
			return ErrRateLimited
		}
		if strings.Contains(msg, "failed authentication") {
			return ErrUnauthorized // too many bad keys from this address; wait it out
		}
		return ErrQuotaExceeded
	case s == http.StatusBadRequest || s == http.StatusRequestEntityTooLarge || s == http.StatusUnprocessableEntity:
		return ErrInvalidRequest
	case s == http.StatusNotFound:
		return ErrNotFound
	case s >= 500:
		return ErrServer
	}
	return nil
}

func newAPIError(resp *http.Response, body []byte) *APIError {
	e := &APIError{StatusCode: resp.StatusCode, Body: body}
	if v := strings.TrimSpace(resp.Header.Get("Retry-After")); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			e.RetryAfter = time.Duration(secs) * time.Second
		} else if when, err := http.ParseTime(v); err == nil {
			if d := time.Until(when); d > 0 {
				e.RetryAfter = d
			}
		}
	}
	e.Message = errorMessage(body)
	return e
}

// errorMessage reads {"detail": "..."} (FastAPI) or {"error": "..."}; a validation
// error's detail is a list, joined into one line.
func errorMessage(body []byte) string {
	var doc struct {
		Detail json.RawMessage `json:"detail"`
		Error  json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &doc) != nil {
		s := strings.TrimSpace(string(body))
		if len(s) > 200 {
			s = s[:200]
		}
		return s
	}
	for _, field := range []json.RawMessage{doc.Detail, doc.Error} {
		if len(field) == 0 {
			continue
		}
		var s string
		if json.Unmarshal(field, &s) == nil {
			return s
		}
		var items []struct {
			Loc []any  `json:"loc"`
			Msg string `json:"msg"`
		}
		if json.Unmarshal(field, &items) == nil && len(items) > 0 {
			parts := make([]string, 0, len(items))
			for _, it := range items {
				loc := make([]string, 0, len(it.Loc))
				for _, l := range it.Loc {
					loc = append(loc, fmt.Sprint(l))
				}
				parts = append(parts, strings.Join(loc, ".")+": "+it.Msg)
			}
			return strings.Join(parts, "; ")
		}
		return string(field)
	}
	return ""
}
