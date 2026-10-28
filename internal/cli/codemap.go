package cli

import (
	"cmp"
	"context"
	"os"
	"strings"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/codemap"
	"github.com/shhac/agent-figma/internal/fetch"
	"github.com/shhac/agent-figma/internal/output"
	"github.com/shhac/agent-figma/internal/simplify"
)

// mapping is the repository's component-to-code mapping, when it has one.
type mapping struct {
	path   string
	exists bool
	file   *codemap.File
}

// loadMapping reads the mapping for a map command, where a broken file is
// the problem to report.
func loadMapping(globals *shared.GlobalFlags) (mapping, error) {
	wd, _ := os.Getwd()
	path, exists := codemap.Locate(globals.ComponentsFile, wd)
	f, err := codemap.Load(path)
	if err != nil {
		return mapping{}, agenterrors.Wrap(err, agenterrors.FixableByAgent).WithHint("Fix or move " + path + "; 'agent-figma components map --help' describes the format.")
	}
	return mapping{path: path, exists: exists, file: f}, nil
}

// readMapping reads the mapping for a read command, which should still
// answer when the mapping is broken: it notes the problem and goes unmapped.
func readMapping(globals *shared.GlobalFlags) mapping {
	m, err := loadMapping(globals)
	if err != nil {
		output.Notice("Ignoring the component mapping: "+err.Error(), "Output is unmapped until the file is fixed.")
		return mapping{}
	}
	return m
}

// refuseMappingWrite keeps MCP clients from writing into the server's
// filesystem, as --out is refused there.
func refuseMappingWrite() error {
	if os.Getenv(ViaMCPEnv) == "" {
		return nil
	}
	return agenterrors.New("changing the component mapping is not available through the MCP server", agenterrors.FixableByHuman).
		WithHint("Run 'agent-figma components map add' (or suggest --write) in a terminal in the repository.")
}

// annotate adds the mapped code component to @components entries and
// instance records. Without a mapping file nothing is added, so output stays
// lean until a repository opts in.
func (m mapping) annotate(result *simplify.Result, store *cache.NodeStore) {
	if !m.exists {
		return
	}
	m.annotateUses(result.Components)
	m.annotateRecords(result.Records, store)
}

// annotateUses marks each component use mapped or not, with its code.
func (m mapping) annotateUses(uses []simplify.ComponentUse) {
	for i := range uses {
		u := &uses[i]
		entry := m.file.Lookup(u.SetKey, u.Key, cmp.Or(u.Set, u.Component))
		mapped := entry != nil
		u.Mapped = &mapped
		if !mapped {
			continue
		}
		u.Code = codeRef(entry)
		for j := range u.Variants {
			u.Variants[j].CodeProps = entry.CodeProps(u.Variants[j].Props)
		}
	}
}

// annotateRecords adds the mapped code component to instance records.
func (m mapping) annotateRecords(records []simplify.Record, store *cache.NodeStore) {
	for i := range records {
		rec := &records[i]
		if rec.Component == nil {
			continue
		}
		c := store.Components[rec.Component.ID]
		if entry := m.file.Lookup(store.ComponentSets[c.ComponentSetID].Key, c.Key, cmp.Or(rec.Component.Set, rec.Component.Name)); entry != nil {
			code := codeRef(entry)
			if props := entry.CodeProps(rec.Props); len(props) > 0 {
				code["props"] = props
			}
			rec.Code = code
		}
	}
}

func codeRef(e *codemap.Entry) map[string]any {
	return pruned(map[string]any{"component": e.Code.Component, "import": e.Code.Import, "notes": e.Notes})
}

