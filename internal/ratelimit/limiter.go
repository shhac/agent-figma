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

type state struct {
	Profiles map[string]*profileState `json:"profiles,omitempty"`
	Files    map[string]*fileState    `json:"files,omitempty"`
}

type profileState struct {
	RateLimitType string                `json:"rate_limit_type,omitempty"`
	ObservedAt    time.Time             `json:"observed_at,omitempty"`
	UpgradeLink   string                `json:"upgrade_link,omitempty"`
	Tiers         map[string]*tierState `json:"tiers,omitempty"`
}

type tierState struct {
	CooldownUntil time.Time   `json:"cooldown_until,omitempty"`
	Recent        []time.Time `json:"recent,omitempty"`
	Month         string      `json:"month,omitempty"`
	MonthCount    int         `json:"month_count,omitempty"`
}

type fileState struct {
	PlanTier   string    `json:"plan_tier,omitempty"`
	ObservedAt time.Time `json:"observed_at,omitempty"`
}

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
// without waiting when the required wait is longer than MaxWait.
func (l *Limiter) Acquire(ctx context.Context, tier Tier, fileKey string) (Grant, error) {
	for {
		var wait time.Duration
		var grant Grant
		var refused *RateLimitedError
		err := l.update(func(s *state) {
			now := l.now()
			ps := s.profile(l.Profile)
			ts := ps.bucket(s.planBucket(fileKey), tier)
			seat := l.seatClass(ps)
			plan := l.plan(s, fileKey)
			if now.Before(ts.CooldownUntil) {
				wait = ts.CooldownUntil.Sub(now)
				if wait > l.MaxWait {
					refused = &RateLimitedError{Tier: tier, RetryAfter: wait, PlanTier: plan, RateLimitType: ps.RateLimitType, UpgradeLink: ps.UpgradeLink, Local: true}
				}
				return
			}
			ts.Recent = pruneBefore(ts.Recent, now.Add(-window))
			if budget := PerMinute(seat, plan, tier); budget > 0 && len(ts.Recent) >= budget {
				wait = ts.Recent[len(ts.Recent)-budget].Add(window).Sub(now)
				if wait > l.MaxWait {
					refused = &RateLimitedError{Tier: tier, RetryAfter: wait, PlanTier: plan, RateLimitType: seat, Local: true}
				}
				return
			}
			wait = 0
			ts.Recent = append(ts.Recent, now)
			month := now.UTC().Format("2006-01")
			if ts.Month != month {
				ts.Month, ts.MonthCount = month, 0
			}
			ts.MonthCount++
			grant = Grant{Tier: tier, Seat: seat, Plan: plan, MonthCount: ts.MonthCount}
		})
		if err != nil {
			// Budget state is advisory; an unreadable file must not block requests.
			return Grant{Tier: tier}, nil
		}
		if refused != nil {
			return Grant{}, refused
		}
		if wait <= 0 {
			return grant, nil
		}
		if err := l.sleep(ctx, wait); err != nil {
			return Grant{}, err
		}
	}
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

// FilePlan is the plan tier Figma last reported for a file, if any.
func (l *Limiter) FilePlan(fileKey string) string {
	var s state
	_ = l.Store.Load(&s)
	if fs := s.Files[fileKey]; fs != nil {
		return fs.PlanTier
	}
	return ""
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

func (l *Limiter) plan(s *state, fileKey string) string {
	if fs := s.Files[fileKey]; fs != nil && fs.PlanTier != "" {
		return fs.PlanTier
	}
	if l.DeclaredPlan != "" && l.DeclaredPlan != "unknown" {
		return l.DeclaredPlan
	}
	return "pro"
}

func (l *Limiter) update(mutate func(*state)) error {
	var s state
	return l.Store.Update(&s, func() error {
		s.ensure()
		mutate(&s)
		return nil
	})
}

func (s *state) ensure() {
	if s.Profiles == nil {
		s.Profiles = map[string]*profileState{}
	}
	if s.Files == nil {
		s.Files = map[string]*fileState{}
	}
}

func (s *state) profile(name string) *profileState {
	ps := s.Profiles[name]
	if ps == nil {
		ps = &profileState{}
		s.Profiles[name] = ps
	}
	if ps.Tiers == nil {
		ps.Tiers = map[string]*tierState{}
	}
	return ps
}

// planBucket is the plan Figma last reported for the file. Figma meters each
// plan separately, so a 429 on one plan's file must not stall another's.
func (s *state) planBucket(fileKey string) string {
	if fs := s.Files[fileKey]; fs != nil && fs.PlanTier != "" {
		return fs.PlanTier
	}
	return "unknown"
}

func (ps *profileState) bucket(plan string, t Tier) *tierState {
	key := plan + "/" + t.String()
	ts := ps.Tiers[key]
	if ts == nil {
		ts = &tierState{}
		ps.Tiers[key] = ts
	}
	return ts
}

func pruneBefore(times []time.Time, cutoff time.Time) []time.Time {
	kept := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	return kept
}
