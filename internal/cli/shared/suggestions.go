package shared

import (
	"errors"
	"slices"
	"strings"
	"time"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-figma/internal/api"
	"github.com/shhac/agent-figma/internal/config"
	"github.com/shhac/agent-figma/internal/output"
	"github.com/shhac/agent-figma/internal/ratelimit"
)

// Suggestion is a settings change the tool has evidence for, with the exact
// command that makes it. Suggestions never change a profile by themselves.
type Suggestion struct {
	Reason  string `json:"reason"`
	Command string `json:"command"`
}

// Suggestions compares a profile's declarations with what Figma has
// reported for it.
func Suggestions(profile string, settings config.Profile, obs ratelimit.Observation) []Suggestion {
	if profile == "" {
		return nil
	}
	var out []Suggestion
	declared := ratelimit.SeatClass(settings.Seat)
	switch {
	case obs.RateLimitType == ratelimit.SeatLow && declared == ratelimit.SeatHigh:
		out = append(out, Suggestion{
			Reason:  "Figma reports a Collab/View seat's limits, but the profile declares --seat " + settings.Seat,
			Command: "agent-figma auth update " + profile + " --seat view",
		})
	case obs.RateLimitType == ratelimit.SeatHigh && declared == ratelimit.SeatLow:
		out = append(out, Suggestion{
			Reason:  "Figma reports a Full/Dev seat's limits, but the profile declares --seat " + settings.Seat,
			Command: "agent-figma auth update " + profile + " --seat full",
		})
	}
	if plan := obs.PlanTier; plan != "" && plan != settings.Plan {
		reason := "Figma reports the " + plan + " plan, but the profile declares --plan " + settings.Plan
		if settings.Plan == "" || settings.Plan == "unknown" {
			reason = "Figma reports the " + plan + " plan, and the profile declares none"
		}
		out = append(out, Suggestion{Reason: reason, Command: "agent-figma auth update " + profile + " --plan " + plan})
	}
	return out
}

// revokedSuggestion covers a token Figma rejects before its recorded expiry.
func revokedSuggestion(profile string, settings config.Profile, err error) []Suggestion {
	he, ok := api.AsHTTPError(err)
	if profile == "" || !ok || he.Status != 403 || !strings.Contains(strings.ToLower(he.Message), "token") {
		return nil
	}
	if days, ok := DaysUntilExpiry(settings.Expires, time.Now()); !ok || days < 0 {
		return nil
	}
	return []Suggestion{{
		Reason:  "Figma rejected the token before its recorded expiry (" + settings.Expires + "): it was revoked, or the date is wrong",
		Command: "agent-figma auth update " + profile + " --form --expires YYYY-MM-DD",
	}}
}

// adviseAfter folds suggestions into the error that revealed them, or, when
// this command made a new observation without failing, prints them once.
func adviseAfter(r *Resolved, observed bool, err error) error {
	suggestions := revokedSuggestion(r.Profile, r.Settings, err)
	if r.Profile != "" && (observed || isRateLimit(err)) {
		suggestions = append(suggestions, Suggestions(r.Profile, r.Settings, r.Client.Limiter.Observe())...)
	}
	if len(suggestions) == 0 {
		return err
	}
	var ae *agenterrors.Error
	if err != nil && errors.As(err, &ae) {
		hints := []string{ae.Hint}
		for _, s := range suggestions {
			hints = append(hints, s.Reason+"; run: "+s.Command)
		}
		ae.Hint = strings.Join(slices.DeleteFunc(hints, func(h string) bool { return h == "" }), " ")
		return ae
	}
	for _, s := range suggestions {
		output.Notice(s.Reason, "Run: "+s.Command)
	}
	return err
}

func isRateLimit(err error) bool {
	var rle *ratelimit.RateLimitedError
	return errors.As(err, &rle)
}
