// Package auth implements the profile and token commands.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/shhac/lib-agent-cli/creds"
	clidialog "github.com/shhac/lib-agent-cli/dialog"
	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/config"
	"github.com/shhac/agent-figma/internal/credential"
	"github.com/shhac/agent-figma/internal/figmaurl"
	"github.com/shhac/agent-figma/internal/output"
	"github.com/shhac/agent-figma/internal/ratelimit"
)

// PromptSecret is a variable so tests can stand in for the native dialog.
var PromptSecret = promptSecretDialog

const formHint = "Agents should use 'agent-figma auth add <profile> --form' so the token never appears in chat or argv."

func Register(root *cobra.Command, globals *shared.GlobalFlags) {
	auth := &cobra.Command{
		Use:   "auth",
		Short: "Manage Figma tokens and profiles",
	}
	registerAdd(auth, globals)
	registerUpdate(auth, globals)
	registerCheck(auth, globals)
	registerList(auth, globals)
	registerDefault(auth, globals)
	registerRemove(auth, globals)
	root.AddCommand(auth)
}

type profileFlags struct {
	token, kind, expires, seat, plan string
	form, makeDefault, clearExpires  bool
}

func (f *profileFlags) bind(cmd *cobra.Command, forUpdate bool) {
	cmd.Flags().StringVar(&f.token, "token", "", "Figma token (prefer --form, or pipe it on stdin)")
	cmd.Flags().BoolVar(&f.form, "form", false, "Prompt for the token via a native OS dialog")
	cmd.Flags().StringVar(&f.kind, "kind", "", "Token kind: pat (personal access token) or plan (plan access token)")
	cmd.Flags().StringVar(&f.expires, "expires", "", "Token expiry date as YYYY-MM-DD (Figma tokens last at most 90 days)")
	cmd.Flags().StringVar(&f.seat, "seat", "", "Your Figma seat: full, dev, collab, view, or unknown")
	cmd.Flags().StringVar(&f.plan, "plan", "", "The plan you mostly read files from: starter, pro, org, enterprise, student, or unknown")
	cmd.Flags().BoolVar(&f.makeDefault, "default", false, "Make this the default profile")
	if forUpdate {
		cmd.Flags().BoolVar(&f.clearExpires, "clear-expires", false, "Forget the recorded expiry date")
	}
}

func (f *profileFlags) validate(cmd *cobra.Command) error {
	if f.kind != "" && !slices.Contains(config.Kinds, f.kind) {
		return agenterrors.New("--kind takes pat or plan", agenterrors.FixableByAgent)
	}
	if f.seat != "" && !slices.Contains(config.Seats, f.seat) {
		return agenterrors.New("--seat takes full, dev, collab, view, or unknown", agenterrors.FixableByAgent)
	}
	if f.plan != "" && !slices.Contains(config.Plans, f.plan) {
		return agenterrors.New("--plan takes starter, pro, org, enterprise, student, or unknown", agenterrors.FixableByAgent)
	}
	if f.expires != "" {
		if _, err := time.Parse("2006-01-02", f.expires); err != nil {
			return agenterrors.New("--expires takes a date as YYYY-MM-DD", agenterrors.FixableByAgent)
		}
	}
	return nil
}

// readToken takes the token from --form, --token, or piped stdin.
func (f *profileFlags) readToken(cmd *cobra.Command, alias string) (string, error) {
	if f.form {
		filled, err := PromptSecret(cmd.Context(), "agent-figma: "+alias, "Figma personal access token", f.token)
		if err != nil {
			fixableBy, hint := classifyDialogError(err)
			return "", agenterrors.Wrap(err, fixableBy).WithHint(hint)
		}
		return filled, nil
	}
	token, err := creds.ReadSecret(cmd.InOrStdin(), f.token)
	if err != nil {
		return "", agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	return token, nil
}

func registerAdd(parent *cobra.Command, globals *shared.GlobalFlags) {
	var f profileFlags
	cmd := &cobra.Command{
		Use:   "add <profile>",
		Short: "Add a profile with a keychain-stored Figma token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]
			if err := shared.ValidProfileName(alias); err != nil {
				return err
			}
			if err := f.validate(cmd); err != nil {
				return err
			}
			token, err := f.readToken(cmd, alias)
			if err != nil {
				return err
			}
			if token == "" {
				return agenterrors.New("no token given", agenterrors.FixableByAgent).WithHint(formHint)
			}
			storage, err := credential.Store(alias, token)
			if err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByHuman).
					WithHint("The token was not saved. Fix keychain access and retry with --form.")
			}
			profile := config.Profile{
				Kind:      orDefault(f.kind, config.KindPAT),
				Expires:   f.expires,
				Seat:      f.seat,
				Plan:      f.plan,
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
			}
			if err := config.StoreProfile(alias, profile, f.makeDefault); err != nil {
				// Without the profile nothing lists or removes the secret, so take it back.
				_ = credential.Remove(alias)
				return agenterrors.Wrap(err, agenterrors.FixableByHuman)
			}
			return output.WriteItem(profileRecord(config.Read(), alias, map[string]any{"status": "added", "storage": storage}), globals.Format)
		},
	}
	f.bind(cmd, false)
	parent.AddCommand(cmd)
}

