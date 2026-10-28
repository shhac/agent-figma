package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/fetch"
	"github.com/shhac/agent-figma/internal/notes"
	"github.com/shhac/agent-figma/internal/output"
	"github.com/shhac/agent-figma/internal/simplify"
)

const maxHandoffIcons = 100

func registerHandoff(root *cobra.Command, globals *shared.GlobalFlags) {
	var nodes []string
	var out string
	var scale float64
	cmd := &cobra.Command{
		Use:   "handoff <ref>",
		Short: "Everything needed to build one node, in one folder (at most 3 Tier 1 calls)",
		Long: `Writes a folder for building --node: screenshot.png, tree.ndjson (simplified nodes, instances folded,
with @styles and @components), components.json, styles.json, icons/*.svg (every svg:true node, one per
component), images/* (image fills), notes.json (designer notes and comments around it), and
manifest.json. Spends at most three Tier 1 calls (the subtree, the screenshot, the icons) and two Tier 2
calls (image fills, comments), each skipped when cached. The folder defaults to the cache; --out writes
elsewhere.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0], nodes)
			if err != nil {
				return err
			}
			if err := shared.RequireOneNode(ref, "handoff"); err != nil {
				return err
			}
			if err := checkOut(out); err != nil {
				return err
			}
			return shared.WithSession(cmd.Context(), globals, ref, func(ctx context.Context, session *fetch.Session) error {
				m, err := loadMapping(globals)
				if err != nil {
					return err
				}
				files, err := buildHandoff(ctx, session, ref.NodeIDs[0], out, scale, m)
				if err != nil {
					return err
				}
				stream, err := shared.FileStream(globals, session)
				if err != nil {
					return err
				}
				for _, f := range files {
					stream.Item(f)
				}
				return shared.Finish(stream, session)
			})
		},
	}
	cmd.Flags().StringArrayVar(&nodes, "node", nil, "The node to hand off (defaults to the link's node-id)")
	cmd.Flags().StringVar(&out, "out", "", "Write the folder here instead of the cache")
	cmd.Flags().Float64Var(&scale, "scale", 2, "Screenshot scale (0.01-4)")
	root.AddCommand(cmd)
}

type handoffFile struct {
	kind, name, src, rel string
}

func buildHandoff(ctx context.Context, session *fetch.Session, id, out string, scale float64, m mapping) ([]map[string]any, error) {
	store, err := requireNode(ctx, session, id)
	if err != nil {
		return nil, err
	}
	tree := simplify.Simplify(store, []string{id}, simplify.Options{View: simplify.ViewAll, MaxDepth: cache.Full})
	m.annotate(&tree, store)
	node, _ := store.Node(id)
	dir := cmp.Or(out, handoffDir(session, store, id, node.Name))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByHuman).WithHint("Check that the handoff folder is writable")
	}

	var files []handoffFile
	shot, err := session.Render(ctx, fetch.DefaultRender([]string{id}, "png", scale))
	if err != nil {
		return nil, err
	}
	for _, img := range shot.Images {
		files = append(files, handoffFile{kind: "screenshot", name: node.Name, src: img.Path, rel: "screenshot.png"})
	}
	found, cov, err := collectNotes(ctx, session, store, id, notes.DefaultRadius, true)
	if err != nil {
		return nil, err
	}
	icons, err := handoffIcons(ctx, session, tree.Records)
	if err != nil {
		return nil, err
	}
	files = append(files, icons...)
	images, err := handoffImages(ctx, session, store, id)
	if err != nil {
		return nil, err
	}
	files = append(files, images...)

	records := make([]map[string]any, 0, len(files)+4)
	for _, f := range files {
		dest := filepath.Join(dir, f.rel)
		if err := copyFile(f.src, dest); err != nil {
			return nil, agenterrors.Wrap(err, agenterrors.FixableByHuman).WithHint("Check that the handoff folder is writable")
		}
		records = append(records, handoffRecord(f.kind, f.name, dest))
	}
	documents := []struct {
		rel, kind string
		write     func(string) error
	}{
		{"tree.ndjson", "tree", func(p string) error { return writeTree(p, tree) }},
		{"components.json", "components", func(p string) error { return writeJSON(p, tree.Components) }},
		{"styles.json", "styles", func(p string) error { return writeJSON(p, tree.Styles) }},
		{"notes.json", "notes", func(p string) error { return writeJSON(p, map[string]any{"notes": found, "coverage": cov}) }},
	}
	for _, d := range documents {
		dest := filepath.Join(dir, d.rel)
		if err := d.write(dest); err != nil {
			return nil, agenterrors.Wrap(err, agenterrors.FixableByHuman)
		}
		records = append(records, handoffRecord(d.kind, "", dest))
	}
	manifest := filepath.Join(dir, "manifest.json")
	if err := writeJSON(manifest, map[string]any{
		"node":  map[string]any{"id": id, "name": node.Name, "type": node.Type},
		"notes": len(found),
		"file":  session.FileInfo, "files": records, "budget": session.Budget(),
	}); err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByHuman)
	}
	return append(records, handoffRecord("manifest", "", manifest)), nil
}

// handoffIcons renders every svg:true node as SVG in one call, one file per
// main component, so an icon used 300 times is exported once.
func handoffIcons(ctx context.Context, session *fetch.Session, records []simplify.Record) ([]handoffFile, error) {
	ids, names := iconTargets(records)
	if len(ids) == 0 {
		return nil, nil
	}
	req := fetch.DefaultRender(ids, "svg", 1)
	req.Source = "icon"
	res, err := session.Render(ctx, req)
	if err != nil {
		return nil, err
	}
	return iconFiles(res.Images, names), nil
}

// handoffDir is the default handoff folder: in the version's cache, named
// for the node (by its label when its name is generic).
func handoffDir(session *fetch.Session, store *cache.NodeStore, id, name string) string {
	slug := cache.Slug(cmp.Or(simplify.NewLabeler(store).For(id), name))
	return filepath.Join(session.File.VersionDir(store.Version), "handoff", slug+"--"+cache.NodeIDSegment(id))
}

// iconTargets picks the svg records to export, one per main component (an
// icon used 300 times is exported once), up to maxHandoffIcons.
func iconTargets(records []simplify.Record) ([]string, map[string]string) {
	var ids []string
	names := map[string]string{}
	seen := map[string]bool{}
	for _, rec := range records {
		if len(ids) == maxHandoffIcons {
			break
		}
		if !rec.SVG {
			continue
		}
		key, name := rec.ID, rec.Name
		if rec.Component != nil {
			key, name = rec.Component.ID, cmp.Or(rec.Component.Name, rec.Name)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		ids = append(ids, rec.ID)
		names[rec.ID] = cmp.Or(name, "icon")
	}
	return ids, names
}

// iconFiles names rendered icons by component, suffixing the id on clashes.
func iconFiles(images []fetch.Image, names map[string]string) []handoffFile {
	var files []handoffFile
	used := map[string]bool{}
	for _, img := range images {
		file := cache.Slug(names[img.ID])
		if used[file] {
			file += "--" + cache.NodeIDSegment(img.ID)
		}
		used[file] = true
		files = append(files, handoffFile{kind: "icon", name: names[img.ID], src: img.Path, rel: filepath.Join("icons", file+".svg")})
	}
	return files
}

func handoffImages(ctx context.Context, session *fetch.Session, store *cache.NodeStore, id string) ([]handoffFile, error) {
	uses := fetch.FillRefs(store, []string{id})
	if len(uses) == 0 {
		return nil, nil
	}
	urls, err := session.Fills(ctx)
	if err != nil {
		return nil, err
	}
	fills, _ := session.DownloadFills(ctx, uses, urls)
	files := make([]handoffFile, 0, len(fills))
	for _, f := range fills {
		files = append(files, handoffFile{kind: "image", name: f.ImageRef, src: f.Path, rel: filepath.Join("images", filepath.Base(f.Path))})
	}
	return files, nil
}

func handoffRecord(kind, name, path string) map[string]any {
	rec := pruned(map[string]any{"kind": kind, "name": name})
	fileFields(rec, path)
	return rec
}

func writeTree(path string, tree simplify.Result) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	lines := []any{map[string]any{output.MetaStyles: tree.Styles}, map[string]any{output.MetaComponents: tree.Components}}
	for _, r := range tree.Records {
		lines = append(lines, r)
	}
	for _, l := range lines {
		if err := enc.Encode(l); err != nil {
			_ = f.Close()
			return err
		}
	}
	return f.Close()
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}
