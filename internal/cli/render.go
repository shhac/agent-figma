package cli

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/fetch"
	"github.com/shhac/agent-figma/internal/output"
)

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
