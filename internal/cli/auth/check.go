package auth

import (
	"context"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/api"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/config"
	"github.com/shhac/agent-figma/internal/figma"
	"github.com/shhac/agent-figma/internal/figmaurl"
	"github.com/shhac/agent-figma/internal/output"
)

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
				planToken := r.Settings.Kind == config.KindPlan
				if planToken && fileRef == "" {
					return agenterrors.New("plan access tokens cannot call /v1/me", agenterrors.FixableByAgent).
						WithHint("Verify with a file the token can read: 'agent-figma auth check <profile> --file <url-or-key>'.")
				}
				obs := r.Client.Limiter.Observe()
				record := map[string]any{"status": "ok", "token_source": r.TokenSource, "budget": obs}
				if r.Profile != "" {
					record = profileRecord(config.Read(), r.Profile, record)
					if s := shared.Suggestions(r.Profile, r.Settings, obs); len(s) > 0 {
						record["suggestions"] = s
					}
				}
				if fileRef != "" {
					file, err := checkFile(ctx, r, fileRef)
					if err != nil {
						return err
					}
					record["file"] = file
				}
				if !planToken {
					user, err := checkMe(ctx, r)
					if err != nil {
						return err
					}
					record["user"] = user
				}
				return output.WriteItem(record, flags.Format)
			})
		},
	}
	cmd.Flags().StringVar(&fileRef, "file", "", "Verify by reading this file's metadata (required for plan access tokens)")
	parent.AddCommand(cmd)
}

func checkFile(ctx context.Context, r *shared.Resolved, fileRef string) (map[string]any, error) {
	ref, err := figmaurl.Parse(fileRef)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	raw, err := r.Client.Get(ctx, "/v1/files/"+ref.Key()+"/meta", nil)
	if err != nil {
		return nil, err
	}
	meta, err := api.Decode[figma.MetaResponse](raw, "file meta")
	if err != nil {
		return nil, err
	}
	return map[string]any{"key": ref.Key(), "name": meta.File.Name, "role": meta.File.Role}, nil
}

func checkMe(ctx context.Context, r *shared.Resolved) (figma.User, error) {
	raw, err := r.Client.Get(ctx, "/v1/me", nil)
	if err != nil {
		return figma.User{}, err
	}
	return api.Decode[figma.User](raw, "user")
}
