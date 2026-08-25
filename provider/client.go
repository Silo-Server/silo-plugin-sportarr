package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultBaseURL  = "https://sportarr.net"
	apiKeyHeader    = "X-Api-Key"
	maxRetries      = 3
	maxResponseBody = 2 << 20 // 2 MB
)

type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	limiter    *rate.Limiter
	lookupIP   func(context.Context, string) ([]net.IP, error)
}

func NewClient(rateLimit int) *Client {
	if rateLimit <= 0 {
		rateLimit = 10
	}
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    defaultBaseURL,
		limiter:    rate.NewLimiter(rate.Limit(rateLimit), rateLimit),
		lookupIP: func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		},
	}
}

func (c *Client) SetBaseURL(baseURL string) {
	c.baseURL = strings.TrimRight(baseURL, "/")
}

func (c *Client) SetAPIKey(apiKey string) {
	c.apiKey = strings.TrimSpace(apiKey)
}

func (c *Client) hasAPIKey() bool {
	return c.apiKey != ""
}

// SetHTTPClient replaces the request client. It is primarily useful for
// callers that need to control transports in tests.
func (c *Client) SetHTTPClient(httpClient *http.Client) {
	c.httpClient = httpClient
}

func (c *Client) requestURL(path string) (string, error) {
	if !strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "//") {
		return "", fmt.Errorf("sportarr: request path must start with /api/")
	}
	requestPath, err := url.Parse(path)
	if err != nil || requestPath.IsAbs() || requestPath.Host != "" || requestPath.User != nil || requestPath.Fragment != "" {
		return "", fmt.Errorf("sportarr: invalid request path")
	}
	if !strings.HasPrefix(requestPath.Path, "/api/") || strings.Contains(requestPath.Path, `\`) {
		return "", fmt.Errorf("sportarr: invalid request path")
	}
	for _, segment := range strings.Split(requestPath.Path, "/") {
		if segment == "." || segment == ".." {
			return "", fmt.Errorf("sportarr: invalid request path")
		}
	}

	base, err := url.Parse(c.baseURL)
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || base.Opaque != "" {
		return "", fmt.Errorf("sportarr: invalid base URL")
	}
	switch strings.ToLower(base.Scheme) {
	case "http", "https":
	default:
		return "", fmt.Errorf("sportarr: base URL must use HTTP or HTTPS")
	}

	// Concatenating the validated absolute API path keeps a configured reverse-
	// proxy prefix (for example /sportarr) instead of replacing it.
	requestURL := strings.TrimRight(c.baseURL, "/") + path
	if _, err := url.ParseRequestURI(requestURL); err != nil {
		return "", fmt.Errorf("sportarr: invalid request URL: %w", err)
	}
	return requestURL, nil
}

func (c *Client) requestClient() http.Client {
	client := *c.httpClient
	// JSON endpoints must not redirect: following an unexpected cross-origin
	// redirect could disclose the instance API key. Image redirects are handled
	// separately and are deliberately inspected without being followed.
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client
}

func (c *Client) doGet(ctx context.Context, path string, dest any) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}

	reqURL, err := c.requestURL(path)
	if err != nil {
		return err
	}
	client := c.requestClient()

	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return fmt.Errorf("sportarr: create request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "silo-plugin-sportarr/1.0")
		req.Header.Set("Cache-Control", "no-cache, no-store")
		req.Header.Set("Pragma", "no-cache")
		if c.apiKey != "" {
			req.Header.Set(apiKeyHeader, c.apiKey)
		}

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("sportarr: request failed: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			_ = resp.Body.Close()
			if attempt < maxRetries {
				backoff := retryAfterOrDefault(resp, attempt)
				slog.Warn("sportarr: rate limited, backing off",
					"path", path, "attempt", attempt+1, "backoff", backoff.String())
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return ctx.Err()
				}
				continue
			}
			return fmt.Errorf("sportarr: rate limited after %d retries", maxRetries)
		}

		if resp.StatusCode >= 500 {
			_ = resp.Body.Close()
			if attempt < maxRetries {
				backoff := time.Duration(1<<attempt) * time.Second
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return ctx.Err()
				}
				continue
			}
			return fmt.Errorf("sportarr: server error %d after %d retries", resp.StatusCode, maxRetries)
		}

		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
			_ = resp.Body.Close()
			return fmt.Errorf("sportarr: HTTP %d: %s", resp.StatusCode, string(body))
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			return fmt.Errorf("sportarr: unexpected HTTP %d", resp.StatusCode)
		}

		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody)).Decode(dest)
		_ = resp.Body.Close()
		if decodeErr != nil {
			return fmt.Errorf("sportarr: decode response: %w", decodeErr)
		}
		return nil
	}
	return fmt.Errorf("sportarr: max retries exceeded")
}

func retryAfterOrDefault(resp *http.Response, attempt int) time.Duration {
	if val := resp.Header.Get("Retry-After"); val != "" {
		if secs, err := strconv.Atoi(val); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return time.Duration(1<<attempt) * time.Second
}

func (c *Client) Search(ctx context.Context, title string) (*AgentSearchResponse, error) {
	path := "/api/metadata/agents/search?title=" + url.QueryEscape(title)
	var resp AgentSearchResponse
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetSeries(ctx context.Context, leagueID string) (*AgentSeriesResponse, error) {
	path := "/api/metadata/agents/series/" + url.PathEscape(leagueID)
	var resp AgentSeriesResponse
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetSeasons(ctx context.Context, leagueID string) (*AgentSeasonsResponse, error) {
	path := "/api/metadata/agents/series/" + url.PathEscape(leagueID) + "/seasons"
	var resp AgentSeasonsResponse
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetSeasonEpisodes(ctx context.Context, leagueID string, seasonNumber int) (*AgentEpisodesResponse, error) {
	path := fmt.Sprintf("/api/metadata/agents/series/%s/season/%d/episodes",
		url.PathEscape(leagueID), seasonNumber)
	var resp AgentEpisodesResponse
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetEntityImages(ctx context.Context, entityType, entityID string) (*EntityImageResponse, error) {
	path := "/api/v1/images/entity/" + entityType + "/" + url.PathEscape(entityID) + "?completed_only=true"
	var resp EntityImageResponse
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetEntityImagesBatch(ctx context.Context, entityType string, entityIDs []string) map[string]*EntityImageResponse {
	type result struct {
		id   string
		resp *EntityImageResponse
	}

	seen := make(map[string]struct{}, len(entityIDs))
	var unique []string
	for _, id := range entityIDs {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			unique = append(unique, id)
		}
	}

	ch := make(chan result, len(unique))
	for _, id := range unique {
		go func(eid string) {
			resp, err := c.GetEntityImages(ctx, entityType, eid)
			if err != nil {
				ch <- result{id: eid}
				return
			}
			ch <- result{id: eid, resp: resp}
		}(id)
	}

	out := make(map[string]*EntityImageResponse, len(unique))
	for range unique {
		r := <-ch
		if r.resp != nil {
			out[r.id] = r.resp
		}
	}
	return out
}

// ResolveImageRedirect returns the public image target supplied by Sportarr's
// configured local API. It never follows the redirect itself.
func (c *Client) ResolveImageRedirect(ctx context.Context, path string) (string, error) {
	if !strings.HasPrefix(path, "/api/") {
		return "", fmt.Errorf("sportarr: image redirect path must start with /api/")
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return "", err
	}

	reqURL, err := c.requestURL(path)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("sportarr: create image redirect request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set(apiKeyHeader, c.apiKey)
	}

	client := c.requestClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sportarr: image redirect request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusMultipleChoices || resp.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("sportarr: image redirect returned HTTP %d", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	target, err := url.Parse(location)
	if err != nil || !target.IsAbs() || target.Scheme != "https" || target.Host == "" || target.User != nil {
		return "", fmt.Errorf("sportarr: image redirect location is not a public HTTPS URL")
	}
	if port := target.Port(); port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return "", fmt.Errorf("sportarr: image redirect location has invalid port")
		}
	}

	host := target.Hostname()
	if host == "" {
		return "", fmt.Errorf("sportarr: image redirect location has no host")
	}
	if literal := net.ParseIP(host); literal != nil {
		if !isGloballyRoutableIP(literal) {
			return "", fmt.Errorf("sportarr: image redirect target is not globally routable")
		}
		return target.String(), nil
	}

	addresses, err := c.lookupIP(ctx, host)
	if err != nil || len(addresses) == 0 {
		return "", fmt.Errorf("sportarr: image redirect target lookup failed")
	}
	for _, address := range addresses {
		if !isGloballyRoutableIP(address) {
			return "", fmt.Errorf("sportarr: image redirect target is not globally routable")
		}
	}
	return target.String(), nil
}

func isGloballyRoutableIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	} else if !globallyRoutableIPv6Range.Contains(ip) {
		return false
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, blocked := range nonGlobalIPRanges {
		if blocked.Contains(ip) {
			return false
		}
	}
	return true
}

var nonGlobalIPRanges = []*net.IPNet{
	mustParseCIDR("0.0.0.0/8"),
	mustParseCIDR("100.64.0.0/10"),
	mustParseCIDR("192.0.0.0/24"),
	mustParseCIDR("192.0.2.0/24"),
	mustParseCIDR("192.88.99.0/24"),
	mustParseCIDR("198.18.0.0/15"),
	mustParseCIDR("198.51.100.0/24"),
	mustParseCIDR("203.0.113.0/24"),
	mustParseCIDR("240.0.0.0/4"),
	mustParseCIDR("64:ff9b:1::/48"),
	mustParseCIDR("100::/64"),
	mustParseCIDR("100:0:0:1::/64"),
	mustParseCIDR("2001::/23"),
	mustParseCIDR("2001:2::/48"),
	mustParseCIDR("2001:10::/28"),
	mustParseCIDR("2001:20::/28"),
	mustParseCIDR("2001:30::/28"),
	mustParseCIDR("2002::/16"),
	mustParseCIDR("2001:db8::/32"),
	mustParseCIDR("3fff::/20"),
}

var globallyRoutableIPv6Range = mustParseCIDR("2000::/3")

func mustParseCIDR(cidr string) *net.IPNet {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	return network
}
