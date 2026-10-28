// Package codemap is the checked-in mapping from design-system components to
// code components, kept per repository in .agent-figma/components.json.
// Entries are keyed by Figma's published component and component set keys,
// which are the same for every user, file, and token, so one mapping serves
// everyone working in the repository whatever their profiles are called.
package codemap

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	Dir      = ".agent-figma"
	FileName = "components.json"
	EnvPath  = "AGENT_FIGMA_COMPONENTS"
)

// File is the mapping document.
type File struct {
	Version    int     `json:"version"`
	Components []Entry `json:"components"`
}

type Entry struct {
	Figma FigmaRef           `json:"figma"`
	Code  Code               `json:"code"`
	Props map[string]PropMap `json:"props,omitempty"`
	Notes string             `json:"notes,omitempty"`
}

// FigmaRef identifies a component: by component set key for variants, by
// component key for a standalone component, by name when unpublished.
type FigmaRef struct {
	SetKey string `json:"set_key,omitempty"`
	Key    string `json:"key,omitempty"`
	Name   string `json:"name,omitempty"`
}

type Code struct {
	Component string `json:"component"`
	Import    string `json:"import,omitempty"`
}

// PropMap maps one Figma variant property onto a code prop.
type PropMap struct {
	Prop   string            `json:"prop,omitempty"`
	Values map[string]string `json:"values,omitempty"`
	Ignore bool              `json:"ignore,omitempty"`
}

// Locate finds the mapping file: the explicit path if given, then the
// environment, then the nearest .agent-figma directory above start. When
// none exists, it proposes one at the repository root (the nearest .git
// above start), or start itself; exists says which.
func Locate(explicit, start string) (path string, exists bool) {
	if p := cmp.Or(explicit, os.Getenv(EnvPath)); p != "" {
		return p, fileExists(p)
	}
	var repo string
	for dir := start; ; dir = filepath.Dir(dir) {
		if info, err := os.Stat(filepath.Join(dir, Dir)); err == nil && info.IsDir() {
			p := filepath.Join(dir, Dir, FileName)
			return p, fileExists(p)
		}
		if repo == "" && fileExists(filepath.Join(dir, ".git")) {
			repo = dir
		}
		if parent := filepath.Dir(dir); parent == dir {
			break
		}
	}
	return filepath.Join(cmp.Or(repo, start), Dir, FileName), false
}

// Root is the directory the mapping file describes (the one holding .agent-figma).
func Root(path string) string { return filepath.Dir(filepath.Dir(path)) }

// SchemaVersion is the mapping format this build reads and writes.
const SchemaVersion = 1

// Load reads a mapping file; a missing file is an empty mapping. A file that
// is not a mapping, or is from a newer format, is an error rather than
// something to overwrite.
func Load(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &File{Version: SchemaVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	if _, ok := probe["components"]; !ok {
		return nil, fmt.Errorf("%s is not a component mapping (no \"components\" list)", path)
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.Version > SchemaVersion {
		return nil, fmt.Errorf("%s uses mapping format %d; this agent-figma reads %d — upgrade it", path, f.Version, SchemaVersion)
	}
	return &f, nil
}

// Save writes the mapping atomically, sorted by name so diffs in review stay
// small. Files are 0644: the mapping is meant to be checked in.
func Save(path string, f *File) error {
	f.Version = SchemaVersion
	slices.SortStableFunc(f.Components, func(a, b Entry) int {
		return strings.Compare(strings.ToLower(a.Figma.Name+a.Code.Component), strings.ToLower(b.Figma.Name+b.Code.Component))
	})
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".components-*.json")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Lookup finds the entry for a component: by set key, then component key,
// then, for entries without keys, by normalised name.
func (f *File) Lookup(setKey, key, name string) *Entry {
	for _, match := range []func(Entry) bool{
		func(e Entry) bool { return setKey != "" && e.Figma.SetKey == setKey },
		func(e Entry) bool { return key != "" && e.Figma.Key == key },
		func(e Entry) bool {
			return e.Figma.SetKey == "" && e.Figma.Key == "" && name != "" && Normalize(e.Figma.Name) == Normalize(name)
		},
	} {
		for i := range f.Components {
			if match(f.Components[i]) {
				return &f.Components[i]
			}
		}
	}
	return nil
}

// Upsert adds an entry, or merges it into the entry with the same identity
// (as Lookup finds it): fields the new entry sets win, the rest are kept, so
// re-adding with fewer flags does not lose earlier detail.
func (f *File) Upsert(e Entry) {
	existing := f.Lookup(e.Figma.SetKey, e.Figma.Key, e.Figma.Name)
	if existing == nil {
		f.Components = append(f.Components, e)
		return
	}
	existing.Figma.SetKey = cmp.Or(e.Figma.SetKey, existing.Figma.SetKey)
	existing.Figma.Key = cmp.Or(e.Figma.Key, existing.Figma.Key)
	existing.Figma.Name = cmp.Or(e.Figma.Name, existing.Figma.Name)
	existing.Code.Component = cmp.Or(e.Code.Component, existing.Code.Component)
	existing.Code.Import = cmp.Or(e.Code.Import, existing.Code.Import)
	existing.Notes = cmp.Or(e.Notes, existing.Notes)
	for name, pm := range e.Props {
		if existing.Props == nil {
			existing.Props = map[string]PropMap{}
		}
		old := existing.Props[name]
		old.Prop = cmp.Or(pm.Prop, old.Prop)
		old.Ignore = old.Ignore || pm.Ignore
		for k, v := range pm.Values {
			if old.Values == nil {
				old.Values = map[string]string{}
			}
			old.Values[k] = v
		}
		existing.Props[name] = old
	}
}

// CodeProps maps a variant's Figma props to the code component's props.
func (e *Entry) CodeProps(props map[string]any) map[string]any {
	if len(props) == 0 {
		return nil
	}
	out := map[string]any{}
	for name, value := range props {
		m, mapped := e.Props[name]
		if m.Ignore {
			continue
		}
		prop := name
		if mapped && m.Prop != "" {
			prop = m.Prop
		}
		if s, ok := value.(string); ok {
			if v, ok := m.Values[s]; ok {
				out[prop] = v
				continue
			}
		}
		out[prop] = value
	}
	return out
}

var notWord = regexp.MustCompile(`[^a-z0-9]+`)

// Normalize reduces a component name to comparable letters and digits:
// "Button ⭐" and "button" are the same component.
func Normalize(name string) string { return notWord.ReplaceAllString(strings.ToLower(name), "") }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
