package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shhac/lib-agent-cli/creds"
	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-figma/internal/ratelimit"
)

func noSleep(context.Context, time.Duration) error { return nil }

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	now := time.Now()
	c := New(srv.URL, "fake-token", &ratelimit.Limiter{
		Store:   creds.Store{Path: filepath.Join(t.TempDir(), "rl.json")},
		Profile: "test",
		MaxWait: time.Minute,
		Now:     func() time.Time { return now },
		Sleep: func(_ context.Context, d time.Duration) error {
			now = now.Add(d)
			return nil
		},
	})
	c.Sleep = noSleep
	return c
}

func TestSendsFigmaTokenHeader(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Figma-Token") != "fake-token" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	if _, err := c.Get(context.Background(), "/v1/me", nil); err != nil {
		t.Fatal(err)
	}
	if c.Calls()[ratelimit.Tier3] != 1 {
		t.Fatalf("calls = %v", c.Calls())
	}
}

func TestShort429IsWaitedOutWithoutSpendingRetries(t *testing.T) {
	var n atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) <= 3 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	c.MaxRetries = 0
	if _, err := c.Get(context.Background(), "/v1/files/KEY", nil); err != nil {
		t.Fatalf("expected success after 429s, got %v", err)
	}
}

func TestLong429ReturnsStructuredError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "86400")
		w.Header().Set("X-Figma-Plan-Tier", "pro")
		w.Header().Set("X-Figma-Rate-Limit-Type", "low")
		w.Header().Set("X-Figma-Upgrade-Link", "https://www.figma.com/pricing")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := c.Get(context.Background(), "/v1/files/KEY", nil)
	var ae *agenterrors.Error
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v", err)
	}
	if ae.FixableBy != agenterrors.FixableByHuman || ae.RetryAfterSeconds != 86400 {
		t.Fatalf("got %+v", ae)
	}
	var rle *ratelimit.RateLimitedError
	if !errors.As(err, &rle) || rle.UpgradeLink == "" {
		t.Fatalf("cause = %v", err)
	}
	// A second call must not reach Figma while the cooldown runs.
	_, err = c.Get(context.Background(), "/v1/files/KEY", nil)
	if !errors.As(err, &rle) || !rle.Local {
		t.Fatalf("second call err = %v", err)
	}
}

func TestRenderFailureIsNotRetried(t *testing.T) {
	var n atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"status":500,"err":"Unexpected rendering error"}`))
	})
	_, err := c.GetNoServerRetry(context.Background(), "/v1/images/KEY", nil)
	if !IsRenderFailure(err) || n.Load() != 1 {
		t.Fatalf("err = %v after %d calls", err, n.Load())
	}
}

func TestRequestTooLargeIsRecognised(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":400,"err":"Request too large. If applicable, filter by query params."}`))
	})
	_, err := c.Get(context.Background(), "/v1/files/KEY", nil)
	if !IsRequestTooLarge(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestErrorShapes(t *testing.T) {
	if got := figmaMessage([]byte(`{"error":true,"status":403,"message":"Invalid token"}`)); got != "Invalid token" {
		t.Fatalf("message shape: %q", got)
	}
	if got := figmaMessage([]byte(`{"status":404,"err":"Not found"}`)); got != "Not found" {
		t.Fatalf("err shape: %q", got)
	}
	var ae *agenterrors.Error
	if !errors.As(mapError(403, []byte(`{"status":403,"err":"Invalid token"}`), "/v1/me"), &ae) || ae.FixableBy != agenterrors.FixableByHuman {
		t.Fatal("invalid token should be human-fixable")
	}
}

func TestFileKeyFromPath(t *testing.T) {
	if FileKeyFromPath("/v1/images/ABC") != "ABC" || FileKeyFromPath("/v1/files/ABC/nodes") != "ABC" || FileKeyFromPath("/v1/me") != "" {
		t.Fatal("file key extraction")
	}
}

func statusSequence(t *testing.T, statuses ...int) (*Client, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		i := int(n.Add(1)) - 1
		status := statuses[min(i, len(statuses)-1)]
		if status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "0")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"status":` + strconv.Itoa(status) + `,"err":"x"}`))
	})
	return c, &n
}

func TestServerErrorsRetryThenSucceedOrGiveUp(t *testing.T) {
	c, n := statusSequence(t, 503, 503, 200)
	if _, err := c.Get(context.Background(), "/v1/me", nil); err != nil || n.Load() != 3 {
		t.Fatalf("503,503,200: err=%v calls=%d", err, n.Load())
	}
	c, n = statusSequence(t, 503)
	_, err := c.Get(context.Background(), "/v1/me", nil)
	var ae *agenterrors.Error
	if !errors.As(err, &ae) || ae.FixableBy != agenterrors.FixableByRetry || n.Load() != 3 {
		t.Fatalf("503 forever: err=%v calls=%d", err, n.Load())
	}
}

func TestRendersRetryGatewayErrorsButNot500(t *testing.T) {
	c, n := statusSequence(t, 502, 200)
	if _, err := c.GetNoServerRetry(context.Background(), "/v1/images/KEY", nil); err != nil || n.Load() != 2 {
		t.Fatalf("502 then 200: err=%v calls=%d", err, n.Load())
	}
}

func TestRepeated429sStopAfterCap(t *testing.T) {
	c, n := statusSequence(t, 429)
	if _, err := c.Get(context.Background(), "/v1/me", nil); err == nil || n.Load() != max429Waits+1 {
		t.Fatalf("endless 429s: err=%v calls=%d", err, n.Load())
	}
}

func TestNetworkFailureAndCancellation(t *testing.T) {
	c := newTestClient(t, func(http.ResponseWriter, *http.Request) {})
	c.BaseURL = "http://127.0.0.1:1"
	var ae *agenterrors.Error
	if _, err := c.Get(context.Background(), "/v1/me", nil); !errors.As(err, &ae) || ae.FixableBy != agenterrors.FixableByRetry {
		t.Fatalf("unreachable host: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Get(ctx, "/v1/me", nil); !errors.As(err, &ae) || !strings.Contains(ae.Hint, "--timeout") {
		t.Fatalf("cancelled context: %v", err)
	}
}

func TestRetryAfterParsing(t *testing.T) {
	if retryAfter("7") != 7*time.Second || retryAfter("") != time.Minute || retryAfter("abc") != time.Minute {
		t.Fatal("seconds, missing, garbage")
	}
	when := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	if d := retryAfter(when); d < 80*time.Second || d > 91*time.Second {
		t.Fatalf("http date: %s", d)
	}
}

func TestTokenNeverReachesDebugOutput(t *testing.T) {
	var buf strings.Builder
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	c.Debug, c.DebugOut = true, &buf
	if _, err := c.Get(context.Background(), "/v1/me", nil); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 || strings.Contains(buf.String(), "fake-token") {
		t.Fatalf("debug output: %q", buf.String())
	}
}
