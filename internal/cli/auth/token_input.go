package auth

import (
	"context"
	"slices"
	"time"

	"github.com/shhac/lib-agent-cli/creds"
	clidialog "github.com/shhac/lib-agent-cli/dialog"
	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/config"
)

const formHint = "Agents should use 'agent-figma auth add <profile> --form' so the token never appears in chat or argv."

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
		filled, err := promptSecretDialog(cmd.Context(), "agent-figma: "+alias, "Figma personal access token", f.token)
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
