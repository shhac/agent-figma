package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/shhac/lib-agent-cli/creds"
	"github.com/shhac/lib-agent-cli/xdg"
)

const window = time.Minute

// Limiter paces and gates requests for one profile.
type Limiter struct {
	Store   creds.Store
	Profile string
	// DeclaredSeat and DeclaredPlan come from the profile; empty or "unknown"
	// defers to what Figma has reported.
	DeclaredSeat string
	DeclaredPlan string
	MaxWait      time.Duration
	Now          func() time.Time
	Sleep        func(context.Context, time.Duration) error
}

// RateLimitedError is a request the limiter refused to send, or a 429 Figma
// returned, that is not worth waiting out.
type RateLimitedError struct {
	Tier          Tier
	RetryAfter    time.Duration
	PlanTier      string
	RateLimitType string
	UpgradeLink   string
	// Local is true when no request was sent: a cooldown from an earlier 429
	// was still running.
	Local bool
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("Figma %s rate limit reached; retry in %s", e.Tier, e.RetryAfter.Round(time.Second))
}

// StatePath is where the shared state lives.
func StatePath(app string) string {
	return filepath.Join(xdg.StateDir(app), "ratelimit.json")
}

func (l *Limiter) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *Limiter) sleep(ctx context.Context, d time.Duration) error {
	if l.Sleep != nil {
		return l.Sleep(ctx, d)
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

// Acquire waits until a request to tier may be sent and records it. It fails
// without waiting when the required wait is longer than MaxWait. The state
// lock is held only to decide, never while sleeping.
func (l *Limiter) Acquire(ctx context.Context, tier Tier, fileKey string) (Grant, error) {
	for {
		var d decision
		err := l.update(func(s *state) { d = l.admit(s, tier, fileKey, l.now()) })
		if err != nil {
			// Budget state is advisory; an unreadable file must not block requests.
			return Grant{Tier: tier}, nil
		}
		if d.refused != nil {
			return Grant{}, d.refused
		}
		if d.wait <= 0 {
			return d.grant, nil
		}
		if err := l.sleep(ctx, d.wait); err != nil {
			return Grant{}, err
		}
	}
}

type decision struct {
	grant   Grant
	wait    time.Duration
	refused *RateLimitedError
}

// admit decides one request against the state: wait out a cooldown or a full
// per-minute window, refuse when that wait is longer than MaxWait, or record
// the request and grant it.
func (l *Limiter) admit(s *state, tier Tier, fileKey string, now time.Time) decision {
	ps := s.profile(l.Profile)
	ts := ps.bucket(s.planBucket(fileKey), tier)
	seat, plan := l.seatClass(ps), l.plan(s, fileKey)
	refuse := func(wait time.Duration) decision {
		if wait <= l.MaxWait {
			return decision{wait: wait}
		}
		return decision{refused: &RateLimitedError{Tier: tier, RetryAfter: wait, PlanTier: plan, RateLimitType: seat, UpgradeLink: ps.UpgradeLink, Local: true}}
	}
	if now.Before(ts.CooldownUntil) {
		return refuse(ts.CooldownUntil.Sub(now))
	}
	ts.Recent = pruneBefore(ts.Recent, now.Add(-window))
	if budget := PerMinute(seat, plan, tier); budget > 0 && len(ts.Recent) >= budget {
		return refuse(ts.Recent[len(ts.Recent)-budget].Add(window).Sub(now))
	}
	ts.Recent = append(ts.Recent, now)
	if month := now.UTC().Format("2006-01"); ts.Month != month {
		ts.Month, ts.MonthCount = month, 0
	}
	ts.MonthCount++
	return decision{grant: Grant{Tier: tier, Seat: seat, Plan: plan, MonthCount: ts.MonthCount}}
}

// Grant describes the budget a request was sent under.
type Grant struct {
	Tier       Tier
	Seat       string
	Plan       string
	MonthCount int
}

// LowSeatTier1 reports a Tier 1 call on a seat whose Tier 1 budget is monthly.
func (g Grant) LowSeatTier1() bool {
	return g.Tier == Tier1 && (g.Seat == SeatLow || g.Plan == "starter")
}

// Observe429 records what a 429 told us and starts the cooldown.
func (l *Limiter) Observe429(tier Tier, fileKey string, h http.Header, retryAfter time.Duration) *RateLimitedError {
	rle := &RateLimitedError{
		Tier:          tier,
		RetryAfter:    retryAfter,
		PlanTier:      h.Get("X-Figma-Plan-Tier"),
		RateLimitType: h.Get("X-Figma-Rate-Limit-Type"),
		UpgradeLink:   h.Get("X-Figma-Upgrade-Link"),
	}
	_ = l.update(func(s *state) {
		now := l.now()
		ps := s.profile(l.Profile)
		if rle.RateLimitType != "" {
			ps.RateLimitType = rle.RateLimitType
			ps.ObservedAt = now
		}
		if rle.UpgradeLink != "" {
			ps.UpgradeLink = rle.UpgradeLink
		}
		if rle.PlanTier != "" && fileKey != "" {
			s.Files[fileKey] = &fileState{PlanTier: rle.PlanTier, ObservedAt: now}
		}
		ts := ps.bucket(s.planBucket(fileKey), tier)
		if until := now.Add(retryAfter); until.After(ts.CooldownUntil) {
			ts.CooldownUntil = until
		}
	})
	if rle.PlanTier == "" {
		rle.PlanTier = l.DeclaredPlan
	}
	return rle
}

// Observation is what is known about a profile's budget, for auth check.
type Observation struct {
	RateLimitType string            `json:"observed_rate_limit_type,omitempty"`
	ObservedAt    *time.Time        `json:"observed_at,omitempty"`
	SeatClass     string            `json:"effective_seat_class"`
	Cooldowns     map[string]string `json:"cooldowns,omitempty"`
	Tier1Month    int               `json:"tier1_calls_this_month"`
}

func (l *Limiter) Observe() Observation {
	var s state
	_ = l.Store.Load(&s)
	s.ensure()
	ps := s.profile(l.Profile)
	obs := Observation{RateLimitType: ps.RateLimitType, SeatClass: l.seatClass(ps)}
	if !ps.ObservedAt.IsZero() {
		at := ps.ObservedAt
		obs.ObservedAt = &at
	}
	now := l.now()
	for name, ts := range ps.Tiers {
		if now.Before(ts.CooldownUntil) {
			if obs.Cooldowns == nil {
				obs.Cooldowns = map[string]string{}
			}
			obs.Cooldowns[name] = ts.CooldownUntil.UTC().Format(time.RFC3339)
		}
	}
	month := now.UTC().Format("2006-01")
	for name, ts := range ps.Tiers {
		if strings.HasSuffix(name, "/"+Tier1.String()) && ts.Month == month {
			obs.Tier1Month += ts.MonthCount
		}
	}
	return obs
}

func (l *Limiter) seatClass(ps *profileState) string {
	if c := SeatClass(l.DeclaredSeat); c != "" {
		return c
	}
	if ps.RateLimitType != "" {
		return ps.RateLimitType
	}
	return SeatHigh
}

// MonthlyTier1 reports whether Tier 1 for this file is a small monthly
// allowance: a Collab/View seat, or a file on a Starter plan.
func (l *Limiter) MonthlyTier1(fileKey string) bool {
	var s state
	_ = l.Store.Load(&s)
	s.ensure()
	return l.seatClass(s.profile(l.Profile)) == SeatLow || l.plan(&s, fileKey) == "starter"
}

func (l *Limiter) plan(s *state, fileKey string) string {
	if fs := s.Files[fileKey]; fs != nil && fs.PlanTier != "" {
		return fs.PlanTier
	}
	if l.DeclaredPlan != "" && l.DeclaredPlan != "unknown" {
		return l.DeclaredPlan
	}
	return "pro"
}
