package shared

import (
	"context"
	"time"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-figma/internal/fetch"
	"github.com/shhac/agent-figma/internal/figmaurl"
	"github.com/shhac/agent-figma/internal/output"
)

// ParseRef resolves a <ref> argument and any --node values into a ref whose
// NodeIDs are the --node values, or the link's node-id when none were given.
func ParseRef(arg string, nodeFlags []string) (figmaurl.Ref, error) {
	ref, err := figmaurl.Parse(arg)
	if err != nil {
		return figmaurl.Ref{}, agenterrors.Wrap(err, agenterrors.FixableByAgent).
			WithHint("Pass a figma.com link or a bare file key; 'agent-figma resolve <url>' shows how a link parses.")
	}
	var ids []string
	for _, raw := range nodeFlags {
		parsed, err := figmaurl.ParseNodeIDs(raw)
		if err != nil {
			return figmaurl.Ref{}, agenterrors.Wrap(err, agenterrors.FixableByAgent)
		}
		ids = append(ids, parsed...)
	}
	if len(ids) > 0 {
		ref.NodeIDs = ids
	}
	return ref, nil
}

// RequireNodes fails when a command that needs node ids got none.
func RequireNodes(ref figmaurl.Ref, command string) error {
	if len(ref.NodeIDs) > 0 {
		return nil
	}
	return agenterrors.New(command+" needs at least one node id", agenterrors.FixableByAgent).
		WithHint("Pass --node <id> or a link with ?node-id=; 'agent-figma file outline <ref>' lists node ids.")
}

// NewSession opens the cache-first view of the file a ref names.
func NewSession(r *Resolved, flags *GlobalFlags, ref figmaurl.Ref) *fetch.Session {
	return fetch.NewSession(r.Client, r.Scope, ref, fetch.Options{
		Refresh:    flags.Refresh,
		Offline:    flags.Offline,
		NoPrefetch: flags.NoPrefetch,
		FreshFor:   time.Duration(r.Defaults.EffectiveCacheFreshS()) * time.Second,
		ServeStale: r.Defaults.EffectiveServeStale(),
		Prefetch:   r.Defaults.EffectivePrefetch(),
	})
}

// Finish writes the trailing @budget line and closes the stream.
func Finish(stream *output.Stream, session *fetch.Session) error {
	stream.Meta(output.MetaBudget, session.Budget())
	return stream.Close()
}

// WithSession resolves the client and runs fn with the file session for ref.
func WithSession(ctx context.Context, flags *GlobalFlags, ref figmaurl.Ref, fn func(context.Context, *fetch.Session) error) error {
	return WithClient(ctx, flags, func(ctx context.Context, r *Resolved) error {
		return fn(ctx, NewSession(r, flags, ref))
	})
}

// FileStream opens the output stream with the @file line first. Open it after
// fetching, so a failed fetch prints nothing on stdout.
func FileStream(flags *GlobalFlags, session *fetch.Session) (*output.Stream, error) {
	stream, err := output.NewStream(flags.Format)
	if err != nil {
		return nil, err
	}
	stream.Meta(output.MetaFile, session.FileInfo)
	return stream, nil
}
