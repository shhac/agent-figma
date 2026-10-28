package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/figma"
)

const (
	maxRenderExtras = 24
	// renderURLLife is how long a render URL is trusted: Figma keeps them for
	// 30 days.
	renderURLLife = 29 * 24 * time.Hour
	// maxRenderPixels is Figma's per-render cap; larger extras are skipped.
	maxRenderPixels = 32_000_000
	downloadWorkers = 4
)

var Formats = []string{"png", "jpg", "svg", "pdf"}

// RenderRequest is one render call's parameters.
type RenderRequest struct {
	IDs               []string
	Format            string
	Scale             float64
	SVGOutlineText    bool
	SVGIncludeID      bool
	SVGSimplifyStroke bool
	ContentsOnly      bool
	UseAbsoluteBounds bool
	// Suffixes names files for designer export settings (id → suffix).
	Suffixes map[string]string
	// WithRelated downloads the speculative extras too.
	WithRelated bool
	// URLOnly skips downloading; the result carries Figma's URLs.
	URLOnly bool
	// Source labels the images ("render" or "export").
	Source string
}

func (r RenderRequest) scale() float64 {
	if r.Format == "svg" || r.Format == "pdf" || r.Scale <= 0 {
		return 1
	}
	return r.Scale
}

func (r RenderRequest) source() string {
	if r.Source == "" {
		return "render"
	}
	return r.Source
}

func (r RenderRequest) optionKey() string {
	return fmt.Sprintf("%s@%g|%t%t%t|%t%t", r.Format, r.scale(), r.SVGOutlineText, r.SVGIncludeID, r.SVGSimplifyStroke, r.ContentsOnly, r.UseAbsoluteBounds)
}

// Image is one rendered or downloaded file.
type Image struct {
	ID     string  `json:"id"`
	Name   string  `json:"name,omitempty"`
	Page   string  `json:"page,omitempty"`
	Path   string  `json:"path,omitempty"`
	URL    string  `json:"url,omitempty"`
	Format string  `json:"format"`
	Scale  float64 `json:"scale"`
	Bytes  int64   `json:"bytes,omitempty"`
	Source string  `json:"source"`
	// Downloaded is false when the file was already on disk.
	Downloaded bool `json:"downloaded"`
	rel        string
}

// Unresolved is a requested id that produced no image.
type Unresolved struct {
	ID        string `json:"id"`
	Reason    string `json:"reason"`
	FixableBy string `json:"fixable_by"`
	Hint      string `json:"hint,omitempty"`
}

type RenderResult struct {
	Images     []Image
	Unresolved []Unresolved
	// Extras are speculative renders whose URLs are cached but (without
	// WithRelated) not downloaded.
	Extras []Image
}

