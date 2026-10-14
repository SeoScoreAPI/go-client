package seoscoreapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// server is a fake API: each handler answers in turn for its path.
type server struct {
	t        *testing.T
	mu       sync.Mutex
	srv      *httptest.Server
	routes   map[string][]http.HandlerFunc
	requests []*http.Request
	bodies   []string
}

func newServer(t *testing.T) *server {
	s := &server{t: t, routes: map[string][]http.HandlerFunc{}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.requests = append(s.requests, r)
		s.bodies = append(s.bodies, string(body))
		key := r.Method + " " + r.URL.Path
		handlers := s.routes[key]
		var h http.HandlerFunc
		if len(handlers) > 0 {
			h = handlers[0]
			if len(handlers) > 1 {
				s.routes[key] = handlers[1:]
			}
		}
		s.mu.Unlock()
		if h == nil {
			t.Errorf("unexpected request %s", key)
			w.WriteHeader(599)
			return
		}
		h(w, r)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// on queues handlers for "METHOD /path"; the last one repeats.
func (s *server) on(route string, handlers ...http.HandlerFunc) { s.routes[route] = handlers }

func (s *server) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func reply(status int, body string, headers ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		for i := 0; i+1 < len(headers); i += 2 {
			w.Header().Set(headers[i], headers[i+1])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// client returns a Client against the fake, recording sleeps instead of sleeping.
func (s *server) client(opts ...Option) (*Client, *[]time.Duration) {
	c := New(append([]Option{WithAPIKey("ssa_test"), WithBaseURL(s.srv.URL)}, opts...)...)
	var slept []time.Duration
	var mu sync.Mutex
	c.sleep = func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		slept = append(slept, d)
		mu.Unlock()
		return ctx.Err()
	}
	return c, &slept
}

const auditJSON = `{"url":"https://example.com/","score":91.5,"grade":"A+","response_code":200,
"audit":{"meta":{"score":87.8,"checks":[{"name":"title","status":"warning","value":"Hi","score":60,
"recommendation":"Keep under 60."}]}},"priorities":[{"severity":"high","category":"technical",
"issue":"Schema missing required fields","fix":"Add them"}],"core_web_vitals":{"status":"not_requested",
"lcp_ms":null,"inp_ms":null,"cls":null,"fcp_ms":null,"ttfb_ms":null,"checks":[]},"sxo":{"sxo_score":78},
"something_new":{"x":1}}`

func TestAuditSendsKeyAndUserAgentAndDecodes(t *testing.T) {
	s := newServer(t)
	s.on("GET /audit", reply(200, auditJSON))
	c, _ := s.client(WithUserAgent("myapp/2.1"))

	res, err := c.Audit(context.Background(), "https://example.com/?a=1", &AuditOptions{CoreWebVitals: true})
	if err != nil {
		t.Fatal(err)
	}
	r := s.requests[0]
	if got := r.Header.Get("X-API-Key"); got != "ssa_test" {
		t.Errorf("X-API-Key = %q", got)
	}
	if got := r.Header.Get("User-Agent"); got != "seoscoreapi-go/"+Version+" myapp/2.1" {
		t.Errorf("User-Agent = %q", got)
	}
	if got := r.URL.Query().Get("url"); got != "https://example.com/?a=1" {
		t.Errorf("url param = %q", got)
	}
	if r.URL.Query().Get("cwv") != "true" {
		t.Error("cwv not sent")
	}
	if res.Score != 91.5 || res.Grade != "A+" || res.Audit["meta"].Checks[0].Recommendation != "Keep under 60." {
		t.Errorf("decoded %+v", res)
	}
	if len(res.Priorities) != 1 || res.CoreWebVitals.Status != "not_requested" || res.CoreWebVitals.LCPms != nil {
		t.Errorf("priorities/cwv %+v %+v", res.Priorities, res.CoreWebVitals)
	}
	var full map[string]any
	if err := json.Unmarshal(res.Raw, &full); err != nil || full["something_new"] == nil {
		t.Errorf("Raw does not keep unmodelled fields: %v %s", err, res.Raw)
	}
}

func TestAPIKeyFromEnvironmentAndMissingKey(t *testing.T) {
	t.Setenv(APIKeyEnv, " ssa_from_env ")
	s := newServer(t)
	s.on("GET /usage", reply(200, `{"tier":"ultra","usage_today":3,"limits":{"daily":null,"monthly":25000,"rpm":120}}`))
	c := New(WithBaseURL(s.srv.URL))
	u, err := c.Usage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.requests[0].Header.Get("X-API-Key") != "ssa_from_env" {
		t.Errorf("key from env not used: %q", s.requests[0].Header.Get("X-API-Key"))
	}
	if u.Tier != "ultra" || u.Limits.Daily != nil || *u.Limits.Monthly != 25000 {
		t.Errorf("usage %+v", u)
	}

	t.Setenv(APIKeyEnv, "")
	_, err = New(WithBaseURL(s.srv.URL)).Usage(context.Background())
	if !errors.Is(err, ErrNoAPIKey) || s.count() != 1 {
		t.Errorf("want ErrNoAPIKey and no request, got %v (%d requests)", err, s.count())
	}
}

func TestErrorKinds(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    error
		msg     string
	}{
		{"401", reply(401, `{"detail":"Invalid API key"}`), ErrUnauthorized, "Invalid API key"},
		{"402", reply(402, `{"detail":"No Deep Audit credits left"}`), ErrPlanRequired, "No Deep Audit credits left"},
		{"quota", reply(429, `{"detail":"You've hit your Free limit of 2 audits today."}`), ErrQuotaExceeded, "You've hit"},
		{"validation", reply(422, `{"detail":[{"loc":["query","url"],"msg":"field required"}]}`),
			ErrInvalidRequest, "query.url: field required"},
		{"404", reply(404, `{"detail":"Job not found"}`), ErrNotFound, "Job not found"},
		{"500", reply(500, `Internal Server Error`), ErrServer, "Internal Server Error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(t)
			s.on("GET /audit", tc.handler)
			c, _ := s.client(WithRetries(0))
			_, err := c.Audit(context.Background(), "https://example.com", nil)
			var ae *APIError
			if !errors.As(err, &ae) || !errors.Is(err, tc.want) || !strings.Contains(ae.Message, tc.msg) {
				t.Fatalf("got %v, want %v containing %q", err, tc.want, tc.msg)
			}
		})
	}
}

func TestQuotaIsNotRetried(t *testing.T) {
	s := newServer(t)
	s.on("GET /audit", reply(429, `{"detail":"You've hit your Starter limit of 200 audits this month."}`))
	c, slept := s.client()
	_, err := c.Audit(context.Background(), "https://example.com", nil)
	if !errors.Is(err, ErrQuotaExceeded) || s.count() != 1 || len(*slept) != 0 {
		t.Fatalf("err=%v requests=%d slept=%v", err, s.count(), *slept)
	}
}

func TestRateLimitWithRetryAfterIsRetried(t *testing.T) {
	s := newServer(t)
	s.on("GET /audit",
		reply(429, `{"error":"Rate limit exceeded: 60 requests per 60 seconds"}`, "Retry-After", "7"),
		reply(200, auditJSON))
	c, slept := s.client()
	res, err := c.Audit(context.Background(), "https://example.com", nil)
	if err != nil || res.Score != 91.5 {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if len(*slept) != 1 || (*slept)[0] != 7*time.Second {
		t.Errorf("slept %v, want [7s]", *slept)
	}
}

func TestRateLimitWithoutRetryAfterWaitsAWhile(t *testing.T) {
	s := newServer(t)
	s.on("GET /audit",
		reply(429, `{"detail":"Rate limit exceeded (10 requests/minute on Starter)."}`),
		reply(200, auditJSON))
	c, slept := s.client()
	if _, err := c.Audit(context.Background(), "https://example.com", nil); err != nil {
		t.Fatal(err)
	}
	if len(*slept) != 1 || (*slept)[0] != 15*time.Second {
		t.Errorf("slept %v, want [15s]", *slept)
	}
}

func TestRetryAfterBeyondCapIsReturned(t *testing.T) {
	s := newServer(t)
	s.on("GET /audit", reply(429, `{"error":"Rate limit exceeded"}`, "Retry-After", "600"))
	c, _ := s.client(WithMaxBackoff(30 * time.Second))
	_, err := c.Audit(context.Background(), "https://example.com", nil)
	var ae *APIError
	if !errors.As(err, &ae) || !errors.Is(err, ErrRateLimited) || ae.RetryAfter != 600*time.Second || s.count() != 1 {
		t.Fatalf("err=%v requests=%d", err, s.count())
	}
}

func TestServerErrorsRetriedWithCap(t *testing.T) {
	s := newServer(t)
	s.on("GET /usage", reply(502, `bad gateway`), reply(504, `timeout`), reply(200, `{"tier":"pro"}`))
	c, slept := s.client()
	u, err := c.Usage(context.Background())
	if err != nil || u.Tier != "pro" || s.count() != 3 || len(*slept) != 2 {
		t.Fatalf("err=%v u=%v requests=%d slept=%v", err, u, s.count(), *slept)
	}

	s2 := newServer(t)
	s2.on("GET /usage", reply(503, `{"detail":"unavailable"}`))
	c2, _ := s2.client(WithRetries(2))
	if _, err := c2.Usage(context.Background()); !errors.Is(err, ErrServer) || s2.count() != 3 {
		t.Fatalf("want 3 attempts and ErrServer, got %v after %d", err, s2.count())
	}
}

func TestPostIsRetriedOnlyWhenSafe(t *testing.T) {
	s := newServer(t)
	s.on("POST /audit/batch", reply(500, `{"detail":"boom"}`))
	c, _ := s.client()
	if _, err := c.BatchAudit(context.Background(), []string{"https://a.example"}, nil); !errors.Is(err, ErrServer) || s.count() != 1 {
		t.Fatalf("a POST 500 must not be retried: %v after %d", err, s.count())
	}

	s2 := newServer(t)
	s2.on("POST /audit/batch", reply(503, `{"detail":"busy"}`), reply(200, `{"count":1,"results":[`+auditJSON+`]}`))
	c2, _ := s2.client()
	res, err := c2.BatchAudit(context.Background(), []string{"https://a.example"}, &AuditOptions{CoreWebVitals: true})
	if err != nil || res.Count != 1 || res.Results[0].Grade != "A+" || s2.count() != 2 {
		t.Fatalf("a POST 503 is retried: %v %+v after %d", err, res, s2.count())
	}
	if !strings.Contains(s2.bodies[1], `"cwv":true`) || !strings.Contains(s2.bodies[1], `"urls":["https://a.example"]`) {
		t.Errorf("body %s", s2.bodies[1])
	}
}

func TestAuditAllBatchesByTen(t *testing.T) {
	s := newServer(t)
	s.on("POST /audit/batch", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URLs []string `json:"urls"`
		}
		_ = json.NewDecoder(strings.NewReader(s.bodies[len(s.bodies)-1])).Decode(&body)
		results := make([]string, len(body.URLs))
		for i, u := range body.URLs {
			results[i] = `{"url":"` + u + `","score":50,"grade":"C"}`
		}
		reply(200, `{"count":`+itoa(len(results))+`,"results":[`+strings.Join(results, ",")+`]}`)(w, r)
	})
	c, _ := s.client()
	urls := make([]string, 23)
	for i := range urls {
		urls[i] = "https://example.com/p" + itoa(i)
	}
	results, err := c.AuditAll(context.Background(), urls, nil)
	if err != nil || len(results) != 23 || s.count() != 3 {
		t.Fatalf("err=%v results=%d requests=%d", err, len(results), s.count())
	}
	for i, r := range results {
		if r.URL != urls[i] {
			t.Fatalf("result %d is %s, want %s", i, r.URL, urls[i])
		}
	}
}

