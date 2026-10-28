package cli

import (
	"maps"
	"slices"
	"strings"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-figma/internal/fetch"
	"github.com/shhac/agent-figma/internal/output"
)

// pruned drops empty strings, nil, false, and empty collections, so records
// carry only what is known.
func pruned(rec map[string]any) map[string]any {
	for k, v := range rec {
		switch x := v.(type) {
		case nil:
			delete(rec, k)
		case string:
			if x == "" {
				delete(rec, k)
			}
		case bool:
			if !x {
				delete(rec, k)
			}
		case []string:
			if len(x) == 0 {
				delete(rec, k)
			}
		case map[string]string:
			if len(x) == 0 {
				delete(rec, k)
			}
		case []map[string]string:
			if len(x) == 0 {
				delete(rec, k)
			}
		case map[string]any:
			if len(x) == 0 {
				delete(rec, k)
			}
		}
	}
	return rec
}

func sortedKeys[V any](m map[string]V) []string { return slices.Sorted(maps.Keys(m)) }

// noteCut warns when Figma refused whole subtrees as too large, so what is
// shown stops at a depth limit rather than at the real leaves.
func noteCut(cut []string) {
	if len(cut) == 0 {
		return
	}
	output.Notice("Figma refused the full subtree of "+strings.Join(cut, ", ")+" as too large; showing a depth-limited tree",
		"Records marked truncated have more children; request them with 'nodes get --node <id>'.")
}

func unresolvedNode(id string) fetch.Unresolved {
	return fetch.Unresolved{
		ID:        id,
		Reason:    "Figma has no node " + id + " in this file",
		FixableBy: string(agenterrors.FixableByAgent),
		Hint:      "Check the id with 'agent-figma file outline <ref>'; prototype and FigJam links can use different ids than the design file.",
	}
}
