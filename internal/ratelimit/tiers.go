// Package ratelimit keeps agent-figma inside Figma's per-tier request budgets.
// Figma meters requests, not ids, in a leaky bucket per user and plan, and the
// budget depends on the seat behind the token and the plan that owns the file.
// State lives in one file under the XDG state dir so the short-lived processes
// an agent starts in parallel share cooldowns and pacing.
package ratelimit

import (
	"regexp"
	"strconv"
)

type Tier int

const (
	Tier1 Tier = 1
	Tier2 Tier = 2
	Tier3 Tier = 3
)

func (t Tier) String() string { return "tier" + strconv.Itoa(int(t)) }

// Seat classes as Figma's X-Figma-Rate-Limit-Type reports them.
const (
	SeatLow  = "low"  // Collab and View seats
	SeatHigh = "high" // Full and Dev seats
)

var tierPatterns = []struct {
	tier    Tier
	pattern *regexp.Regexp
}{
	{Tier1, regexp.MustCompile(`^/v1/files/[^/]+(/nodes)?$`)},
	{Tier1, regexp.MustCompile(`^/v1/images/[^/]+$`)},
	{Tier3, regexp.MustCompile(`^/v1/files/[^/]+/meta$`)},
	{Tier3, regexp.MustCompile(`^/v1/files/[^/]+/(components|component_sets|styles)$`)},
	{Tier3, regexp.MustCompile(`^/v1/(components|component_sets|styles)/[^/]+$`)},
	{Tier3, regexp.MustCompile(`^/v1/teams/[^/]+/(components|component_sets|styles)$`)},
	{Tier3, regexp.MustCompile(`^/v1/(me|oembed)$`)},
	{Tier3, regexp.MustCompile(`^/v1/projects/[^/]+/meta$`)},
	{Tier3, regexp.MustCompile(`^/v2/folders/[^/]+/meta$`)},
	{Tier3, regexp.MustCompile(`^/v1/(analytics|activity_logs|developer_logs|payments)(/|$)`)},
}

// TierFor classifies an API path. Anything unrecognised is treated as Tier 2,
// where most of the remaining endpoints sit.
func TierFor(path string) Tier {
	for _, tp := range tierPatterns {
		if tp.pattern.MatchString(path) {
			return tp.tier
		}
	}
	return Tier2
}

// PerMinute is the documented budget for a seat class, plan tier, and endpoint
// tier. Zero means the budget is not per minute (Tier 1 on a low seat is a
// small monthly allowance), so pacing does not apply.
func PerMinute(seat, plan string, tier Tier) int {
	if seat == SeatLow || plan == "starter" {
		switch tier {
		case Tier2:
			return 5
		case Tier3:
			return 10
		default:
			return 0
		}
	}
	row := map[string][3]int{
		"pro":        {10, 25, 50},
		"org":        {15, 50, 100},
		"enterprise": {20, 100, 150},
	}
	budget, ok := row[plan]
	if !ok {
		budget = row["pro"]
	}
	return budget[int(tier)-1]
}

// SeatClass maps a declared seat onto Figma's low/high split.
func SeatClass(seat string) string {
	switch seat {
	case "full", "dev":
		return SeatHigh
	case "collab", "view":
		return SeatLow
	}
	return ""
}
