package cache

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shhac/lib-agent-cli/creds"
)

// KeepVersions is how many version directories are kept per file.
const KeepVersions = 3

// File is one file's cache within a profile's partition.
type File struct {
	dir string
}

func OpenFile(scope, key string) File {
	return File{dir: filepath.Join(ProfileDir(scope), safeSegment(key))}
}

func (f File) Dir() string { return f.dir }

// Head records the newest version known for the file and when it was checked.
type Head struct {
	Version   string    `json:"version"`
	CheckedAt time.Time `json:"checked_at"`
	Name      string    `json:"name,omitempty"`
}

func (f File) headStore() creds.Store { return creds.Store{Path: filepath.Join(f.dir, "head.json")} }

func (f File) Head() (Head, bool) {
	var h Head
	if err := f.headStore().Load(&h); err != nil || h.Version == "" {
		return Head{}, false
	}
	return h, true
}

func (f File) SetHead(h Head) error {
	var cur Head
	return f.headStore().Update(&cur, func() error {
		cur = h
		return nil
	})
}

// VersionDir is where one version's data lives.
func (f File) VersionDir(version string) string {
	return filepath.Join(f.dir, "v-"+safeSegment(version))
}

// Versions lists cached versions, newest first by modification time.
func (f File) Versions() []string {
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return nil
	}
	type v struct {
		name string
		mod  time.Time
	}
	var vs []v
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "v-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		vs = append(vs, v{strings.TrimPrefix(e.Name(), "v-"), info.ModTime()})
	}
	sort.Slice(vs, func(i, j int) bool { return vs[i].mod.After(vs[j].mod) })
	out := make([]string, len(vs))
	for i, x := range vs {
		out[i] = x.name
	}
	return out
}

// Prune removes all but the newest KeepVersions version directories.
func (f File) Prune() {
	versions := f.Versions()
	for _, old := range versions[min(len(versions), KeepVersions):] {
		_ = os.RemoveAll(f.VersionDir(old))
	}
}

func (f File) nodesPath(version string) string {
	return filepath.Join(f.VersionDir(version), "nodes.json.gz")
}

// LoadNodes reads a version's node store, empty if nothing is cached.
func (f File) LoadNodes(version string) (*NodeStore, error) {
	s, err := readGzipJSON(f.nodesPath(version))
	if errors.Is(err, fs.ErrNotExist) {
		return NewNodeStore(version), nil
	}
	return s, err
}

// UpdateNodes merges into a version's node store under a lock, so parallel
// processes add to each other's results instead of overwriting them.
func (f File) UpdateNodes(version string, merge func(*NodeStore) error) (*NodeStore, error) {
	path := f.nodesPath(version)
	var result *NodeStore
	err := creds.Store{Path: path}.WithLock(func() error {
		s, err := readGzipJSON(path)
		if errors.Is(err, fs.ErrNotExist) {
			s, err = NewNodeStore(version), nil
		}
		if err != nil {
			s = NewNodeStore(version)
		}
		if err := merge(s); err != nil {
			return err
		}
		result = s
		return writeGzipJSON(path, s)
	})
	if err == nil {
		touch(f.VersionDir(version))
		f.Prune()
	}
	return result, err
}

// JSONMap is a locked JSON document in a version directory (renders, fills,
// library index).
func (f File) JSONMap(version, name string) creds.Store {
	return creds.Store{Path: filepath.Join(f.VersionDir(version), name+".json")}
}

func readGzipJSON(path string) (*NodeStore, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	zr, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	var s NodeStore
	if err := json.NewDecoder(zr).Decode(&s); err != nil {
		return nil, err
	}
	if s.Nodes == nil {
		s.Nodes = map[string]*StoredNode{}
	}
	return &s, nil
}

func writeGzipJSON(path string, s *NodeStore) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".nodes-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	zw := gzip.NewWriter(tmp)
	if err := json.NewEncoder(zw).Encode(s); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func touch(path string) {
	now := time.Now()
	_ = os.Chtimes(path, now, now)
}

var unsafePath = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safeSegment(s string) string {
	s = unsafePath.ReplaceAllString(s, "_")
	if s == "" || s == "." || s == ".." {
		return "_"
	}
	return s
}

var slugRun = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a node or page name into a readable path segment.
func Slug(name string) string {
	s := strings.Trim(slugRun.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 60 {
		s = strings.TrimRight(s[:60], "-")
	}
	if s == "" {
		return "untitled"
	}
	return s
}

// NodeIDSegment makes a node id safe as a file name part ("1:2" → "1-2").
func NodeIDSegment(id string) string {
	return safeSegment(strings.NewReplacer(":", "-", ";", "_").Replace(id))
}
