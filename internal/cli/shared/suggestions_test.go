package shared

import (
	"strings"
	"testing"
	"time"

	"github.com/shhac/agent-figma/internal/api"
	"github.com/shhac/agent-figma/internal/config"
	"github.com/shhac/agent-figma/internal/ratelimit"
)

func TestSuggestions(t *testing.T) {
	cases := []struct {
		name     string
		profile  string
		settings config.Profile
		obs      ratelimit.Observation
		want     []string
	}{
		{"no profile, no nudge", "", config.Profile{Seat: "full"}, ratelimit.Observation{RateLimitType: "low"}, nil},
		{"low limits on a declared full seat", "work", config.Profile{Seat: "full"}, ratelimit.Observation{RateLimitType: "low"}, []string{"--seat view"}},
		{"high limits on a declared view seat", "work", config.Profile{Seat: "view"}, ratelimit.Observation{RateLimitType: "high"}, []string{"--seat full"}},
		{"matching seat", "work", config.Profile{Seat: "view"}, ratelimit.Observation{RateLimitType: "low"}, nil},
		{"undeclared plan", "work", config.Profile{}, ratelimit.Observation{PlanTier: "org"}, []string{"--plan org"}},
		{"declared plan seen on another file", "work", config.Profile{Plan: "pro"}, ratelimit.Observation{PlanTier: "org", PlansSeen: []string{"org", "pro"}}, nil},
		{"declared plan never seen", "work", config.Profile{Plan: "starter"}, ratelimit.Observation{PlanTier: "org", PlansSeen: []string{"org"}}, []string{"--plan org"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Suggestions(tc.profile, tc.settings, tc.obs)
			if len(got) != len(tc.want) {
				t.Fatalf("got %+v, want %v", got, tc.want)
			}
			for i, w := range tc.want {
				if !strings.Contains(got[i].Command, w) {
					t.Fatalf("got %q, want %q", got[i].Command, w)
				}
			}
		})
	}
}

func TestRevokedSuggestionOnlyBeforeExpiry(t *testing.T) {
	invalid := &api.HTTPError{Status: 403, Message: "Invalid token"}
	future := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	past := time.Now().AddDate(0, -1, 0).Format("2006-01-02")
	if len(revokedSuggestion("work", config.Profile{Expires: future}, wrap(invalid))) != 1 {
		t.Fatal("a token rejected before its expiry should nudge")
	}
	for _, tc := range []struct {
		settings config.Profile
		err      error
	}{
		{config.Profile{Expires: past}, wrap(invalid)},
		{config.Profile{}, wrap(invalid)},
		{config.Profile{Expires: future}, wrap(&api.HTTPError{Status: 403, Message: "Limited by Figma plan"})},
	} {
		if s := revokedSuggestion("work", tc.settings, tc.err); len(s) != 0 {
			t.Fatalf("unexpected nudge for %+v: %+v", tc, s)
		}
	}
}

type wrapped struct{ cause error }

func (w wrapped) Error() string { return w.cause.Error() }
func (w wrapped) Unwrap() error { return w.cause }

func wrap(err error) error { return wrapped{err} }