func registerComponentMap(group *cobra.Command, globals *shared.GlobalFlags) {
	mapCmd := &cobra.Command{
		Use:   "map",
		Short: "The repository's component-to-code mapping (.agent-figma/components.json)",
		Long: `The mapping lives in the repository, found by walking up from the working directory to the nearest
.agent-figma/ (else proposed at the git root), so it can be checked in and shared: it is keyed by
Figma's published component and set keys, which are the same for every user and token. Once it
exists, @components and instance records carry the mapped code component and props.`,
	}
	var statusNodes []string
	status := &cobra.Command{
		Use:   "status <ref>",
		Short: "Which components a subtree (or the cached file) uses, and which are mapped",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0], statusNodes)
			if err != nil {
				return err
			}
			m, err := loadMapping(globals)
			if err != nil {
				return err
			}
			return shared.WithSession(cmd.Context(), globals, ref, func(ctx context.Context, session *fetch.Session) error {
				uses, err := componentUses(ctx, session, ref.NodeIDs)
				if err != nil {
					return err
				}
				m.annotateUses(uses)
				stream, err := shared.FileStream(globals, session)
				if err != nil {
					return err
				}
				stream.Meta("@components_file", map[string]any{"path": m.path, "exists": m.exists, "entries": len(m.file.Components)})
				for _, u := range uses {
					stream.Item(u)
				}
				return shared.Finish(stream, session)
			})
		},
	}
	status.Flags().StringArrayVar(&statusNodes, "node", nil, "Subtree to check (default: every component in the cached file)")
	mapCmd.AddCommand(status)

	var setKey, key, name, component, importPath, notes string
	var propFlags, valueFlags, ignoreFlags []string
	add := &cobra.Command{
		Use:   "add",
		Short: "Map a Figma component (by set key or key) to a code component",
		Example: `  agent-figma components map add --set-key <set key> --name Button --component Button --import @/components/ui/button \
    --prop Size=size --value Size.md=default --ignore State`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := refuseMappingWrite(); err != nil {
				return err
			}
			if component == "" || (setKey == "" && key == "" && name == "") {
				return agenterrors.New("map add needs --component and one of --set-key, --key, or --name", agenterrors.FixableByAgent).
					WithHint("Keys come from 'agent-figma components map status <ref>' (set_key / key on each entry).")
			}
			entry := codemap.Entry{Figma: codemap.FigmaRef{SetKey: setKey, Key: key, Name: name}, Code: codemap.Code{Component: component, Import: importPath}, Notes: notes}
			if err := applyPropFlags(&entry, propFlags, valueFlags, ignoreFlags); err != nil {
				return err
			}
			m, err := loadMapping(globals)
			if err != nil {
				return err
			}
			m.file.Upsert(entry)
			if err := codemap.Save(m.path, m.file); err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByHuman)
			}
			return output.WriteItem(map[string]any{"status": "saved", "path": m.path, "entry": entry}, globals.Format)
		},
	}
	f := add.Flags()
	f.StringVar(&setKey, "set-key", "", "Published key of the component set (for variants)")
	f.StringVar(&key, "key", "", "Published key of a standalone component")
	f.StringVar(&name, "name", "", "Figma component (set) name; the match when neither key is given")
	f.StringVar(&component, "component", "", "Code component name")
	f.StringVar(&importPath, "import", "", "Import path for the code component")
	f.StringVar(&notes, "notes", "", "Anything an implementer should know")
	f.StringArrayVar(&propFlags, "prop", nil, "Figma property to code prop, as Figma=code (repeatable)")
	f.StringArrayVar(&valueFlags, "value", nil, "Figma value to code value, as Property.figma=code (repeatable)")
	f.StringArrayVar(&ignoreFlags, "ignore", nil, "Figma property to drop, e.g. State (repeatable)")
	mapCmd.AddCommand(add)

	var suggestNodes []string
	var src string
	var write bool
	suggest := &cobra.Command{
		Use:   "suggest <ref>",
		Short: "Propose mappings for unmapped components by matching names against exported code components",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0], suggestNodes)
			if err != nil {
				return err
			}
			if write {
				if err := refuseMappingWrite(); err != nil {
					return err
				}
			}
			m, err := loadMapping(globals)
			if err != nil {
				return err
			}
			root := src
			if root == "" {
				root = codemap.Root(m.path)
			}
			return shared.WithSession(cmd.Context(), globals, ref, func(ctx context.Context, session *fetch.Session) error {
				uses, err := componentUses(ctx, session, ref.NodeIDs)
				if err != nil {
					return err
				}
				exports := codemap.ScanExports(root)
				stream, err := shared.FileStream(globals, session)
				if err != nil {
					return err
				}
				written := 0
				for _, u := range uses {
					name := cmp.Or(u.Set, u.Component)
					if m.file.Lookup(u.SetKey, u.Key, name) != nil {
						continue
					}
					match, ok := codemap.BestMatch(name, exports)
					rec := pruned(map[string]any{"set": u.Set, "component": u.Component, "set_key": u.SetKey, "key": u.Key, "count": u.Count})
					if !ok {
						rec["match"] = "none"
						stream.Item(rec)
						continue
					}
					rec["match"], rec["code"] = match.Confidence, match.Export
					if write && match.Confidence == "exact" {
						m.file.Upsert(codemap.Entry{Figma: codemap.FigmaRef{SetKey: u.SetKey, Key: u.Key, Name: name}, Code: codemap.Code{Component: match.Export.Name, Import: match.Export.Import}})
						rec["written"] = true
						written++
					}
					stream.Item(rec)
				}
				if written > 0 {
					if err := codemap.Save(m.path, m.file); err != nil {
						return agenterrors.Wrap(err, agenterrors.FixableByHuman)
					}
				}
				stream.Meta("@components_file", map[string]any{"path": m.path, "written": written, "scanned": root, "exports": len(exports)})
				return shared.Finish(stream, session)
			})
		},
	}
	suggest.Flags().StringArrayVar(&suggestNodes, "node", nil, "Subtree whose components to map (default: every component in the cached file)")
	suggest.Flags().StringVar(&src, "src", "", "Directory to scan for code components (default: the mapping's repository root)")
	suggest.Flags().BoolVar(&write, "write", false, "Add exact matches to the mapping file")
	mapCmd.AddCommand(suggest)
	group.AddCommand(mapCmd)
}

