package seoscoreapi

import (
	"context"
	"net/http"
)

// GeoAudit runs a GEO (Generative Engine Optimization) audit: how visible a page is
// to LLMs (Basic plan or higher; uses the plan's monthly GEO allowance).
func (c *Client) GeoAudit(ctx context.Context, pageURL string) (Object, error) {
	var out Object
	return out, c.getJSON(ctx, "/geo/audit", values("url", pageURL), &out)
}

// BrandProbe is the request of GeoBrandProbe.
type BrandProbe struct {
	Brand         string   `json:"brand"`
	Domain        string   `json:"domain"`
	Prompts       []string `json:"prompts"`
	Models        []string `json:"models,omitempty"`
	RunsPerPrompt int      `json:"runs_per_prompt,omitempty"` // the API's default is 3
}

// GeoBrandProbe asks LLMs your prompts and measures how often they mention your
// brand (Basic plan or higher).
func (c *Client) GeoBrandProbe(ctx context.Context, probe BrandProbe) (Object, error) {
	var out Object
	return out, c.sendJSON(ctx, http.MethodPost, "/geo/brand-probe", probe, &out)
}

// GeoMonitorOptions are optional settings for AddGeoMonitor.
type GeoMonitorOptions struct {
	Frequency      string // "weekly" (default) or "daily"; the Basic plan runs weekly
	AlertThreshold int    // 0 uses -5
	WebhookURL     string
}

// AddGeoMonitor creates a GEO monitor for a URL (Basic plan or higher).
func (c *Client) AddGeoMonitor(ctx context.Context, pageURL string, opts *GeoMonitorOptions) (Object, error) {
	if opts == nil {
		opts = &GeoMonitorOptions{}
	}
	body := map[string]any{"url": pageURL, "frequency": orDefault(opts.Frequency, "weekly"),
		"alert_threshold": intOr(opts.AlertThreshold, -5)}
	if opts.WebhookURL != "" {
		body["webhook_url"] = opts.WebhookURL
	}
	var out Object
	return out, c.sendJSON(ctx, http.MethodPost, "/geo/monitor", body, &out)
}

// RemoveGeoMonitor removes the GEO monitor for a URL.
func (c *Client) RemoveGeoMonitor(ctx context.Context, pageURL string) (Object, error) {
	var out Object
	return out, c.sendJSON(ctx, http.MethodDelete, "/geo/monitor", map[string]string{"url": pageURL}, &out)
}

// ListGeoMonitors lists the key's active GEO monitors.
func (c *Client) ListGeoMonitors(ctx context.Context) ([]Object, error) {
	var out struct {
		Monitors []Object `json:"monitors"`
	}
	if err := c.getJSON(ctx, "/geo/monitors", nil, &out); err != nil {
		return nil, err
	}
	return out.Monitors, nil
}

// GeoMonitorHistory returns a GEO monitor's score history. page starts at 1;
// perPage is 1-100 (0 uses 20).
func (c *Client) GeoMonitorHistory(ctx context.Context, monitorID int64, page, perPage int) (Object, error) {
	q := values("page", itoa(intOr(page, 1)), "per_page", itoa(intOr(perPage, 20)))
	var out Object
	return out, c.getJSON(ctx, "/geo/monitor/"+pathID(monitorID)+"/history", q, &out)
}