func registerUpdate(parent *cobra.Command, globals *shared.GlobalFlags) {
	var f profileFlags
	cmd := &cobra.Command{
		Use:   "update <profile>",
		Short: "Replace a profile's token or change its metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]
			if _, ok := config.Read().Profiles[alias]; !ok {
				return agenterrors.New("profile "+alias+" is not configured", agenterrors.FixableByAgent).
					WithHint("Run 'agent-figma auth list', or create it with 'agent-figma auth add " + alias + " --form'.")
			}
			if err := f.validate(cmd); err != nil {
				return err
			}
			result := map[string]any{"status": "updated"}
			token, err := f.readToken(cmd, alias)
			if err != nil {
				return err
			}
			if token != "" {
				storage, err := credential.Store(alias, token)
				if err != nil {
					return agenterrors.Wrap(err, agenterrors.FixableByHuman)
				}
				result["token"] = "replaced"
				result["storage"] = storage
			}
			changed := func(name string) bool { return cmd.Flags().Changed(name) }
			if err := config.UpdateProfile(alias, func(p *config.Profile) {
				if changed("kind") {
					p.Kind = f.kind
				}
				if changed("expires") {
					p.Expires = f.expires
				}
				if f.clearExpires {
					p.Expires = ""
				}
				if changed("seat") {
					p.Seat = f.seat
				}
				if changed("plan") {
					p.Plan = f.plan
				}
			}); err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByAgent)
			}
			if f.makeDefault {
				if err := config.SetDefault(alias); err != nil {
					return agenterrors.Wrap(err, agenterrors.FixableByAgent)
				}
			}
			return output.WriteItem(profileRecord(config.Read(), alias, result), globals.Format)
		},
	}
	f.bind(cmd, true)
	parent.AddCommand(cmd)
}

func registerCheck(parent *cobra.Command, globals *shared.GlobalFlags) {
	var fileRef string
	cmd := &cobra.Command{
		Use:   "check [profile]",
		Short: "Verify a token against Figma and show what is known about its budget",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := *globals
			if len(args) > 0 {
				flags.Profile = args[0]
			}
			return shared.WithClient(cmd.Context(), &flags, func(ctx context.Context, r *shared.Resolved) error {
				record := map[string]any{
					"status":       "ok",
					"profile":      r.Profile,
					"token_source": r.TokenSource,
					"budget":       r.Client.Limiter.Observe(),
				}
				if r.Profile != "" {
					record["kind"] = orDefault(r.Settings.Kind, config.KindPAT)
					record["declared_seat"] = orDefault(r.Settings.Seat, "unknown")
					record["declared_plan"] = orDefault(r.Settings.Plan, "unknown")
					if r.Settings.Expires != "" {
						record["expires"] = r.Settings.Expires
						days, _ := shared.DaysUntilExpiry(r.Settings.Expires, time.Now())
						record["expires_in_days"] = days
					}
				}
				if r.Settings.Kind == config.KindPlan || fileRef != "" {
					if fileRef == "" {
						return agenterrors.New("plan access tokens cannot call /v1/me", agenterrors.FixableByAgent).
							WithHint("Verify with a file the token can read: 'agent-figma auth check <profile> --file <url-or-key>'.")
					}
					ref, err := figmaurl.Parse(fileRef)
					if err != nil {
						return agenterrors.Wrap(err, agenterrors.FixableByAgent)
					}
					raw, err := r.Client.Get(ctx, "/v1/files/"+ref.Key()+"/meta", nil)
					if err != nil {
						return err
					}
					var meta struct {
						File struct {
							Name string `json:"name"`
							Role string `json:"role"`
						} `json:"file"`
					}
					_ = json.Unmarshal(raw, &meta)
					record["file"] = map[string]any{"key": ref.Key(), "name": meta.File.Name, "role": meta.File.Role}
				}
				if r.Settings.Kind != config.KindPlan {
					raw, err := r.Client.Get(ctx, "/v1/me", nil)
					if err != nil {
						return err
					}
					var me struct {
						ID     string `json:"id"`
						Handle string `json:"handle"`
						Email  string `json:"email"`
					}
					_ = json.Unmarshal(raw, &me)
					record["user"] = map[string]any{"id": me.ID, "handle": me.Handle, "email": me.Email}
				}
				return output.WriteItem(record, flags.Format)
			})
		},
	}
	cmd.Flags().StringVar(&fileRef, "file", "", "Verify by reading this file's metadata (required for plan access tokens)")
	parent.AddCommand(cmd)
}

