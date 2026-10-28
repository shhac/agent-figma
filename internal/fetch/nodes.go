package fetch

import (
	"cmp"
	"context"
	"strconv"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-figma/internal/api"
	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/figma"
)

// maxNodeExtras caps speculative ids per node call; each brings its subtree.
const maxNodeExtras = 10

// NodesResult is what a node query answered with.
type NodesResult struct {
	Store *cache.NodeStore
	// Missing are requested ids Figma does not have.
	Missing []string
	// Cut are requested ids whose subtrees came back depth-limited because
	// Figma refused the full request as too large.
	Cut []string
}

// Nodes makes sure each id's subtree is cached to depth levels (cache.Full
// for whole subtrees) and returns the store holding it.
func (s *Session) Nodes(ctx context.Context, ids []string, depth int) (*NodesResult, error) {
	version, err := s.CurrentVersion(ctx)
	if err != nil {
		return nil, err
	}
	satisfied := func(st *cache.NodeStore) bool {
		for _, id := range ids {
			if !st.Has(id, depth) && !st.IsMissing(id) {
				return false
			}
		}
		return true
	}
	store, err := s.answerFromCache(version, satisfied)
	if err != nil {
		return nil, err
	}
	if store != nil {
		return &NodesResult{Store: store, Missing: missingOf(store, ids)}, nil
	}
	// What to fetch is judged against this version only; an older version's
	// cache is a guide to what is related, not to what is current.
	current, err := s.File.LoadNodes(cacheVersion(version))
	if err != nil {
		current = cache.NewNodeStore(version)
	}
	var need []string
	for _, id := range ids {
		if !current.Has(id, depth) && !current.IsMissing(id) {
			need = append(need, id)
		}
	}
	if len(need) == 0 {
		return &NodesResult{Store: current, Missing: missingOf(current, ids)}, nil
	}
	known := s.knownStore(version)
	extras := []string{}
	if s.prefetching() {
		extras = relatedIDs(known, need, s.cachedVariantSets(version), maxNodeExtras)
	}
	result, err := s.fetchNodes(ctx, need, extras, depth, "")
	if err != nil {
		return nil, err
	}
	result.Missing = missingOf(result.Store, ids)
	return result, nil
}

func missingOf(store *cache.NodeStore, ids []string) []string {
	var out []string
	for _, id := range ids {
		if store.IsMissing(id) {
			out = append(out, id)
		}
	}
	return out
}

// Document makes sure the file's tree is cached to depth levels from the
// document root.
func (s *Session) Document(ctx context.Context, depth int) (*cache.NodeStore, error) {
	version, err := s.CurrentVersion(ctx)
	if err != nil {
		return nil, err
	}
	satisfied := func(st *cache.NodeStore) bool { return st.Root != "" && st.Has(st.Root, depth) }
	store, err := s.answerFromCache(version, satisfied)
	if err != nil || store != nil {
		return store, err
	}
	for d := depth; ; d-- {
		resp, err := s.getFile(ctx, query("depth", depthParam(d)))
		if api.IsRequestTooLarge(err) && d > 1 {
			continue
		}
		if err != nil {
			return nil, err
		}
		return s.storeFile(resp, nil, d, "")
	}
}

// answerFromCache returns a store that satisfies the query: the current
// version's, an older version's when stale answers are allowed (or offline),
// or nil when Figma must be asked. Offline with no answer is an error.
func (s *Session) answerFromCache(version string, satisfied func(*cache.NodeStore) bool) (*cache.NodeStore, error) {
	if version != "" {
		if st, err := s.File.LoadNodes(version); err == nil && satisfied(st) {
			s.CacheHits++
			s.noteStore(st, false, "")
			return st, nil
		}
	}
	// --refresh asks for the current version, so it never settles for an older one.
	if s.Opts.Offline || (s.servesStale() && !s.Opts.Refresh) {
		for _, older := range s.File.Versions() {
			if older == version {
				continue
			}
			if st, err := s.File.LoadNodes(older); err == nil && satisfied(st) {
				s.CacheHits++
				s.noteStore(st, version != "", version)
				return st, nil
			}
		}
	}
	if s.Opts.Offline {
		return nil, offlineMiss()
	}
	return nil, nil
}

// knownStore is the best cached knowledge for planning extras: the current
// version's store, or the newest older one when the current has nothing yet
// (an edit just bumped the version). Structure rarely changes between edits,
// so an older version is a good guide to what is related.
func (s *Session) knownStore(version string) *cache.NodeStore {
	candidates := append([]string{version}, s.File.Versions()...)
	for _, v := range candidates {
		if v == "" {
			continue
		}
		if st, err := s.File.LoadNodes(v); err == nil && len(st.Nodes) > 0 {
			return st
		}
	}
	return cache.NewNodeStore(version)
}

