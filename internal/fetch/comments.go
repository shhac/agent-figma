package fetch

import (
	"context"
	"time"

	"github.com/shhac/agent-figma/internal/api"
	"github.com/shhac/agent-figma/internal/figma"
)

type commentCache struct {
	FetchedAt time.Time       `json:"fetched_at"`
	Comments  []figma.Comment `json:"comments"`
}

// Comments returns the file's comments (Tier 2), cached for the freshness
// window: comments change without the file's version moving.
func (s *Session) Comments(ctx context.Context) ([]figma.Comment, error) {
	version, err := s.CurrentVersion(ctx)
	if err != nil {
		return nil, err
	}
	store := s.File.JSONMap(cacheVersion(version), "comments")
	var cached commentCache
	if store.Load(&cached) == nil && cached.Comments != nil && (s.Opts.Offline || (!s.Opts.Refresh && s.now().Sub(cached.FetchedAt) < s.Opts.FreshFor)) {
		s.CacheHits++
		return cached.Comments, nil
	}
	if s.Opts.Offline {
		return nil, offlineMiss()
	}
	raw, err := s.Client.Get(ctx, "/v1/files/"+s.Key()+"/comments", nil)
	if err != nil {
		return nil, err
	}
	resp, err := api.Decode[figma.CommentsResponse](raw, "comments")
	if err != nil {
		return nil, err
	}
	if resp.Comments == nil {
		resp.Comments = []figma.Comment{}
	}
	_ = store.Update(&cached, func() error {
		cached.Comments, cached.FetchedAt = resp.Comments, s.now()
		return nil
	})
	return resp.Comments, nil
}
