package cli

import (
	"context"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/fetch"
	"github.com/shhac/agent-figma/internal/figma"
	"github.com/shhac/agent-figma/internal/output"
)

// ViaMCPEnv is set for commands run by the MCP server, where writing outside
// the cache (--out) is refused: the agent reads files through the cache root.
const ViaMCPEnv = "AGENT_FIGMA_VIA_MCP"

func checkOut(out string) error {
	if out != "" && os.Getenv(ViaMCPEnv) != "" {
		return agenterrors.New("--out is not available through the MCP server", agenterrors.FixableByAgent).
			WithHint("Files are saved in the cache; read them through the MCP fs tool using the manifest path.")
	}
	return nil
}

type renderFlags struct {
	nodes                                     []string
	as                                        string
	scale                                     float64
	out                                       string
	urlOnly, withRelated                      bool
	svgOutlineText, svgIncludeID, svgSimplify bool
	contentsOnly, absoluteBounds              bool
}

func registerRender(root *cobra.Command, globals *shared.GlobalFlags) {
	var f renderFlags
	cmd := &cobra.Command{
		Use:   "render <ref>",
		Short: "Render nodes to png/jpg/svg/pdf and save them (one Tier 1 call per uncached batch)",
		Long: `Renders the nodes, downloads the images into the cache, and prints one manifest line per image
with its absolute path. Sibling variants of a requested variant (and the variants of a requested
component set) are rendered in the same call and listed as @related with cost "download": their URLs
are cached, so fetching them later spends no API call. --with-related downloads them now.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0], f.nodes)
			if err != nil {
				return err
			}
			if err := shared.RequireNodes(ref, "render"); err != nil {
				return err
			}
			if err := checkOut(f.out); err != nil {
				return err
			}
			req := fetch.RenderRequest{
				IDs: ref.NodeIDs, Format: strings.ToLower(f.as), Scale: f.scale,
				SVGOutlineText: f.svgOutlineText, SVGIncludeID: f.svgIncludeID, SVGSimplifyStroke: f.svgSimplify,
				ContentsOnly: f.contentsOnly, UseAbsoluteBounds: f.absoluteBounds,
				WithRelated: f.withRelated, URLOnly: f.urlOnly,
			}
			return shared.WithClient(cmd.Context(), globals, func(ctx context.Context, r *shared.Resolved) error {
				session := shared.NewSession(r, globals, ref)
				res, err := session.Render(ctx, req)
				if err != nil {
					return err
				}
				stream, err := output.NewStream(globals.Format)
				if err != nil {
					return err
				}
				stream.Meta(output.MetaFile, session.FileInfo)
				if err := writeImages(stream, res.Images, f.out, f.urlOnly); err != nil {
					return err
				}
				if f.withRelated {
					if err := writeImages(stream, res.Extras, f.out, f.urlOnly); err != nil {
						return err
					}
				}
				for _, u := range res.Unresolved {
					stream.MetaEach(output.MetaUnresolved, u)
				}
				if !f.withRelated {
					for _, img := range res.Extras {
						stream.MetaEach(output.MetaRelated, fetch.Related{Rel: "render", ID: img.ID, Name: img.Name, Render: renderLabel(img), Cost: fetch.CostDownload})
					}
				}
				return shared.Finish(stream, session)
			})
		},
	}
	fl := cmd.Flags()
	fl.StringArrayVar(&f.nodes, "node", nil, "Node id(s) to render (1:2 or 1-2; repeatable, comma-separated); defaults to the link's node-id")
	fl.StringVar(&f.as, "as", "png", "Image format: png, jpg, svg, or pdf (--format is the output format)")
	fl.Float64Var(&f.scale, "scale", 2, "Scale 0.01-4 for png/jpg (svg and pdf always render at 1)")
	fl.StringVar(&f.out, "out", "", "Also copy images into this directory, under <page>/")
	fl.BoolVar(&f.urlOnly, "url-only", false, "Print Figma's render URLs (valid ~30 days) instead of downloading")
	fl.BoolVar(&f.withRelated, "with-related", false, "Also download the sibling variants rendered alongside")
	fl.BoolVar(&f.svgOutlineText, "svg-outline-text", true, "SVG: render text as outlines")
	fl.BoolVar(&f.svgIncludeID, "svg-include-id", false, "SVG: add layer names as id attributes")
	fl.BoolVar(&f.svgSimplify, "svg-simplify-stroke", true, "SVG: simplify inside/outside strokes")
	fl.BoolVar(&f.contentsOnly, "contents-only", true, "Render only the node, not content overlapping it")
	fl.BoolVar(&f.absoluteBounds, "absolute-bounds", false, "Use the node's full bounds, ignoring cropping")
	root.AddCommand(cmd)
}

func renderLabel(img fetch.Image) string {
	if img.Scale == 1 {
		return img.Format
	}
	return img.Format + "@" + trimFloat(img.Scale) + "x"
}

func trimFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// writeImages prints manifest lines, copying into out when given.
func writeImages(stream *output.Stream, images []fetch.Image, out string, urlOnly bool) error {
	for _, img := range images {
		rec := pruned(map[string]any{"id": img.ID, "name": img.Name, "page": img.Page, "format": img.Format, "scale": img.Scale, "source": img.Source})
		if urlOnly {
			rec["url"] = img.URL
			stream.Item(rec)
			continue
		}
		rec["bytes"], rec["downloaded"] = img.Bytes, img.Downloaded
		fileFields(rec, img.Path)
		if out != "" {
			if err := copyOut(rec, img.Path, filepath.Join(out, strings.TrimPrefix(img.RelPath(), "renders"+string(filepath.Separator)))); err != nil {
				return err
			}
		}
		stream.Item(rec)
	}
	return nil
}

// copyOut copies a cached file to dest and points the record's path there,
// keeping the cache location as cache_path.
func copyOut(rec map[string]any, src, dest string) error {
	if err := copyFile(src, dest); err != nil {
		return agenterrors.Wrap(err, agenterrors.FixableByHuman).WithHint("Check that --out is writable")
	}
	rec["cache_path"], rec["path"] = src, absPath(dest)
	return nil
}

func absPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func copyFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	outFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(outFile, in); err != nil {
		_ = outFile.Close()
		return err
	}
	return outFile.Close()
}

func registerAssets(root *cobra.Command, globals *shared.GlobalFlags) {
	assets := &cobra.Command{Use: "assets", Short: "Download image fills and designer-marked exports"}
	var fillNodes []string
	var fillOut string
	fills := &cobra.Command{
		Use:   "fills <ref>",
		Short: "Download the image fills used under --node (or every fill in the file)",
		Long:  "The fill map comes from one Tier 2 call and covers the whole file; it is cached. With --node, only fills used in those subtrees are downloaded (their nodes are read cache-first).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0], fillNodes)
			if err != nil {
				return err
			}
			if err := checkOut(fillOut); err != nil {
				return err
			}
			return shared.WithClient(cmd.Context(), globals, func(ctx context.Context, r *shared.Resolved) error {
				session := shared.NewSession(r, globals, ref)
				var uses []fetch.FillUse
				var missingNodes []string
				if len(ref.NodeIDs) > 0 {
					res, err := session.Nodes(ctx, ref.NodeIDs, cache.Full)
					if err != nil {
						return err
					}
					uses, missingNodes = fetch.FillRefs(res.Store, ref.NodeIDs), res.Missing
				}
				urls, err := session.Fills(ctx)
				if err != nil {
					return err
				}
				if len(ref.NodeIDs) == 0 {
					for _, imageRef := range sortedKeys(urls) {
						uses = append(uses, fetch.FillUse{ImageRef: imageRef})
					}
				}
				downloaded, missing := session.DownloadFills(ctx, uses, urls)
				stream, err := output.NewStream(globals.Format)
				if err != nil {
					return err
				}
				stream.Meta(output.MetaFile, session.FileInfo)
				for _, f := range downloaded {
					rec := pruned(map[string]any{
						"image_ref": f.ImageRef, "nodes": f.Nodes, "scale_mode": f.ScaleMode, "format": strings.TrimPrefix(filepath.Ext(f.Path), "."),
						"mime": f.MIME, "source": f.Source,
					})
					rec["bytes"], rec["downloaded"] = f.Bytes, f.Downloaded
					fileFields(rec, f.Path)
					if fillOut != "" {
						if err := copyOut(rec, f.Path, filepath.Join(fillOut, "fills", filepath.Base(f.Path))); err != nil {
							return err
						}
					}
					stream.Item(rec)
				}
				for _, id := range missingNodes {
					stream.MetaEach(output.MetaUnresolved, unresolvedNode(id))
				}
				for _, u := range missing {
					stream.MetaEach(output.MetaUnresolved, u)
				}
				return shared.Finish(stream, session)
			})
		},
	}
	fills.Flags().StringArrayVar(&fillNodes, "node", nil, "Only fills used under these node ids")
	fills.Flags().StringVar(&fillOut, "out", "", "Also copy fills into <dir>/fills/")
	assets.AddCommand(fills)

	var exportNodes []string
	var exportOut string
	export := &cobra.Command{
		Use:   "export <ref>",
		Short: "Render every node under --node that designers marked with export settings",
		Long:  "Reads the subtrees cache-first, collects export settings (format, scale or width/height constraint, suffix), and spends one render call per distinct format and scale.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0], exportNodes)
			if err != nil {
				return err
			}
			if err := shared.RequireNodes(ref, "assets export"); err != nil {
				return err
			}
			if err := checkOut(exportOut); err != nil {
				return err
			}
			return shared.WithClient(cmd.Context(), globals, func(ctx context.Context, r *shared.Resolved) error {
				session := shared.NewSession(r, globals, ref)
				res, err := session.Nodes(ctx, ref.NodeIDs, cache.Full)
				if err != nil {
					return err
				}
				stream, err := output.NewStream(globals.Format)
				if err != nil {
					return err
				}
				stream.Meta(output.MetaFile, session.FileInfo)
				for _, group := range exportGroups(res.Store, ref.NodeIDs) {
					rendered, err := session.Render(ctx, fetch.RenderRequest{
						IDs: group.ids, Format: group.format, Scale: group.scale, Suffixes: group.suffixes,
						SVGOutlineText: true, SVGSimplifyStroke: true, ContentsOnly: true, Source: "export",
					})
					if err != nil {
						return err
					}
					if err := writeImages(stream, rendered.Images, exportOut, false); err != nil {
						return err
					}
					for _, u := range rendered.Unresolved {
						stream.MetaEach(output.MetaUnresolved, u)
					}
				}
				for _, id := range res.Missing {
					stream.MetaEach(output.MetaUnresolved, unresolvedNode(id))
				}
				return shared.Finish(stream, session)
			})
		},
	}
	export.Flags().StringArrayVar(&exportNodes, "node", nil, "Subtrees to search for export settings")
	export.Flags().StringVar(&exportOut, "out", "", "Also copy exports into this directory, under <page>/")
	assets.AddCommand(export)
	root.AddCommand(assets)
}

type exportGroup struct {
	format   string
	scale    float64
	ids      []string
	suffixes map[string]string
}

// exportGroups collects export settings under ids, one group per format and
// scale, since each render call takes a single format and scale. A node with
// two settings of the same format and scale lands in two groups so both
// suffixes are kept.
func exportGroups(store *cache.NodeStore, ids []string) []exportGroup {
	var groups []*exportGroup
	find := func(format string, scale float64, id string) *exportGroup {
		for _, g := range groups {
			if g.format == format && g.scale == scale && !slices.Contains(g.ids, id) {
				return g
			}
		}
		g := &exportGroup{format: format, scale: scale, suffixes: map[string]string{}}
		groups = append(groups, g)
		return g
	}
	seen := map[string]bool{}
	var walk func(id string)
	walk = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		n, ok := store.Node(id)
		if !ok || !n.IsVisible() {
			return
		}
		for _, setting := range n.ExportSettings {
			format := strings.ToLower(setting.Format)
			if !slices.Contains(fetch.Formats, format) {
				continue
			}
			scale := exportScale(n, setting)
			g := find(format, scale, id)
			g.ids = append(g.ids, id)
			g.suffixes[id] = setting.Suffix
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
	out := make([]exportGroup, len(groups))
	for i, g := range groups {
		out[i] = *g
	}
	return out
}

// exportScale turns an export constraint into a render scale.
func exportScale(n figma.Node, s figma.ExportSetting) float64 {
	scale := 1.0
	box := n.AbsoluteBoundingBox
	switch s.Constraint.Type {
	case "SCALE":
		scale = s.Constraint.Value
	case "WIDTH":
		if box != nil && box.Width > 0 {
			scale = s.Constraint.Value / box.Width
		}
	case "HEIGHT":
		if box != nil && box.Height > 0 {
			scale = s.Constraint.Value / box.Height
		}
	}
	if scale <= 0 {
		scale = 1
	}
	return math.Min(math.Max(math.Round(scale*1000)/1000, 0.01), 4)
}
