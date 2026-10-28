package cli

import (
	"context"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"

	// Screenshots may be JPEG as well as PNG.
	_ "image/jpeg"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/fetch"
	"github.com/shhac/agent-figma/internal/figma"
	"github.com/shhac/agent-figma/internal/imagediff"
	"github.com/shhac/agent-figma/internal/simplify"
)

func registerCompare(root *cobra.Command, globals *shared.GlobalFlags) {
	var nodes []string
	var imagePath, out string
	var scale float64
	var threshold int
	cmd := &cobra.Command{
		Use:   "compare <ref>",
		Short: "Where does an implementation screenshot differ from the design? (at most 1-2 Tier 1 calls)",
		Long: `Renders --node at the screenshot's scale (its width over the node's width unless --scale is given),
compares the two pixel by pixel, and reports match_percent and the regions that differ, each attributed
to the deepest design node covering it, plus a diff image (the design faded, differences in red, any
size mismatch in orange). Take the screenshot of just the component, e.g. with Playwright's
locator.screenshot().`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0], nodes)
			if err != nil {
				return err
			}
			if err := shared.RequireOneNode(ref, "compare"); err != nil {
				return err
			}
			if imagePath == "" {
				return agenterrors.New("compare needs --image <screenshot>", agenterrors.FixableByAgent).
					WithHint("Screenshot just the component, e.g. Playwright's locator.screenshot({ path }).")
			}
			if err := checkOut(out); err != nil {
				return err
			}
			actual, err := loadImage(imagePath)
			if err != nil {
				return err
			}
			return shared.WithSession(cmd.Context(), globals, ref, func(ctx context.Context, session *fetch.Session) error {
				rec, err := runCompare(ctx, session, ref.NodeIDs[0], actual, scale, uint8(min(max(threshold, 0), 255)), out)
				if err != nil {
					return err
				}
				stream, err := shared.FileStream(globals, session)
				if err != nil {
					return err
				}
				stream.Item(rec)
				return shared.Finish(stream, session)
			})
		},
	}
	cmd.Flags().StringArrayVar(&nodes, "node", nil, "The design node to compare with (defaults to the link's node-id)")
	cmd.Flags().StringVar(&imagePath, "image", "", "Screenshot of the implementation (PNG or JPEG)")
	cmd.Flags().Float64Var(&scale, "scale", 0, "Render scale (default: screenshot width / node width)")
	cmd.Flags().IntVar(&threshold, "threshold", 24, "Per-channel difference (0-255) a pixel must exceed to count")
	cmd.Flags().StringVar(&out, "out", "", "Write the diff image here instead of the cache")
	root.AddCommand(cmd)
}

func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent).WithHint("Pass a PNG or JPEG screenshot path with --image")
	}
	defer func() { _ = f.Close() }()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent).WithHint("--image must be a PNG or JPEG")
	}
	return img, nil
}

func runCompare(ctx context.Context, session *fetch.Session, id string, actual image.Image, scale float64, threshold uint8, out string) (map[string]any, error) {
	store, err := requireNode(ctx, session, id)
	if err != nil {
		return nil, err
	}
	node, ok := store.Node(id)
	if !ok || node.AbsoluteBoundingBox == nil || node.AbsoluteBoundingBox.Width == 0 {
		return nil, agenterrors.New("node "+id+" has no size to compare against", agenterrors.FixableByAgent).
			WithHint("Compare a frame, component, or instance rather than a page or an empty node.")
	}
	if scale == 0 {
		scale = inferScale(actual.Bounds().Dx(), node)
	}
	scale = min(max(scale, 0.01), 4)
	render, err := session.Render(ctx, fetch.DefaultRender([]string{id}, "png", scale))
	if err != nil {
		return nil, err
	}
	if len(render.Images) == 0 {
		return nil, agenterrors.New("Figma rendered nothing for "+id, agenterrors.FixableByAgent)
	}
	rendered, err := loadImage(render.Images[0].Path)
	if err != nil {
		return nil, err
	}
	design, origin := alignDesign(rendered, actual.Bounds(), node, scale)
	result := imagediff.Compare(design, actual, threshold)
	diffPath := out
	if diffPath == "" {
		diffPath = filepath.Join(session.File.VersionDir(store.Version), "compare", cache.NodeIDSegment(id)+"@"+trimFloat(scale)+"x-diff.png")
	}
	if err := writePNG(diffPath, result.Diff); err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByHuman).WithHint("Check that the diff image path is writable")
	}
	regions := make([]map[string]any, 0, len(result.Regions))
	for _, r := range result.Regions {
		entry := map[string]any{"box": r.Box, "pixels": r.Pixels}
		if hit := nodeAt(store, id, origin, r.Box, scale); hit != nil {
			entry["node"] = hit
		}
		regions = append(regions, entry)
	}
	db := design.Bounds()
	rec := map[string]any{
		"id": id, "name": node.Name, "scale": scale,
		"match_percent":     math.Round(result.MatchPercent*100) / 100,
		"mismatched_pixels": result.Mismatched,
		"design_size":       []int{db.Dx(), db.Dy()}, "image_size": []int{actual.Bounds().Dx(), actual.Bounds().Dy()},
		"regions": regions,
	}
	if db.Dx() != actual.Bounds().Dx() || db.Dy() != actual.Bounds().Dy() {
		rec["size_mismatch"] = true
	}
	diffRec := map[string]any{}
	fileFields(diffRec, diffPath)
	rec["diff_image"] = diffRec
	return rec, nil
}

