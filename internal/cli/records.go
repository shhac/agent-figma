package cli

import (
	"maps"
	"slices"
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
