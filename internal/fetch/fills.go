package fetch

import (
	"context"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-figma/internal/api"
	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/figma"
)

// fillURLLife is how long fill URLs are trusted: Figma keeps them for at most 14 days.
const fillURLLife = 13 * 24 * time.Hour

type fillCache struct {
	FetchedAt time.Time         `json:"fetched_at"`
	Images    map[string]string `json:"images"`
}

// Fills returns the file's image-fill map (image_ref → URL). The one Tier 2
// call returns every fill in the file, so all of it is cached.
func (s *Session) Fills(ctx context.Context) (map[string]string, error) {
	version, err := s.CurrentVersion(ctx)
	if err != nil {
		return nil, err
	}
	store := s.File.JSONMap(cacheVersion(version), "fills")
	var cached fillCache
	if store.Load(&cached) == nil && cached.Images != nil && s.now().Sub(cached.FetchedAt) < fillURLLife {
		s.CacheHits++
		return cached.Images, nil
	}
	if s.Opts.Offline {
		return nil, offlineMiss()
	}
	raw, err := s.Client.Get(ctx, "/v1/files/"+s.Key()+"/images", nil)
	if err != nil {
		return nil, err
	}
	resp, err := api.Decode[figma.FillsResponse](raw, "image fills")
	if err != nil {
		return nil, err
	}
	images := resp.Meta.Images
	if images == nil {
		images = map[string]string{}
	}
	_ = store.Update(&cached, func() error {
		cached.Images, cached.FetchedAt = images, s.now()
		return nil
	})
	return images, nil
}

// FillUse is an image fill and the nodes that use it.
type FillUse struct {
	ImageRef  string   `json:"image_ref"`
	Nodes     []string `json:"nodes"`
	ScaleMode string   `json:"scale_mode,omitempty"`
}

// FillRefs lists image fills used in the subtrees under ids.
func FillRefs(store *cache.NodeStore, ids []string) []FillUse {
	byRef := map[string]*FillUse{}
	var order []string
	var walk func(id string)
	seen := map[string]bool{}
	walk = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		n, ok := store.Node(id)
		if !ok || !n.IsVisible() {
			return
		}
		for _, p := range n.Fills {
			if p.Type != "IMAGE" || p.ImageRef == "" || !p.IsVisible() {
				continue
			}
			u := byRef[p.ImageRef]
			if u == nil {
				u = &FillUse{ImageRef: p.ImageRef, ScaleMode: strings.ToLower(p.ScaleMode)}
				byRef[p.ImageRef] = u
				order = append(order, p.ImageRef)
			}
			if !slices.Contains(u.Nodes, id) {
				u.Nodes = append(u.Nodes, id)
			}
		}
		if stored := store.Nodes[id]; stored != nil {
			for _, c := range stored.Children {
				walk(c)
			}
		}
	}
	for _, id := range ids {
		walk(id)
	}
	out := make([]FillUse, 0, len(order))
	for _, ref := range order {
		out = append(out, *byRef[ref])
	}
	return out
}

// Fill is one downloaded image fill.
type Fill struct {
	FillUse
	Path       string `json:"path"`
	Bytes      int64  `json:"bytes"`
	MIME       string `json:"mime,omitempty"`
	Source     string `json:"source"`
	Downloaded bool   `json:"downloaded"`
}

// DownloadFills saves the given fills into the cache.
func (s *Session) DownloadFills(ctx context.Context, uses []FillUse, urls map[string]string) ([]Fill, []Unresolved) {
	version := cacheVersion(s.FileInfo.Version)
	if head, ok := s.File.Head(); ok && s.FileInfo.Version == "" {
		version = head.Version
	}
	dir := filepath.Join(s.File.VersionDir(version), "fills")
	var fills []Fill
	var missing []Unresolved
	for _, u := range uses {
		url, ok := urls[u.ImageRef]
		if !ok || url == "" {
			missing = append(missing, Unresolved{ID: u.ImageRef, Reason: "Figma has no URL for image fill " + u.ImageRef, FixableBy: string(agenterrors.FixableByAgent), Hint: "Render the node instead with 'agent-figma render'."})
			continue
		}
		if existing := findFill(dir, u.ImageRef); existing != "" {
			info, _ := os.Stat(existing)
			fills = append(fills, Fill{FillUse: u, Path: existing, Bytes: info.Size(), MIME: mime.TypeByExtension(filepath.Ext(existing)), Source: "fill"})
			continue
		}
		path, n, mimeType, err := s.fetchFill(ctx, url, dir, u.ImageRef)
		if err != nil {
			missing = append(missing, Unresolved{ID: u.ImageRef, Reason: "downloading the fill failed: " + err.Error(), FixableBy: string(agenterrors.FixableByRetry)})
			continue
		}
		fills = append(fills, Fill{FillUse: u, Path: path, Bytes: n, MIME: mimeType, Source: "fill", Downloaded: true})
	}
	return fills, missing
}

func findFill(dir, ref string) string {
	matches, _ := filepath.Glob(filepath.Join(dir, cache.NodeIDSegment(ref)+".*"))
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}

func (s *Session) fetchFill(ctx context.Context, url, dir, ref string) (string, int64, string, error) {
	tmp := filepath.Join(dir, cache.NodeIDSegment(ref)+".download")
	n, err := s.fetchToFile(ctx, url, tmp)
	if err != nil {
		return "", 0, "", err
	}
	head := make([]byte, 512)
	f, err := os.Open(tmp)
	if err != nil {
		return "", 0, "", err
	}
	read, _ := f.Read(head)
	_ = f.Close()
	mimeType := http.DetectContentType(head[:read])
	ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}[mimeType]
	if ext == "" {
		ext = ".bin"
	}
	path := filepath.Join(dir, cache.NodeIDSegment(ref)+ext)
	return path, n, mimeType, os.Rename(tmp, path)
}
