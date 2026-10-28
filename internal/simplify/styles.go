package simplify

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/figma"
)

// pending is a style value whose fate (inline, or a shared table id) is
// decided once every record has been built and uses are counted.
type pending struct{ key string }

type styleUse struct {
	id    string
	value any
	name  string
	count int
}

type styleTable struct {
	store *cache.NodeStore
	uses  map[string]*styleUse
}

func newStyleTable(store *cache.NodeStore) *styleTable {
	return &styleTable{store: store, uses: map[string]*styleUse{}}
}

// named returns the Figma style name bound to a node for any of keys.
func (t *styleTable) named(n figma.Node, keys ...string) string {
	for _, k := range keys {
		if id, ok := n.Styles[k]; ok {
			if s, ok := t.store.Styles[id]; ok {
				return s.Name
			}
		}
	}
	return ""
}

func (t *styleTable) use(category string, value any, name string) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	sum := sha1.Sum(append([]byte(category+":"), raw...))
	key := category + "_" + hex.EncodeToString(sum[:])[:8]
	u := t.uses[key]
	if u == nil {
		u = &styleUse{id: key, value: value}
		t.uses[key] = u
	}
	if name != "" {
		u.name = name
	}
	u.count++
	return pending{key: key}
}

// finish resolves pending values in records: a value used by 2+ records or
// bound to a named style becomes a table id; the rest are inlined.
func (t *styleTable) finish(records []Record) map[string]StyleEntry {
	table := map[string]StyleEntry{}
	resolve := func(v any) any {
		p, ok := v.(pending)
		if !ok {
			return v
		}
		u := t.uses[p.key]
		if u.count >= 2 || u.name != "" {
			table[u.id] = StyleEntry{Name: u.name, Value: u.value}
			return u.id
		}
		return u.value
	}
	for i := range records {
		r := &records[i]
		r.Layout = resolve(r.Layout)
		r.Fills = resolve(r.Fills)
		r.Strokes = resolve(r.Strokes)
		r.Effects = resolve(r.Effects)
		r.TextStyle = resolve(r.TextStyle)
	}
	return table
}