type renderEntry struct {
	URL       string    `json:"url,omitempty"`
	Null      bool      `json:"null,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
}

type renderCache struct {
	Entries map[string]renderEntry `json:"entries"`
}

// Render renders ids, downloads the images into the cache, and returns where
// they are. Render URLs are cached per version, so a re-render of the same
// node, format, and scale costs no API call.
func (s *Session) Render(ctx context.Context, req RenderRequest) (*RenderResult, error) {
	if !slices.Contains(Formats, req.Format) {
		return nil, agenterrors.New("image format must be one of "+strings.Join(Formats, ", "), agenterrors.FixableByAgent)
	}
	if req.Scale < 0.01 || req.Scale > 4 {
		return nil, agenterrors.New("--scale must be between 0.01 and 4", agenterrors.FixableByAgent)
	}
	version, err := s.CurrentVersion(ctx)
	if err != nil {
		return nil, err
	}
	known := s.knownStore(version)
	entries := s.loadRenders(version)
	opt := req.optionKey()
	now := s.now()
	usable := func(id string) (renderEntry, bool) {
		e, ok := entries[id+"|"+opt]
		return e, ok && now.Sub(e.FetchedAt) < renderURLLife
	}

	var need []string
	for _, id := range req.IDs {
		if _, ok := usable(id); !ok {
			need = append(need, id)
		}
	}
	var extras []string
	if s.prefetching() && len(need) > 0 {
		extras = s.renderExtras(known, req, need, usable)
	}
	if len(need) > 0 {
		if s.Opts.Offline {
			return nil, offlineMiss()
		}
		got, err := s.requestRenders(ctx, req, need, extras)
		if err != nil {
			return nil, err
		}
		for id, url := range got {
			e := renderEntry{FetchedAt: now}
			if url == nil || *url == "" {
				e.Null = true
			} else {
				e.URL = *url
			}
			entries[id+"|"+opt] = e
		}
		s.saveRenders(version, entries)
	} else {
		s.CacheHits++
	}

	result := &RenderResult{}
	var downloads []*Image
	for _, id := range req.IDs {
		e, ok := usable(id)
		if !ok || e.Null {
			result.Unresolved = append(result.Unresolved, Unresolved{
				ID: id, Reason: "Figma rendered nothing for " + id, FixableBy: string(agenterrors.FixableByAgent),
				Hint: "The node may not exist, be invisible or 0% opacity, or have no renderable content; check it with 'nodes get'.",
			})
			continue
		}
		img := s.describeImage(known, version, id, e.URL, req, req.source())
		result.Images = append(result.Images, img)
	}
	for _, id := range extras {
		e, ok := usable(id)
		if !ok || e.Null {
			continue
		}
		s.Prefetched.Renders++
		result.Extras = append(result.Extras, s.describeImage(known, version, id, e.URL, req, req.source()))
	}
	if req.URLOnly {
		return result, nil
	}
	for i := range result.Images {
		downloads = append(downloads, &result.Images[i])
	}
	if req.WithRelated {
		for i := range result.Extras {
			downloads = append(downloads, &result.Extras[i])
		}
	}
	failed := s.download(ctx, downloads)
	if len(failed) > 0 {
		// An expired or revoked URL: forget it so the next run re-renders.
		for _, id := range failed {
			delete(entries, id+"|"+opt)
		}
		s.saveRenders(version, entries)
		kept := result.Images[:0]
		for _, img := range result.Images {
			if slices.Contains(failed, img.ID) {
				result.Unresolved = append(result.Unresolved, Unresolved{ID: img.ID, Reason: "downloading the render failed", FixableBy: string(agenterrors.FixableByRetry), Hint: "Run the command again; the render will be requested afresh."})
				continue
			}
			kept = append(kept, img)
		}
		result.Images = kept
	}
	return result, nil
}

// renderExtras picks sibling variants (and the variants of a requested
// component set) at the same format and scale, skipping any whose size would
// exceed Figma's render cap.
func (s *Session) renderExtras(known *cache.NodeStore, req RenderRequest, need []string, usable func(string) (renderEntry, bool)) []string {
	seen := map[string]bool{}
	for _, id := range req.IDs {
		seen[id] = true
	}
	var out []string
	consider := func(id string) {
		if seen[id] || len(out) >= maxRenderExtras {
			return
		}
		seen[id] = true
		if _, ok := usable(id); ok {
			return
		}
		n, ok := known.Node(id)
		if !ok || !n.IsVisible() {
			return
		}
		if b := n.AbsoluteBoundingBox; b != nil && b.Width*b.Height*req.scale()*req.scale() > maxRenderPixels {
			return
		}
		out = append(out, id)
	}
	for _, id := range need {
		n, ok := known.Node(id)
		if !ok {
			continue
		}
		stored := known.Nodes[id]
		if n.Type == "COMPONENT_SET" && stored.KnownDepth != 0 {
			for _, c := range stored.Children {
				consider(c)
			}
		}
		if parent, ok := known.Node(stored.Parent); ok && parent.Type == "COMPONENT_SET" {
			if ps := known.Nodes[parent.ID]; ps != nil {
				for _, c := range ps.Children {
					consider(c)
				}
			}
		}
	}
	return out
}

func (s *Session) requestRenders(ctx context.Context, req RenderRequest, need, extras []string) (map[string]*string, error) {
	call := func(ids []string) (map[string]*string, error) {
		q := query("ids", joinIDs(ids), "format", req.Format, "scale", strconv.FormatFloat(req.scale(), 'f', -1, 64))
		if req.Format == "svg" {
			q.Set("svg_outline_text", strconv.FormatBool(req.SVGOutlineText))
			q.Set("svg_include_id", strconv.FormatBool(req.SVGIncludeID))
			q.Set("svg_simplify_stroke", strconv.FormatBool(req.SVGSimplifyStroke))
		}
		if !req.ContentsOnly {
			q.Set("contents_only", "false")
		}
		if req.UseAbsoluteBounds {
			q.Set("use_absolute_bounds", "true")
		}
		raw, err := s.Client.GetNoServerRetry(ctx, "/v1/images/"+s.Key(), q)
		if err != nil {
			return nil, err
		}
		var resp figma.ImagesResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, unexpected(err, "images")
		}
		if resp.Images == nil {
			resp.Images = map[string]*string{}
		}
		for _, id := range ids {
			if _, ok := resp.Images[id]; !ok {
				resp.Images[id] = nil
			}
		}
		return resp.Images, nil
	}
	all := append(append([]string{}, need...), extras...)
	got, err := call(all)
	if err != nil && len(extras) > 0 && retryableWithoutExtras(err) {
		got, err = call(need)
	}
	if err != nil && len(need) > 1 && retryableWithoutExtras(err) {
		half := len(need) / 2
		first, err1 := call(need[:half])
		if err1 != nil {
			return nil, err1
		}
		second, err2 := call(need[half:])
		if err2 != nil {
			return nil, err2
		}
		for k, v := range second {
			first[k] = v
		}
		return first, nil
	}
	return got, err
}

func (s *Session) describeImage(known *cache.NodeStore, version, id, url string, req RenderRequest, source string) Image {
	img := Image{ID: id, URL: url, Format: req.Format, Scale: req.scale(), Source: source}
	suffix := ""
	if req.Suffixes != nil {
		suffix = req.Suffixes[id]
	} else if img.Scale != 1 {
		suffix = "@" + strconv.FormatFloat(img.Scale, 'f', -1, 64) + "x"
	}
	file := cache.NodeIDSegment(id) + suffix + "." + req.Format
	dir := "renders"
	if n, ok := known.Node(id); ok {
		img.Name = n.Name
		file = cache.Slug(n.Name) + "--" + file
		if stored := known.Nodes[id]; stored != nil {
			if page, ok := known.Node(stored.Page); ok {
				img.Page = page.Name
				dir = filepath.Join(dir, cache.Slug(page.Name))
			}
		}
	}
	img.rel = filepath.Join(dir, file)
	img.Path = filepath.Join(s.File.VersionDir(cacheVersion(version)), img.rel)
	return img
}

// RelPath is the image's path relative to its version directory
// (renders/<page>/<file>), for copying elsewhere with the same layout.
func (i Image) RelPath() string { return i.rel }

func cacheVersion(version string) string {
	if version == "" {
		return "unknown"
	}
	return version
}

func (s *Session) loadRenders(version string) map[string]renderEntry {
	var rc renderCache
	_ = s.File.JSONMap(cacheVersion(version), "renders").Load(&rc)
	if rc.Entries == nil {
		rc.Entries = map[string]renderEntry{}
	}
	return rc.Entries
}

func (s *Session) saveRenders(version string, entries map[string]renderEntry) {
	var rc renderCache
	_ = s.File.JSONMap(cacheVersion(version), "renders").Update(&rc, func() error {
		if rc.Entries == nil {
			rc.Entries = map[string]renderEntry{}
		}
		for k, v := range entries {
			rc.Entries[k] = v
		}
		return nil
	})
}

// download fetches images that are not already on disk, in parallel. The
// URLs point at Figma's image CDN, so no token is sent. It returns the ids
// that failed.
func (s *Session) download(ctx context.Context, images []*Image) []string {
	var mu sync.Mutex
	var failed []string
	sem := make(chan struct{}, downloadWorkers)
	var wg sync.WaitGroup
	for _, img := range images {
		if info, err := os.Stat(img.Path); err == nil && info.Size() > 0 {
			img.Bytes = info.Size()
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(img *Image) {
			defer wg.Done()
			defer func() { <-sem }()
			n, err := s.fetchToFile(ctx, img.URL, img.Path)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, img.ID)
				return
			}
			img.Bytes, img.Downloaded = n, true
		}(img)
	}
	wg.Wait()
	return failed
}

func (s *Session) fetchToFile(ctx context.Context, url, path string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := s.Client.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("image download returned HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".download-*")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	n, err := io.Copy(tmp, resp.Body)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, err
	}
	return n, os.Rename(tmp.Name(), path)
}
