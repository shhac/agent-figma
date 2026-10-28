// Package auth implements the profile and token commands.
package auth

import (
	"cmp"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/shhac/lib-agent-cli/creds"
	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/config"
	"github.com/shhac/agent-figma/internal/credential"
	"github.com/shhac/agent-figma/internal/output"
	"github.com/shhac/agent-figma/internal/ratelimit"
)

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
				Kind:      cmp.Or(f.kind, config.KindPAT),
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

func registerList(parent *cobra.Command, globals *shared.GlobalFlags) {
	parent.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List profiles without exposing tokens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Read()
			rows := make([]any, 0, len(cfg.Profiles))
			for _, alias := range slices.Sorted(maps.Keys(cfg.Profiles)) {
				settings := cfg.Profiles[alias]
				limiter := ratelimit.Limiter{Store: creds.Store{Path: ratelimit.StatePath(config.AppName)}, Profile: alias, DeclaredSeat: settings.Seat, DeclaredPlan: settings.Plan}
				obs := limiter.Observe()
				extra := map[string]any{"credential": credential.Storage(alias), "budget": obs}
				if s := shared.Suggestions(alias, settings, obs); len(s) > 0 {
					extra["suggestions"] = s
				}
				rows = append(rows, profileRecord(cfg, alias, extra))
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
		"kind":          cmp.Or(p.Kind, config.KindPAT),
		"declared_seat": cmp.Or(p.Seat, "unknown"),
		"declared_plan": cmp.Or(p.Plan, "unknown"),
	}
	if p.Expires != "" {
		record["expires"] = p.Expires
		days, _ := shared.DaysUntilExpiry(p.Expires, time.Now())
		record["expires_in_days"] = days
	}
	maps.Copy(record, extra)
	return record
}
