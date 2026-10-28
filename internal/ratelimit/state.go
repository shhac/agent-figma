package ratelimit

import "time"

// The persisted budget state, shared by every agent-figma process through
// one locked file.

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
