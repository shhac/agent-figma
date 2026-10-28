package ratelimit

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/shhac/lib-agent-cli/creds"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }
func (c *clock) sleep(_ context.Context, d time.Duration) error {
	c.t = c.t.Add(d)
	return nil
}

func newLimiter(t *testing.T, c *clock) *Limiter {
	t.Helper()
	return &Limiter{
		Store:   creds.Store{Path: filepath.Join(t.TempDir(), "ratelimit.json")},
		Profile: "work",
		MaxWait: time.Minute,
		Now:     c.now,
		Sleep:   c.sleep,
	}
}

func TestTierFor(t *testing.T) {
	cases := map[string]Tier{
		"/v1/files/KEY":                 Tier1,
		"/v1/files/KEY/nodes":           Tier1,
		"/v1/images/KEY":                Tier1,
		"/v1/files/KEY/images":          Tier2,
		"/v1/files/KEY/meta":            Tier3,
		"/v1/files/KEY/components":      Tier3,
		"/v1/teams/1/styles":            Tier3,
		"/v1/me":                        Tier3,
		"/v1/files/KEY/comments":        Tier2,
		"/v1/files/KEY/variables/local": Tier2,
		"/v1/files/KEY/versions":        Tier2,
		"/v1/files/KEY/dev_resources":   Tier2,
		"/v1/component_sets/abc":        Tier3,
	}
	for path, want := range cases {
		if got := TierFor(path); got != want {
			t.Errorf("TierFor(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestPerMinute(t *testing.T) {
	if PerMinute(SeatHigh, "pro", Tier1) != 10 || PerMinute(SeatHigh, "enterprise", Tier3) != 150 {
		t.Fatal("high seat budgets wrong")
	}
	if PerMinute(SeatLow, "enterprise", Tier1) != 0 || PerMinute(SeatLow, "org", Tier2) != 5 {
		t.Fatal("low seat budgets wrong")
	}
	if PerMinute(SeatHigh, "starter", Tier3) != 10 {
		t.Fatal("starter plans get the low budgets")
	}
	if PerMinute(SeatHigh, "unknown", Tier1) != 10 {
		t.Fatal("unknown plans assume pro")
	}
}

func TestAcquirePacesWithinWindow(t *testing.T) {
	c := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	l := newLimiter(t, c)
	start := c.t
	for i := 0; i < 10; i++ {
		if _, err := l.Acquire(context.Background(), Tier1, "K"); err != nil {
			t.Fatal(err)
		}
	}
	if !c.t.Equal(start) {
		t.Fatal("the first 10 Tier 1 calls on pro/high should not wait")
	}
	grant, err := l.Acquire(context.Background(), Tier1, "K")
	if err != nil {
		t.Fatal(err)
	}
	if c.t.Sub(start) < time.Minute {
		t.Fatalf("11th call waited only %s", c.t.Sub(start))
	}
	if grant.MonthCount != 11 {
		t.Fatalf("month count = %d", grant.MonthCount)
	}
}

func TestObserve429StartsSharedCooldown(t *testing.T) {
	c := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	l := newLimiter(t, c)
	h := http.Header{}
	h.Set("X-Figma-Plan-Tier", "org")
	h.Set("X-Figma-Rate-Limit-Type", "low")
	l.Observe429(Tier1, "K", h, 2*time.Hour)

	other := *l // a second process sharing the state file
	_, err := other.Acquire(context.Background(), Tier1, "K")
	var rle *RateLimitedError
	if !errors.As(err, &rle) || !rle.Local || rle.RateLimitType != "low" || rle.PlanTier != "org" {
		t.Fatalf("Acquire during a long cooldown = %v", err)
	}
	if l.Observe().SeatClass != SeatLow {
		t.Fatal("observations not recorded")
	}

	short := &clock{t: c.t}
	l2 := newLimiter(t, short)
	l2.Observe429(Tier3, "K", http.Header{}, 30*time.Second)
	if _, err := l2.Acquire(context.Background(), Tier3, "K"); err != nil {
		t.Fatalf("a short cooldown should be waited out: %v", err)
	}
	if short.t.Sub(c.t) < 30*time.Second {
		t.Fatal("did not wait for the cooldown")
	}
}

func TestPlanBucketsAreSeparate(t *testing.T) {
	c := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	l := newLimiter(t, c)
	h := http.Header{}
	h.Set("X-Figma-Plan-Tier", "starter")
	l.Observe429(Tier1, "STARTERFILE", h, 2*time.Hour)
	if _, err := l.Acquire(context.Background(), Tier1, "STARTERFILE"); err == nil {
		t.Fatal("the starter file's bucket should be cooling down")
	}
	if _, err := l.Acquire(context.Background(), Tier1, "OTHERFILE"); err != nil {
		t.Fatalf("another plan's file should not be blocked: %v", err)
	}
}

func TestDeclaredSeatWins(t *testing.T) {
	c := &clock{t: time.Now()}
	l := newLimiter(t, c)
	l.DeclaredSeat = "view"
	g, err := l.Acquire(context.Background(), Tier1, "K")
	if err != nil || !g.LowSeatTier1() {
		t.Fatalf("grant = %+v, %v", g, err)
	}
}

func TestFullWindowRefusesWhenWaitTooLong(t *testing.T) {
	c := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	l := newLimiter(t, c)
	l.MaxWait = time.Second
	for i := 0; i < 10; i++ {
		if _, err := l.Acquire(context.Background(), Tier1, "K"); err != nil {
			t.Fatal(err)
		}
	}
	_, err := l.Acquire(context.Background(), Tier1, "K")
	var rle *RateLimitedError
	if !errors.As(err, &rle) || !rle.Local || rle.RetryAfter < 59*time.Second || rle.RateLimitType != SeatHigh {
		t.Fatalf("11th call with a 1s max wait: %v", err)
	}
}

func TestCorruptStateFailsOpen(t *testing.T) {
	c := &clock{t: time.Now()}
	l := newLimiter(t, c)
	if err := os.WriteFile(l.Store.Path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Acquire(context.Background(), Tier1, "K"); err != nil {
		t.Fatalf("an unreadable state file must not block requests: %v", err)
	}
}

func TestCancelledWaitReturnsContextError(t *testing.T) {
	l := newLimiter(t, &clock{t: time.Now()})
	l.Now, l.Sleep = nil, nil
	l.Observe429(Tier3, "K", http.Header{}, 30*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.Acquire(ctx, Tier3, "K"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestConcurrentProcessesShareTheWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ratelimit.json")
	var mu sync.Mutex
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	counts := make(chan int, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l := &Limiter{
				Store: creds.Store{Path: path}, Profile: "work", MaxWait: time.Hour,
				Now: func() time.Time { mu.Lock(); defer mu.Unlock(); return now },
				Sleep: func(_ context.Context, d time.Duration) error {
					mu.Lock()
					defer mu.Unlock()
					now = now.Add(d)
					return nil
				},
			}
			g, err := l.Acquire(context.Background(), Tier1, "K")
			if err != nil {
				t.Error(err)
			}
			counts <- g.MonthCount
		}()
	}
	wg.Wait()
	close(counts)
	seen := map[int]bool{}
	for c := range counts {
		if seen[c] {
			t.Fatalf("month count %d granted twice: a lost update", c)
		}
		seen[c] = true
	}
	if len(seen) != 20 {
		t.Fatalf("%d grants recorded", len(seen))
	}
}