func registerList(parent *cobra.Command, globals *shared.GlobalFlags) {
	parent.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List profiles without exposing tokens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Read()
			rows := make([]any, 0, len(cfg.Profiles))
			for _, alias := range slices.Sorted(maps.Keys(cfg.Profiles)) {
				limiter := ratelimit.Limiter{Store: creds.Store{Path: ratelimit.StatePath(config.AppName)}, Profile: alias, DeclaredSeat: cfg.Profiles[alias].Seat}
				rows = append(rows, profileRecord(cfg, alias, map[string]any{
					"credential": credential.Storage(alias),
					"budget":     limiter.Observe(),
				}))
			}
			return output.WriteList(rows, nil, globals.Format)
		},
	})
}

func registerDefault(parent *cobra.Command, globals *shared.GlobalFlags) {
	parent.AddCommand(&cobra.Command{
		Use:   "default <profile>",
		Short: "Set the default profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.SetDefault(args[0]); err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByAgent).WithHint("Run 'agent-figma auth list'.")
			}
			return output.WriteItem(map[string]any{"status": "default_set", "profile": args[0]}, globals.Format)
		},
	})
}

func registerRemove(parent *cobra.Command, globals *shared.GlobalFlags) {
	parent.AddCommand(&cobra.Command{
		Use:   "remove <profile>",
		Short: "Remove a profile, its stored token, and its cache",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]
			if err := shared.ValidProfileName(alias); err != nil {
				return err
			}
			_, hasProfile := config.Read().Profiles[alias]
			var notFound *credential.NotFoundError
			err := credential.Remove(alias)
			tokenAlreadyGone := errors.As(err, &notFound) && hasProfile
			if err != nil && !tokenAlreadyGone {
				return agenterrors.Wrap(err, agenterrors.FixableByAgent).WithHint("Run 'agent-figma auth list'.")
			}
			if err := config.RemoveProfile(alias); err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByHuman)
			}
			if err := cache.PurgeProfile(alias); err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByHuman).WithHint("The profile was removed but its cache was not; run 'agent-figma cache path' and delete it.")
			}
			return output.WriteItem(map[string]any{"status": "removed", "profile": alias}, globals.Format)
		},
	})
}

func profileRecord(cfg *config.Config, alias string, extra map[string]any) map[string]any {
	p := cfg.Profiles[alias]
	record := map[string]any{
		"profile":       alias,
		"default":       cfg.DefaultProfile == alias,
		"kind":          orDefault(p.Kind, config.KindPAT),
		"declared_seat": orDefault(p.Seat, "unknown"),
		"declared_plan": orDefault(p.Plan, "unknown"),
	}
	if p.Expires != "" {
		record["expires"] = p.Expires
		days, _ := shared.DaysUntilExpiry(p.Expires, time.Now())
		record["expires_in_days"] = days
	}
	maps.Copy(record, extra)
	return record
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// promptSecretDialog opens a masked native prompt seeded with initial, so a
// token never transits argv.
func promptSecretDialog(ctx context.Context, title, label, initial string) (string, error) {
	res, err := clidialog.Prompt(ctx, clidialog.Spec{
		Title: title,
		Items: []clidialog.Field{{ID: "secret", Label: label, InputType: clidialog.Password, Initial: initial}},
	})
	if err != nil {
		return "", err
	}
	for _, r := range res {
		if r.ID == "secret" {
			return r.Value, nil
		}
	}
	return "", nil
}

// classifyDialogError maps a dialog failure onto fixable_by, so a cancelled
// prompt reads as retry and a headless host as human.
func classifyDialogError(err error) (agenterrors.FixableBy, string) {
	category, hint := clidialog.ClassifyError(err)
	switch category {
	case clidialog.CategoryHuman:
		return agenterrors.FixableByHuman, hint
	case clidialog.CategoryRetry:
		return agenterrors.FixableByRetry, hint
	default:
		return agenterrors.FixableByAgent, hint
	}
}
