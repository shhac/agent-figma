package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/figma"
)

const (
	maxRenderExtras = 24
	// maxRenderPixels is Figma's per-render cap; larger extras are skipped.
	maxRenderPixels = 32_000_000
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
	failed     bool
}

type RenderResult struct {
	Images     []Image
	Unresolved []Unresolved
	// Extras are speculative renders whose URLs are cached but (without
	// WithRelated) not downloaded.
	Extras []Image
}

// Render renders ids, downloads the images into the cache, and returns where
// they are. Render URLs are cached per version, so a re-render of the same
// node, format, and scale costs no API call.
func (s *Session) Render(ctx context.Context, req RenderRequest) (*RenderResult, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	version, err := s.CurrentVersion(ctx)
	if err != nil {
		return nil, err
	}
	known := s.knownStore(version)
	view := s.renderView(version, req)
	need := view.missing(req.IDs)
	var extras []string
	if s.prefetching() && len(need) > 0 {
		extras = s.renderExtras(known, req, need, view)
	}
	if err := s.refreshRenders(ctx, version, req, &view, need, extras); err != nil {
		return nil, err
	}
	result := s.assembleRenders(known, version, req, view, extras)
	if !req.URLOnly {
		s.downloadRenders(ctx, version, req, &view, result)
	}
	return result, nil
}

func (r RenderRequest) validate() error {
	if !slices.Contains(Formats, r.Format) {
		return agenterrors.New("image format must be one of "+strings.Join(Formats, ", "), agenterrors.FixableByAgent)
	}
	if r.Scale < 0.01 || r.Scale > 4 {
		return agenterrors.New("--scale must be between 0.01 and 4", agenterrors.FixableByAgent)
	}
	return nil
}

// refreshRenders asks Figma for the URLs the cache lacks and records them,
// nulls included, so a node that renders nothing is not asked about again.
func (s *Session) refreshRenders(ctx context.Context, version string, req RenderRequest, view *renderView, need, extras []string) error {
	if len(need) == 0 {
		s.CacheHits++
		return nil
	}
	if s.Opts.Offline {
		return offlineMiss()
	}
	got, err := s.requestRenders(ctx, req, need, extras)
	if err != nil {
		return err
	}
	s.updateRenders(version, view, func(m map[string]renderEntry) {
		for id, url := range got {
			e := renderEntry{FetchedAt: view.now}
			if url == nil || *url == "" {
				e.Null = true
			} else {
				e.URL = *url
			}
			m[view.key(id)] = e
		}
	})
	return nil
}

// assembleRenders describes the requested images and the speculative extras
// whose URLs are now cached.
func (s *Session) assembleRenders(known *cache.NodeStore, version string, req RenderRequest, view renderView, extras []string) *RenderResult {
	result := &RenderResult{}
	for _, id := range req.IDs {
		e, ok := view.usable(id)
		if !ok || e.Null {
			result.Unresolved = append(result.Unresolved, Unresolved{
				ID: id, Reason: "Figma rendered nothing for " + id, FixableBy: string(agenterrors.FixableByAgent),
				Hint: "The node may not exist, be invisible or 0% opacity, or have no renderable content; check it with 'nodes get'.",
			})
			continue
		}
		result.Images = append(result.Images, s.describeImage(known, version, id, e.URL, req))
	}
	for _, id := range extras {
		if e, ok := view.usable(id); ok && !e.Null {
			s.Prefetched.Renders++
			result.Extras = append(result.Extras, s.describeImage(known, version, id, e.URL, req))
		}
	}
	return result
}

// downloadRenders saves the requested images (and extras with WithRelated).
// A failed download usually means an expired URL, so it is forgotten and the
// next run renders afresh.
func (s *Session) downloadRenders(ctx context.Context, version string, req RenderRequest, view *renderView, result *RenderResult) {
	targets := make([]*Image, 0, len(result.Images)+len(result.Extras))
	for i := range result.Images {
		targets = append(targets, &result.Images[i])
	}
	if req.WithRelated {
		for i := range result.Extras {
			targets = append(targets, &result.Extras[i])
		}
	}
	s.download(ctx, targets)
	images, failedImages := keepDownloaded(result.Images)
	extras, failedExtras := keepDownloaded(result.Extras)
	result.Images, result.Extras = images, extras
	for _, id := range failedImages {
		result.Unresolved = append(result.Unresolved, Unresolved{ID: id, Reason: "downloading the render failed", FixableBy: string(agenterrors.FixableByRetry), Hint: "Run the command again; the render will be requested afresh."})
	}
	failed := append(failedImages, failedExtras...)
	if len(failed) == 0 {
		return
	}
	s.updateRenders(version, view, func(m map[string]renderEntry) {
		for _, id := range failed {
			delete(m, view.key(id))
		}
	})
}

// keepDownloaded splits images into those whose download succeeded (or that
// were already on disk) and the ids that failed.
func keepDownloaded(images []Image) ([]Image, []string) {
	kept := images[:0]
	var failed []string
	for _, img := range images {
		if img.failed {
			failed = append(failed, img.ID)
			continue
		}
		kept = append(kept, img)
	}
	return kept, failed
}

// renderExtras picks sibling variants (and the variants of a requested
// component set) at the same format and scale, skipping any whose size would
// exceed Figma's render cap.
func (s *Session) renderExtras(known *cache.NodeStore, req RenderRequest, need []string, view renderView) []string {
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
		if _, ok := view.usable(id); ok {
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

func (s *Session) describeImage(known *cache.NodeStore, version, id, url string, req RenderRequest) Image {
	img := Image{ID: id, URL: url, Format: req.Format, Scale: req.scale(), Source: req.source()}
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
