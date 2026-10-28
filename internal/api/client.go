// Package api is agent-figma's HTTP client for the Figma REST API: auth
// headers, the shared rate limiter, retries, error mapping, and --debug
// records. It never sends the token anywhere but the configured API host.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-figma/internal/ratelimit"
)

const DefaultBaseURL = "https://api.figma.com"

type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient Doer
	Limiter    *ratelimit.Limiter
	MaxRetries int
	// MaxWait is the longest Retry-After the client waits out itself.
	MaxWait  time.Duration
	Debug    bool
	DebugOut io.Writer
	// OnGrant runs after the limiter admits a request, so callers can warn
	// about spending a scarce Tier 1 call.
	OnGrant func(ratelimit.Grant)
	// On429 runs after a 429 is observed, so callers can act on what it
	// revealed about the seat and plan.
	On429 func()
	Sleep func(context.Context, time.Duration) error

	mu    sync.Mutex
	calls map[ratelimit.Tier]int
}

// New builds a client. Every request goes through limiter, which paces
// requests, records 429s, and sits out short cooldowns.
func New(baseURL, token string, limiter *ratelimit.Limiter) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Token:      token,
		HTTPClient: &http.Client{},
		Limiter:    limiter,
		MaxRetries: 2,
		MaxWait:    60 * time.Second,
	}
}

// Calls reports how many requests were sent per tier.
func (c *Client) Calls() map[ratelimit.Tier]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[ratelimit.Tier]int{}
	for k, v := range c.calls {
		out[k] = v
	}
	return out
}

func (c *Client) count(tier ratelimit.Tier) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.calls == nil {
		c.calls = map[ratelimit.Tier]int{}
	}
	c.calls[tier]++
}

// Get calls a Figma endpoint and returns the raw JSON body.
func (c *Client) Get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.get(ctx, path, query, true)
}

// GetNoServerRetry is Get without retries on 5xx, for renders, where a 500
// usually means the render itself failed and repeating it spends budget for
// the same answer.
func (c *Client) GetNoServerRetry(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.get(ctx, path, query, false)
}

const max429Waits = 5

func (c *Client) get(ctx context.Context, path string, query url.Values, retry5xx bool) (json.RawMessage, error) {
	tier := ratelimit.TierFor(path)
	fileKey := FileKeyFromPath(path)
	failures, waits := 0, 0
	var waited time.Duration
	for {
		started := time.Now()
		grant, err := c.Limiter.Acquire(ctx, tier, fileKey)
		waited += time.Since(started)
		if err != nil {
			return nil, c.limitError(err, fileKey)
		}
		if c.OnGrant != nil {
			c.OnGrant(grant)
		}
		req, err := c.newRequest(ctx, path, query)
		if err != nil {
			return nil, err
		}
		c.count(tier)
		status, header, raw, err := c.send(req)
		switch {
		case err != nil && ctx.Err() != nil:
			return nil, timedOut(ctx.Err())
		case err != nil && failures >= c.MaxRetries:
			return nil, agenterrors.Wrap(err, agenterrors.FixableByRetry).WithHint("Network request to Figma failed after retries")
		case err != nil:
		case status == http.StatusTooManyRequests:
			// The cooldown Observe429 records makes the next Acquire wait, so
			// a short Retry-After is sat out without spending a retry.
			after := retryAfter(header.Get("Retry-After"))
			rle := c.Limiter.Observe429(tier, fileKey, header, after)
			if c.On429 != nil {
				c.On429()
			}
			if waited+after > c.MaxWait || waits >= max429Waits {
				return nil, c.limitError(rle, fileKey)
			}
			waits++
			continue
		case retryableStatus(status, retry5xx) && failures < c.MaxRetries:
		case status < 200 || status >= 300:
			return nil, mapError(status, raw, path)
		default:
			return json.RawMessage(raw), nil
		}
		failures++
		if err := c.wait(ctx, backoff(failures)); err != nil {
			return nil, timedOut(err)
		}
	}
}

func retryableStatus(status int, retry5xx bool) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	case http.StatusInternalServerError:
		return retry5xx
	}
	return false
}

func (c *Client) send(req *http.Request) (int, http.Header, []byte, error) {
	started := time.Now()
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		c.debug(map[string]any{"debug": "request_failed", "url": req.URL.String(), "error": err.Error()})
		return 0, nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, nil, err
	}
	c.debug(map[string]any{
		"debug":   "response",
		"url":     req.URL.String(),
		"status":  resp.StatusCode,
		"tier":    ratelimit.TierFor(req.URL.Path).String(),
		"bytes":   len(raw),
		"elapsed": time.Since(started).Round(time.Millisecond).String(),
	})
	return resp.StatusCode, resp.Header, raw, nil
}

func (c *Client) debug(fields map[string]any) {
	if c.Debug && c.DebugOut != nil {
		_ = json.NewEncoder(c.DebugOut).Encode(fields)
	}
}

func (c *Client) newRequest(ctx context.Context, path string, query url.Values) (*http.Request, error) {
	base, err := url.Parse(c.BaseURL)
	if err != nil || base.Host == "" {
		return nil, agenterrors.New("invalid Figma API base URL "+c.BaseURL, agenterrors.FixableByAgent).
			WithHint("Check --base-url or AGENT_FIGMA_BASE_URL")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := *base
	u.Path = strings.TrimRight(base.Path, "/") + path
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("X-Figma-Token", c.Token)
	}
	return req, nil
}

func (c *Client) wait(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

var fileKeyInPath = regexp.MustCompile(`^/v1/(?:files|images)/([^/]+)`)

// FileKeyFromPath extracts the file key from file-scoped endpoint paths.
func FileKeyFromPath(path string) string {
	if m := fileKeyInPath.FindStringSubmatch(path); m != nil {
		return m[1]
	}
	return ""
}

func backoff(attempt int) time.Duration {
	return time.Duration(250*(1<<attempt)) * time.Millisecond
}

func retryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Minute
	}
	var seconds int
	if _, err := fmt.Sscanf(value, "%d", &seconds); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
		return 0
	}
	return time.Minute
}

func timedOut(err error) error {
	return agenterrors.Wrap(err, agenterrors.FixableByRetry).WithHint("The request ran out of time; retry, or raise --timeout.")
}