func applyPropFlags(e *codemap.Entry, props, values, ignores []string) error {
	e.Props = map[string]codemap.PropMap{}
	get := func(name string) codemap.PropMap { return e.Props[name] }
	for _, p := range props {
		figma, code, ok := strings.Cut(p, "=")
		if !ok {
			return agenterrors.New("--prop takes Figma=code, got "+p, agenterrors.FixableByAgent)
		}
		pm := get(figma)
		pm.Prop = code
		e.Props[figma] = pm
	}
	for _, v := range values {
		lhs, code, ok := strings.Cut(v, "=")
		prop, figma, ok2 := strings.Cut(lhs, ".")
		if !ok || !ok2 {
			return agenterrors.New("--value takes Property.figma=code, got "+v, agenterrors.FixableByAgent)
		}
		pm := get(prop)
		if pm.Values == nil {
			pm.Values = map[string]string{}
		}
		pm.Values[figma] = code
		e.Props[prop] = pm
	}
	for _, name := range ignores {
		pm := get(name)
		pm.Ignore = true
		e.Props[name] = pm
	}
	if len(e.Props) == 0 {
		e.Props = nil
	}
	return nil
}

// componentUses summarises component usage under ids, or across everything
// cached for the file when no ids are given (no Tier 1 call).
func componentUses(ctx context.Context, session *fetch.Session, ids []string) ([]simplify.ComponentUse, error) {
	if len(ids) > 0 {
		res, err := session.Nodes(ctx, ids, cache.Full)
		if err != nil {
			return nil, err
		}
		return simplify.Simplify(res.Store, ids, simplify.Options{View: simplify.ViewStructure, MaxDepth: cache.Full}).Components, nil
	}
	store, err := session.Searchable(ctx)
	if err != nil {
		return nil, err
	}
	root := store.Nodes[store.Root]
	if root == nil {
		return nil, nil
	}
	return simplify.Simplify(store, root.Children, simplify.Options{View: simplify.ViewStructure, MaxDepth: cache.Full}).Components, nil
}
