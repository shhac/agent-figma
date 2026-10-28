package cli

import (
	"io/fs"
	"os"
	"path/filepath"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/output"
)

func registerCache(root *cobra.Command, globals *shared.GlobalFlags) {
	group := &cobra.Command{Use: "cache", Short: "Inspect or clear cached Figma data"}
	group.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the cache directory and this profile's partition",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := shared.Scope(globals)
			return output.WriteItem(map[string]any{"cache_root": cache.Root(), "scope": scope, "scope_path": cache.ProfileDir(scope)}, globals.Format)
		},
	})
	group.AddCommand(&cobra.Command{
		Use:   "status [ref]",
		Short: "List cached files and versions for the selected profile",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := shared.Scope(globals)
			keys, err := cachedKeys(scope, args)
			if err != nil {
				return err
			}
			rows := make([]any, 0, len(keys))
			for _, key := range keys {
				f := cache.OpenFile(scope, key)
				rec := map[string]any{"scope": scope, "file_key": key, "versions": f.Versions(), "bytes": dirSize(f.Dir()), "path": f.Dir()}
				if head, ok := f.Head(); ok {
					rec["name"], rec["current_version"], rec["checked_at"] = head.Name, head.Version, head.CheckedAt
				}
				if versions := f.Versions(); len(versions) > 0 {
					if store, err := f.LoadNodes(versions[0]); err == nil {
						rec["nodes"] = len(store.Nodes)
					}
				}
				rows = append(rows, rec)
			}
			return output.WriteList(rows, nil, globals.Format)
		},
	})
	var all bool
	clear := &cobra.Command{
		Use:   "clear [ref]",
		Short: "Delete one file's cache, or --all for the selected profile",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := shared.Scope(globals)
			switch {
			case all && len(args) == 0:
				if err := cache.PurgeProfile(scope); err != nil {
					return agenterrors.Wrap(err, agenterrors.FixableByHuman)
				}
				return output.WriteItem(map[string]any{"status": "cleared", "scope": scope}, globals.Format)
			case len(args) == 1 && !all:
				ref, err := shared.ParseRef(args[0], nil)
				if err != nil {
					return err
				}
				f := cache.OpenFile(scope, ref.Key())
				if err := os.RemoveAll(f.Dir()); err != nil {
					return agenterrors.Wrap(err, agenterrors.FixableByHuman)
				}
				return output.WriteItem(map[string]any{"status": "cleared", "scope": scope, "file_key": ref.Key()}, globals.Format)
			}
			return agenterrors.New("cache clear takes a <ref> or --all", agenterrors.FixableByAgent)
		},
	}
	clear.Flags().BoolVar(&all, "all", false, "Clear everything cached for the selected profile")
	group.AddCommand(clear)
	root.AddCommand(group)
}

func cachedKeys(scope string, args []string) ([]string, error) {
	if len(args) == 1 {
		ref, err := shared.ParseRef(args[0], nil)
		if err != nil {
			return nil, err
		}
		return []string{ref.Key()}, nil
	}
	entries, err := os.ReadDir(cache.ProfileDir(scope))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByHuman)
	}
	var keys []string
	for _, e := range entries {
		if e.IsDir() {
			keys = append(keys, e.Name())
		}
	}
	return keys, nil
}

func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}
