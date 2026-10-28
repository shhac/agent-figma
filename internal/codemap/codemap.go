// Package codemap is the checked-in mapping from design-system components to
// code components, kept per repository in .agent-figma/components.json.
// Entries are keyed by Figma's published component and component set keys,
// which are the same for every user, file, and token, so one mapping serves
// everyone working in the repository whatever their profiles are called.
package codemap

import (
	"encoding/json"
	"errors"
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
	if p := firstNonEmpty(explicit, os.Getenv(EnvPath)); p != "" {
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
	return filepath.Join(firstNonEmpty(repo, start), Dir, FileName), false
}

// Root is the directory the mapping file describes (the one holding .agent-figma).
func Root(path string) string { return filepath.Dir(filepath.Dir(path)) }

// Load reads a mapping file; a missing file is an empty mapping.
func Load(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &File{Version: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// Save writes the mapping, sorted by name so diffs in review stay small.
func Save(path string, f *File) error {
	f.Version = 1
	slices.SortStableFunc(f.Components, func(a, b Entry) int {
		return strings.Compare(strings.ToLower(a.Figma.Name+a.Code.Component), strings.ToLower(b.Figma.Name+b.Code.Component))
	})
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
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

// Upsert adds an entry, replacing one with the same identity.
func (f *File) Upsert(e Entry) {
	for i, existing := range f.Components {
		if (e.Figma.SetKey != "" && existing.Figma.SetKey == e.Figma.SetKey) ||
			(e.Figma.Key != "" && existing.Figma.Key == e.Figma.Key) ||
			(e.Figma.SetKey == "" && e.Figma.Key == "" && Normalize(existing.Figma.Name) == Normalize(e.Figma.Name)) {
			f.Components[i] = e
			return
		}
	}
	f.Components = append(f.Components, e)
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