// fetchNodes fetches need (and the speculative extras) at version, or at the
// current version when version is empty.
func (s *Session) fetchNodes(ctx context.Context, need, extras []string, depth int, version string) (*NodesResult, error) {
	if depth != cache.Full {
		return s.fetchNodesEndpoint(ctx, need, extras, depth, version)
	}
	resp, extras, err := withExtras(need, extras, func(ids []string) (*figma.FileResponse, error) {
		return s.getFile(ctx, query("ids", joinIDs(ids), "version", version))
	})
	if api.IsRequestTooLarge(err) {
		for _, d := range []int{4, 2, 1} {
			res, nerr := s.fetchNodesEndpoint(ctx, need, nil, d, version)
			if api.IsRequestTooLarge(nerr) {
				continue
			}
			if nerr == nil {
				res.Cut = need
			}
			return res, nerr
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	store, err := s.storeFile(resp, append(append([]string{}, need...), extras...), cache.Full, version)
	if err != nil {
		return nil, err
	}
	s.Prefetched.Nodes += countNew(store, extras)
	return &NodesResult{Store: store}, nil
}

func (s *Session) fetchNodesEndpoint(ctx context.Context, need, extras []string, depth int, version string) (*NodesResult, error) {
	get := func(ids []string) (*figma.NodesResponse, error) {
		raw, err := s.Client.Get(ctx, "/v1/files/"+s.Key()+"/nodes", query("ids", joinIDs(ids), "depth", depthParam(depth), "version", version))
		if err != nil {
			return nil, err
		}
		resp, err := api.Decode[figma.NodesResponse](raw, "nodes")
		if err != nil {
			return nil, err
		}
		return &resp, nil
	}
	resp, extras, err := withExtras(need, extras, get)
	if err != nil {
		return nil, err
	}
	store, err := s.File.UpdateNodes(s.versionOf(cmp.Or(resp.Version, version)), func(st *cache.NodeStore) error {
		_, merr := st.MergeNodes(resp, depth)
		return merr
	})
	if err != nil {
		return nil, cacheError(err)
	}
	if version == "" {
		s.recordVersion(resp.Version, resp.Name)
		s.noteStore(store, false, "")
	}
	s.Prefetched.Nodes += countNew(store, extras)
	return &NodesResult{Store: store}, nil
}

func (s *Session) getFile(ctx context.Context, q map[string][]string) (*figma.FileResponse, error) {
	raw, err := s.Client.Get(ctx, "/v1/files/"+s.Key(), q)
	if err != nil {
		return nil, err
	}
	resp, err := api.Decode[figma.FileResponse](raw, "file")
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// storeFile merges a file response. A response for a pinned (older) version
// is stored under it without moving the file's head.
func (s *Session) storeFile(resp *figma.FileResponse, requested []string, depth int, pinned string) (*cache.NodeStore, error) {
	store, err := s.File.UpdateNodes(s.versionOf(cmp.Or(resp.Version, pinned)), func(st *cache.NodeStore) error {
		return st.MergeFile(resp, requested, depth)
	})
	if err != nil {
		return nil, cacheError(err)
	}
	if pinned == "" {
		s.recordVersion(resp.Version, resp.Name)
		s.noteStore(store, false, "")
	}
	return store, nil
}

// versionOf files content under the version the response reports, so an
// edit between the freshness check and the fetch cannot mix versions.
func (s *Session) versionOf(reported string) string {
	if reported != "" {
		return reported
	}
	if head, ok := s.File.Head(); ok {
		return head.Version
	}
	return "unknown"
}

func (s *Session) recordVersion(version, name string) {
	if version == "" {
		return
	}
	_ = s.File.SetHead(cache.Head{Version: version, CheckedAt: s.now(), Name: name})
}

func (s *Session) noteStore(st *cache.NodeStore, stale bool, latest string) {
	s.FileInfo.Version = st.Version
	if st.Name != "" {
		s.FileInfo.Name = st.Name
	}
	s.FileInfo.LastModified = st.LastModified
	s.FileInfo.EditorType = st.EditorType
	s.FileInfo.Stale = stale
	if stale {
		s.FileInfo.LatestVersion = latest
	}
}

func depthParam(depth int) string {
	if depth == cache.Full {
		return ""
	}
	return strconv.Itoa(max(depth, 1))
}

func countNew(store *cache.NodeStore, extras []string) int {
	n := 0
	for _, id := range extras {
		if store.Has(id, 0) {
			n++
		}
	}
	return n
}

func cacheError(err error) error {
	return agenterrors.Wrap(err, agenterrors.FixableByHuman).WithHint("Could not write the cache; check 'agent-figma cache path' is writable.")
}

// Searchable returns the richest cached store for the file's current
// version, fetching the page/frame outline first if nothing is cached.
func (s *Session) Searchable(ctx context.Context) (*cache.NodeStore, error) {
	version, err := s.CurrentVersion(ctx)
	if err != nil {
		return nil, err
	}
	hasNodes := func(st *cache.NodeStore) bool { return len(st.Nodes) > 0 }
	store, err := s.answerFromCache(version, hasNodes)
	if err != nil || store != nil {
		return store, err
	}
	return s.Document(ctx, 2)
}

// NodesAt makes sure each id's full subtree is cached at an exact version,
// fetching with ?version= if it is not. It never moves the file's head.
func (s *Session) NodesAt(ctx context.Context, ids []string, version string) (*cache.NodeStore, error) {
	store, err := s.File.LoadNodes(version)
	if err == nil && hasAll(store, ids) {
		s.CacheHits++
		return store, nil
	}
	if s.Opts.Offline {
		return nil, offlineMiss()
	}
	res, err := s.fetchNodes(ctx, ids, nil, cache.Full, version)
	if err != nil {
		return nil, err
	}
	return res.Store, nil
}

// PreviousVersion is the newest cached version older than current whose
// store holds every id's full subtree, or "" when none does.
func (s *Session) PreviousVersion(current string, ids []string) string {
	for _, v := range s.File.Versions() {
		if v == current {
			continue
		}
		if st, err := s.File.LoadNodes(v); err == nil && hasAll(st, ids) {
			return v
		}
	}
	return ""
}

func hasAll(store *cache.NodeStore, ids []string) bool {
	for _, id := range ids {
		if !store.Has(id, cache.Full) {
			return false
		}
	}
	return true
}
