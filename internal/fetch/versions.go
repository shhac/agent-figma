package fetch

import (
	"context"

	"github.com/shhac/agent-figma/internal/cache"
)

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
