package fetch

import (
	"context"
	"time"

	"github.com/shhac/agent-figma/internal/api"
	"github.com/shhac/agent-figma/internal/figma"
)

// Library kinds, matching the endpoint names.
const (
	KindComponents    = "components"
	KindComponentSets = "component_sets"
	KindStyles        = "styles"
)

type libraryCache struct {
	FetchedAt time.Time                             `json:"fetched_at"`
	Lists     map[string][]figma.PublishedComponent `json:"lists"`
}

// Library returns a file's published components, component sets, or styles
// (Tier 3), cached per file version. These endpoints need the main file key.
func (s *Session) Library(ctx context.Context, kind string) ([]figma.PublishedComponent, error) {
	version, err := s.CurrentVersion(ctx)
	if err != nil {
		return nil, err
	}
	store := s.File.JSONMap(version, "library")
	var cached libraryCache
	if version != "" && store.Load(&cached) == nil {
		if list, ok := cached.Lists[kind]; ok {
			s.CacheHits++
			return list, nil
		}
	}
	if s.Opts.Offline {
		return nil, offlineMiss()
	}
	raw, err := s.Client.Get(ctx, "/v1/files/"+s.MainKey()+"/"+kind, nil)
	if err != nil {
		return nil, err
	}
	resp, err := api.Decode[struct {
		Meta map[string][]figma.PublishedComponent `json:"meta"`
	}](raw, kind)
	if err != nil {
		return nil, err
	}
	list := resp.Meta[kind]
	if list == nil {
		list = []figma.PublishedComponent{}
	}
	if version != "" {
		_ = store.Update(&cached, func() error {
			if cached.Lists == nil {
				cached.Lists = map[string][]figma.PublishedComponent{}
			}
			cached.Lists[kind] = list
			cached.FetchedAt = s.now()
			return nil
		})
	}
	return list, nil
}

// cachedVariantSets maps published variant node ids to their component set
// node ids, from the library cache only; it never calls Figma.
func (s *Session) cachedVariantSets(version string) map[string]string {
	var cached libraryCache
	if version == "" || s.File.JSONMap(version, "library").Load(&cached) != nil {
		return nil
	}
	out := map[string]string{}
	for _, c := range cached.Lists[KindComponents] {
		if f := c.ContainingFrame; f != nil && f.ContainingComponentSet != nil {
			out[c.NodeID] = f.ContainingComponentSet.NodeID
		}
	}
	return out
}