// nodeAt names the deepest visible design node covering a region's centre,
// so a difference reads as "the price text" rather than as coordinates.
func nodeAt(store *cache.NodeStore, rootID string, origin figma.Vector, box imagediff.Box, scale float64) map[string]any {
	cx, cy := box.Center()
	px := origin.X + float64(cx)/scale
	py := origin.Y + float64(cy)/scale
	best, bestDepth := "", -1
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		n, ok := store.Node(id)
		if !ok || !n.IsVisible() || !contains(n.AbsoluteBoundingBox, px, py) {
			return
		}
		if depth > bestDepth {
			best, bestDepth = id, depth
		}
		if stored := store.Nodes[id]; stored != nil {
			for _, c := range stored.Children {
				walk(c, depth+1)
			}
		}
	}
	walk(rootID, 0)
	if best == "" {
		return nil
	}
	n, _ := store.Node(best)
	return pruned(map[string]any{"id": best, "type": n.Type, "name": n.Name, "text": textPreview(n)})
}

// niceScales are the scales screenshots are usually taken at.
var niceScales = []float64{0.5, 1, 1.5, 2, 2.5, 3, 4}

// inferScale works out the screenshot's scale from its width. Figma renders
// include effects such as shadows (the render bounds) while element
// screenshots usually do not (the bounding box), so both are tried, and the
// candidate closest to a usual scale wins.
func inferScale(width int, n figma.Node) float64 {
	const tolerance = 0.03
	best, bestErr := 0.0, math.Inf(1)
	for _, b := range []*figma.Rect{n.AbsoluteBoundingBox, n.AbsoluteRenderBounds} {
		if b == nil || b.Width <= 0 {
			continue
		}
		raw := float64(width) / b.Width
		for _, nice := range niceScales {
			if rel := math.Abs(raw-nice) / nice; rel < bestErr {
				best, bestErr = nice, rel
			}
		}
	}
	if bestErr <= tolerance {
		return best
	}
	return math.Round(float64(width)/n.AbsoluteBoundingBox.Width*100) / 100
}

// alignDesign crops a render that includes effects down to the node's box
// when the screenshot is the size of the box, and returns where the design
// image's top-left sits in Figma coordinates.
func alignDesign(rendered image.Image, target image.Rectangle, n figma.Node, scale float64) (image.Image, figma.Vector) {
	box, rb := n.AbsoluteBoundingBox, n.AbsoluteRenderBounds
	origin := figma.Vector{X: box.X, Y: box.Y}
	if rb == nil || (rb.Width == box.Width && rb.Height == box.Height) {
		return rendered, origin
	}
	near := func(a, b int) bool { return a-b <= 2 && b-a <= 2 }
	w, h := int(math.Round(box.Width*scale)), int(math.Round(box.Height*scale))
	if !near(target.Dx(), w) || !near(target.Dy(), h) {
		return rendered, figma.Vector{X: rb.X, Y: rb.Y}
	}
	x0, y0 := int(math.Round((box.X-rb.X)*scale)), int(math.Round((box.Y-rb.Y)*scale))
	sub, ok := rendered.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return rendered, figma.Vector{X: rb.X, Y: rb.Y}
	}
	b := rendered.Bounds()
	return sub.SubImage(image.Rect(b.Min.X+x0, b.Min.Y+y0, b.Min.X+x0+w, b.Min.Y+y0+h)), origin
}

func contains(b *figma.Rect, x, y float64) bool {
	return b != nil && x >= b.X && x <= b.X+b.Width && y >= b.Y && y <= b.Y+b.Height
}

func textPreview(n figma.Node) string {
	if n.Type != "TEXT" {
		return ""
	}
	return simplify.Preview(n.Characters)
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
