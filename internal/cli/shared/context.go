package shared

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/shhac/lib-agent-cli/creds"
	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-figma/internal/api"
	"github.com/shhac/agent-figma/internal/config"
	"github.com/shhac/agent-figma/internal/credential"
	"github.com/shhac/agent-figma/internal/output"
	"github.com/shhac/agent-figma/internal/ratelimit"
)

// EnvProfile names the budget and cache scope for a token that came from the
// flag or environment rather than a stored profile.
const EnvProfile = "env"

// Resolved is an authenticated client and the profile it belongs to.
type Resolved struct {
	Client *api.Client
	// Profile is the stored profile in use, or EnvProfile.
	Profile string
	// Scope names the cache and budget partition: the profile name, or
	// EnvProfile for a token from the flag or environment.
	Scope       string
	Settings    config.Profile
	TokenSource string
	Defaults    config.Defaults
}

var profileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidProfileName reports a name that is safe to use in file paths.
func ValidProfileName(name string) error {
	if !profileName.MatchString(name) {
		return agenterrors.New(fmt.Sprintf("profile name %q must be letters, digits, '.', '_' or '-' (max 64)", name), agenterrors.FixableByAgent)
	}
	return nil
}

// SelectedProfile is the profile the flags and environment pick, possibly empty.
func SelectedProfile(flags *GlobalFlags, cfg *config.Config) string {
	return creds.FirstNonEmpty(flags.Profile, os.Getenv("AGENT_FIGMA_PROFILE"), cfg.DefaultProfile)
}

// Resolve builds the client for the selected profile. Token precedence:
// --token, AGENT_FIGMA_TOKEN, the profile's stored token, then FIGMA_TOKEN,
// which is last so a variable exported for other Figma tools never overrides
// a profile configured here.
func Resolve(flags *GlobalFlags) (*Resolved, error) {
	cfg := config.Read()
	name := SelectedProfile(flags, cfg)
	settings, hasProfile := cfg.Profiles[name]
	if name != "" && !hasProfile && flags.Profile != "" {
		return nil, agenterrors.New(fmt.Sprintf("profile %q is not configured", name), agenterrors.FixableByAgent).
			WithHint("Run 'agent-figma auth list', or add it with 'agent-figma auth add " + name + " --form'.")
	}

	token, source := flags.Token, "flag"
	if token == "" {
		token, source = os.Getenv("AGENT_FIGMA_TOKEN"), "AGENT_FIGMA_TOKEN"
	}
	scope := EnvProfile
	if token == "" && hasProfile {
		stored, err := credential.Get(name)
		if err != nil {
			return nil, agenterrors.Wrap(err, agenterrors.FixableByHuman).
				WithHint("Re-add the token with 'agent-figma auth update " + name + " --form'.")
		}
		token, source, scope = stored, "profile", name
	}
	if token == "" {
		token, source = os.Getenv("FIGMA_TOKEN"), "FIGMA_TOKEN"
	}
	if token == "" && !flags.Offline {
		return nil, agenterrors.New("no Figma token configured", agenterrors.FixableByHuman).
			WithHint("Create a personal access token in Figma (Settings > Security > Personal access tokens), then run 'agent-figma auth add <profile> --form'.")
	}
	if source != "profile" {
		name, settings = "", config.Profile{}
	} else {
		warnExpiry(name, settings)
	}

	maxWait := time.Duration(flags.MaxWaitS) * time.Second
	client := api.New(creds.FirstNonEmpty(flags.BaseURL, os.Getenv("AGENT_FIGMA_BASE_URL"), api.DefaultBaseURL), token)
	client.MaxRetries = flags.MaxRetries
	client.MaxWait = maxWait
	client.Debug = flags.Debug
	client.DebugOut = output.Stderr()
	client.Limiter = &ratelimit.Limiter{
		Store:        creds.Store{Path: ratelimit.StatePath(config.AppName)},
		Profile:      scope,
		DeclaredSeat: settings.Seat,
		DeclaredPlan: settings.Plan,
		MaxWait:      maxWait,
	}
	client.OnGrant = func(g ratelimit.Grant) {
		if g.LowSeatTier1() {
			output.Notice(fmt.Sprintf("Spent a Tier 1 call on a monthly-limited budget (%d this month for this profile)", g.MonthCount),
				"Figma allows Collab/View seats and Starter files only a few Tier 1 calls a month; prefer cached reads and Tier 3 commands like 'file index'.")
		}
	}
	return &Resolved{Client: client, Profile: name, Scope: scope, Settings: settings, TokenSource: source, Defaults: cfg.Defaults}, nil
}

// WithClient resolves the client and runs fn under the --timeout deadline.
func WithClient(ctx context.Context, flags *GlobalFlags, fn func(context.Context, *Resolved) error) error {
	resolved, err := Resolve(flags)
	if err != nil {
		return err
	}
	if flags.TimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(flags.TimeoutMS)*time.Millisecond)
		defer cancel()
	}
	return fn(ctx, resolved)
}

// ExpiryWarningDays is how close to a recorded expiry date commands start warning.
const ExpiryWarningDays = 7

// DaysUntilExpiry is the whole days left before a YYYY-MM-DD expiry, and
// whether one is recorded.
func DaysUntilExpiry(expires string, now time.Time) (int, bool) {
	if expires == "" {
		return 0, false
	}
	t, err := time.Parse("2006-01-02", expires)
	if err != nil {
		return 0, false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return int(t.Sub(today).Hours() / 24), true
}

func warnExpiry(name string, settings config.Profile) {
	days, ok := DaysUntilExpiry(settings.Expires, time.Now())
	if !ok || days > ExpiryWarningDays {
		return
	}
	hint := "Generate a new token in Figma and run 'agent-figma auth update " + name + " --form --expires YYYY-MM-DD'."
	if days < 0 {
		output.Notice(fmt.Sprintf("The token for profile %q expired on %s", name, settings.Expires), hint)
		return
	}
	output.Notice(fmt.Sprintf("The token for profile %q expires in %d day(s), on %s", name, days, settings.Expires), hint)
}

// Scope names the cache partition the flags select, without needing a token:
// the env partition for a token from the flag or environment, otherwise the
// selected profile.
func Scope(flags *GlobalFlags) string {
	if flags.Token != "" || os.Getenv("AGENT_FIGMA_TOKEN") != "" {
		return EnvProfile
	}
	cfg := config.Read()
	if name := SelectedProfile(flags, cfg); name != "" {
		if _, ok := cfg.Profiles[name]; ok {
			return name
		}
	}
	return EnvProfile
}