func TestDeepAuditWaitsForQueueThenPolls(t *testing.T) {
	s := newServer(t)
	s.on("POST /site-audit",
		reply(429, `{"detail":"Audit queue is full"}`, "Retry-After", "120"),
		reply(200, `{"job_id":"j1","status":"queued","poll":"/site-audit/j1"}`))
	s.on("GET /site-audit/j1",
		reply(200, `{"job_id":"j1","status":"queued","queue_position":2,"eta_seconds":40}`),
		reply(200, `{"job_id":"j1","status":"running","progress":60,"stage":"Section 6"}`),
		reply(200, `{"job_id":"j1","status":"completed","progress":100,
			"result":{"scores":{"lai_score":3.9,"lai_grade":"B"},"findings":[{"id":"x"}]}}`))
	c, slept := s.client(WithMaxBackoff(60 * time.Second))
	local := true
	var seen []string
	res, err := c.DeepAudit(context.Background(), "https://example.com",
		&DeepAuditOptions{BusinessType: "saas", IsLocal: &local},
		&WaitOptions{PollInterval: 3 * time.Second, OnProgress: func(st any) {
			seen = append(seen, st.(*DeepAuditStatus).Status)
		}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Scores["lai_grade"] != "B" || !strings.Contains(string(res.Findings), `"x"`) || len(res.Raw) == 0 {
		t.Errorf("result %+v", res)
	}
	want := []time.Duration{120 * time.Second, 15 * time.Second, 3 * time.Second}
	if len(*slept) != 3 || (*slept)[0] != want[0] || (*slept)[1] != want[1] || (*slept)[2] != want[2] {
		t.Errorf("slept %v, want %v (queue Retry-After, ETA capped at 15s, poll interval)", *slept, want)
	}
	if strings.Join(seen, ",") != "queued,running,completed" {
		t.Errorf("progress %v", seen)
	}
	if !strings.Contains(s.bodies[1], `"business_type":"saas"`) || !strings.Contains(s.bodies[1], `"is_local":true`) {
		t.Errorf("start body %s", s.bodies[1])
	}
}

func TestDeepAuditFailedAndCancelled(t *testing.T) {
	s := newServer(t)
	s.on("GET /site-audit/j2", reply(200, `{"job_id":"j2","status":"failed","error":"site unreachable"}`))
	c, _ := s.client()
	_, err := c.WaitForDeepAudit(context.Background(), "j2", nil)
	var je *JobError
	if !errors.Is(err, ErrJobFailed) || !errors.As(err, &je) || je.Message != "site unreachable" {
		t.Fatalf("got %v", err)
	}

	s2 := newServer(t)
	s2.on("GET /site-audit/j3", reply(200, `{"job_id":"j3","status":"running","progress":10}`))
	c2 := New(WithAPIKey("k"), WithBaseURL(s2.srv.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = c2.WaitForDeepAudit(ctx, "j3", &WaitOptions{PollInterval: time.Hour})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
}

func TestDeepAuditHostAndUsagePath(t *testing.T) {
	s := newServer(t)
	s.on("GET /deep-audit/usage", reply(200, `{"tier":"pro","site_audit":{"used":2,"remaining":8}}`))
	c, _ := s.client()
	u, err := c.DeepAuditUsage(context.Background())
	if err != nil || u.SiteAudit.Remaining != 8 {
		t.Fatalf("err=%v u=%+v", err, u)
	}
	engine := New(WithDeepAuditBaseURL(EngineURL))
	if engine.deepBase() != EngineURL || engine.baseURL != DefaultBaseURL {
		t.Errorf("deep base %s base %s", engine.deepBase(), engine.baseURL)
	}
}

func TestCrawlFlow(t *testing.T) {
	s := newServer(t)
	s.on("POST /crawl", reply(200, `{"job_id":"c1","status":"queued","max_pages":20}`))
	s.on("GET /crawl/c1",
		reply(200, `{"job_id":"c1","status":"running","pages_done":5}`),
		reply(200, `{"job_id":"c1","status":"completed","pages_done":20,"site_map":"https://x/s","result":{"summary":{}}}`))
	c, _ := s.client()
	job, err := c.Crawl(context.Background(), "https://example.com", 20, nil)
	if err != nil || job.PagesDone != 20 || job.SiteMap == "" || len(job.Result) == 0 {
		t.Fatalf("err=%v job=%+v", err, job)
	}
	if !strings.Contains(s.bodies[0], `"max_pages":20`) {
		t.Errorf("body %s", s.bodies[0])
	}
}

func TestSignupVerifyWithoutKey(t *testing.T) {
	s := newServer(t)
	s.on("POST /signup", reply(200, `{"status":"verification_required","message":"Check your email"}`))
	s.on("POST /verify", reply(200, `{"api_key":"ssa_new"}`))
	c := New(WithBaseURL(s.srv.URL))
	t.Setenv(APIKeyEnv, "")
	c.apiKey = ""
	res, err := c.Signup(context.Background(), "you@example.com")
	if err != nil || res.Status != "verification_required" {
		t.Fatalf("signup %v %+v", err, res)
	}
	key, err := c.Verify(context.Background(), "you@example.com", " 123456 ")
	if err != nil || key != "ssa_new" {
		t.Fatalf("verify %v %q", err, key)
	}
	if !strings.Contains(s.bodies[1], `"code":"123456"`) || s.requests[0].Header.Get("X-API-Key") != "" {
		t.Errorf("verify body %s / key header %q", s.bodies[1], s.requests[0].Header.Get("X-API-Key"))
	}
}

func TestSmallerEndpoints(t *testing.T) {
	s := newServer(t)
	s.on("GET /llms-generator", reply(200, "# example.com\n"))
	s.on("GET /audit/export", reply(200, "%PDF-1.7"))
	s.on("PUT /scoreboard/opt-out", reply(200, `{"opt_out":true}`))
	s.on("DELETE /monitors", reply(200, `{"removed":true}`))
	s.on("GET /history/domains", reply(200, `{"domains":[{"domain":"example.com","latest_score":80,"latest_grade":"B","trend_30d":2}]}`))
	s.on("GET /citations/prompts/starter", reply(200, `{"prompts":["a"]}`))
	s.on("POST /citations/topups/checkout", reply(200, `{"url":"https://checkout.stripe.com/x"}`))
	c, _ := s.client()
	ctx := context.Background()

	txt, err := New(WithBaseURL(s.srv.URL), WithAPIKey("")).GenerateLlmsTxt(ctx, "example.com")
	if err != nil || txt != "# example.com\n" {
		t.Errorf("llms.txt %v %q", err, txt)
	}
	pdf, err := c.AuditExport(ctx, "https://example.com", &ExportOptions{BrandName: "Agency"})
	if err != nil || string(pdf) != "%PDF-1.7" {
		t.Errorf("export %v %q", err, pdf)
	}
	if q := s.requests[1].URL.Query(); q.Get("format") != "pdf" || q.Get("brand_name") != "Agency" || q.Has("logo_url") {
		t.Errorf("export query %v", q)
	}
	if _, err := c.ScoreboardOptOut(ctx, true); err != nil || s.requests[2].URL.Query().Get("opt_out") != "true" {
		t.Errorf("opt-out %v", err)
	}
	if _, err := c.RemoveMonitor(ctx, "https://example.com"); err != nil || s.bodies[3] != `{"url":"https://example.com"}` {
		t.Errorf("remove monitor %v %s", err, s.bodies[3])
	}
	domains, err := c.HistoryDomains(ctx)
	if err != nil || len(domains) != 1 || domains[0].LatestGrade != "B" {
		t.Errorf("history domains %v %+v", err, domains)
	}
	if _, err := c.CitationStarterPrompts(ctx, "seo tools", &StarterPromptOptions{Competitors: []string{"a", "b"}}); err != nil ||
		s.requests[5].URL.Query().Get("competitors") != "a,b" {
		t.Errorf("starter prompts %v %v", err, s.requests[5].URL.Query())
	}
	u, err := c.CitationTopupCheckout(ctx, "500")
	if err != nil || u != "https://checkout.stripe.com/x" {
		t.Errorf("checkout %v %q", err, u)
	}
	if got := c.ReportURL("example.com"); got != s.srv.URL+"/report/example.com" {
		t.Errorf("report url %s", got)
	}
}
