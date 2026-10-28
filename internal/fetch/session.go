// Package fetch answers questions about a Figma file cache-first: it decides
// which file version is current, reads what the cache already holds, and
// otherwise spends as few Figma calls as it can, widening each Tier 1 call
// with ids the agent is likely to ask for next.
package fetch

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-figma/internal/api"
	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/config"
	"github.com/shhac/agent-figma/internal/figma"
	"github.com/shhac/agent-figma/internal/figmaurl"
	"github.com/shhac/agent-figma/internal/output"
	"github.com/shhac/agent-figma/internal/ratelimit"
)

type Options struct {
	Refresh    bool
	Offline    bool
	NoPrefetch bool
	FreshFor   time.Duration
	ServeStale string
	Prefetch   string
}

// Session is one command's view of one file.
type Session struct {
	Client *api.Client
	Ref    figmaurl.Ref
	Opts   Options
	File   cache.File

	CacheHits  int
	Prefetched struct{ Nodes, Renders int }

	// FileInfo is filled in as the session learns about the file.
	FileInfo FileInfo
	now      func() time.Time
}

// FileInfo is the @file metadata line.
type FileInfo struct {
	Key           string `json:"key"`
	BranchKey     string `json:"branch_key,omitempty"`
	Name          string `json:"name,omitempty"`
	Version       string `json:"version,omitempty"`
	LastModified  string `json:"last_modified,omitempty"`
	EditorType    string `json:"editor_type,omitempty"`
	Stale         bool   `json:"stale,omitempty"`
	LatestVersion string `json:"latest_version,omitempty"`
}

func NewSession(client *api.Client, scope string, ref figmaurl.Ref, opts Options) *Session {
	return &Session{
		Client:   client,
		Ref:      ref,
		Opts:     opts,
		File:     cache.OpenFile(scope, ref.Key()),
		FileInfo: FileInfo{Key: ref.FileKey, BranchKey: ref.BranchKey},
		now:      time.Now,
	}
}

// Key is the key reads that accept a branch key use.
func (s *Session) Key() string { return s.Ref.Key() }

// MainKey is the key for endpoints that refuse branch keys (library, dev resources).
func (s *Session) MainKey() string { return s.Ref.FileKey }

func (s *Session) prefetching() bool {
	return !s.Opts.NoPrefetch && s.Opts.Prefetch != config.PrefetchOff
}

// lowSeat reports a monthly Tier 1 budget for this file, where reusing an
// older version beats spending a call.
func (s *Session) lowSeat() bool { return s.Client.Limiter.MonthlyTier1(s.Key()) }

func (s *Session) servesStale() bool {
	switch s.Opts.ServeStale {
	case config.ServeStaleOn:
		return true
	case config.ServeStaleOff:
		return false
	}
	return s.lowSeat()
}

// Unresolved is a requested id that produced no image.
type Unresolved struct {
	ID        string `json:"id"`
	Reason    string `json:"reason"`
	FixableBy string `json:"fixable_by"`
	Hint      string `json:"hint,omitempty"`
}

// Budget is the @budget metadata line.
func (s *Session) Budget() map[string]any {
	calls := s.Client.Calls()
	return map[string]any{
		"calls":      map[string]int{"tier1": calls[ratelimit.Tier1], "tier2": calls[ratelimit.Tier2], "tier3": calls[ratelimit.Tier3]},
		"cache_hits": s.CacheHits,
		"prefetched": map[string]int{"nodes": s.Prefetched.Nodes, "renders": s.Prefetched.Renders},
	}
}

// Meta calls GET /v1/files/:key/meta (Tier 3) and records the version it reports.
func (s *Session) Meta(ctx context.Context) (figma.FileMeta, error) {
	raw, err := s.Client.Get(ctx, "/v1/files/"+s.Key()+"/meta", nil)
	if err != nil {
		return figma.FileMeta{}, err
	}
	resp, err := api.Decode[figma.MetaResponse](raw, "file meta")
	if err != nil {
		return figma.FileMeta{}, err
	}
	if resp.File.Version != "" {
		_ = s.File.SetHead(cache.Head{Version: resp.File.Version, CheckedAt: s.now(), Name: resp.File.Name})
	}
	s.FileInfo.Name = resp.File.Name
	s.FileInfo.EditorType = resp.File.EditorType
	s.FileInfo.LastModified = resp.File.LastTouchedAt
	return resp.File, nil
}

// CurrentVersion decides which version to answer from. An empty result means
// it could not be determined; the next Tier 1 response will say.
func (s *Session) CurrentVersion(ctx context.Context) (string, error) {
	version, err := s.currentVersion(ctx)
	if err == nil && version != "" {
		s.FileInfo.Version = version
		if st, lerr := s.File.LoadNodes(version); lerr == nil && st.Name != "" {
			s.FileInfo.Name, s.FileInfo.LastModified, s.FileInfo.EditorType = st.Name, st.LastModified, st.EditorType
		}
	}
	return version, err
}

func (s *Session) currentVersion(ctx context.Context) (string, error) {
	head, haveHead := s.File.Head()
	if haveHead {
		s.FileInfo.Name = head.Name
	}
	if s.Opts.Offline {
		if haveHead {
			return head.Version, nil
		}
		if versions := s.File.Versions(); len(versions) > 0 {
			return versions[0], nil
		}
		return "", offlineMiss()
	}
	if haveHead && s.fresh(head.CheckedAt) {
		return head.Version, nil
	}
	meta, err := s.Meta(ctx)
	if err == nil && meta.Version != "" {
		return meta.Version, nil
	}
	if he, ok := api.AsHTTPError(err); ok && he.Status == http.StatusForbidden {
		output.Notice("Could not check the file's version with /meta, so the cache was not validated",
			"Add the file_metadata:read scope to the token so cached data can be reused safely.")
		return "", nil
	}
	if err != nil && !haveHead {
		return "", err
	}
	if err != nil {
		output.Notice("Could not re-check the file's version; answering from the last known version", err.Error())
	}
	return head.Version, nil
}

// fresh reports something fetched at t that is still inside the freshness
// window, unless --refresh asked to look again.
func (s *Session) fresh(t time.Time) bool {
	return !s.Opts.Refresh && s.now().Sub(t) < s.Opts.FreshFor
}

func offlineMiss() error {
	return agenterrors.New("the cache cannot answer this and --offline forbids calling Figma", agenterrors.FixableByAgent).
		WithHint("Drop --offline, or check 'agent-figma cache status' for the profile and file key in use.")
}

func joinIDs(ids []string) string { return strings.Join(ids, ",") }

func query(pairs ...string) url.Values {
	q := url.Values{}
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			q.Set(pairs[i], pairs[i+1])
		}
	}
	return q
}

// withExtras sends need plus the speculative extras; when that fails in a way
// that dropping them might fix, it retries with need alone. It returns the
// extras that were actually sent, so callers credit only those as prefetched.
func withExtras[T any](need, extras []string, call func([]string) (T, error)) (T, []string, error) {
	if len(extras) > 0 {
		got, err := call(append(append([]string{}, need...), extras...))
		if err == nil || !retryableWithoutExtras(err) {
			return got, extras, err
		}
	}
	got, err := call(need)
	return got, nil, err
}

// retryableWithoutExtras reports failures where dropping speculative extras
// might help: an oversized request, a failed render, or a server error.
func retryableWithoutExtras(err error) bool {
	if api.IsRequestTooLarge(err) || api.IsRenderFailure(err) {
		return true
	}
	he, ok := api.AsHTTPError(err)
	return ok && he.Status >= 500
}
