package codemap

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var sourceExts = []string{".tsx", ".ts", ".jsx", ".js", ".vue", ".svelte"}

var skippedDirs = map[string]bool{"node_modules": true, ".git": true, "dist": true, "build": true, ".next": true, "out": true, "coverage": true, "vendor": true, ".turbo": true, ".cache": true}

var exportPattern = regexp.MustCompile(`export\s+(?:default\s+)?(?:function|const|class|let)\s+([A-Z][A-Za-z0-9]*)`)

var exportList = regexp.MustCompile(`export\s*\{([^}]*)\}`)

// Export is a component a source file exports.
type Export struct {
	Name   string `json:"component"`
	File   string `json:"file"`
	Import string `json:"import"`
}

// ScanExports lists PascalCase exports (and, for .vue and .svelte, the file
// itself) under root, with import paths relative to root.
func ScanExports(root string) []Export {
	var out []Export
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skippedDirs[d.Name()] && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if !slices.Contains(sourceExts, ext) || strings.HasSuffix(path, ".d.ts") || strings.Contains(path, ".test.") || strings.Contains(path, ".stories.") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		imp := strings.TrimSuffix(filepath.ToSlash(rel), ext)
		if ext == ".vue" || ext == ".svelte" {
			imp = filepath.ToSlash(rel)
			out = append(out, Export{Name: strings.TrimSuffix(filepath.Base(path), ext), File: rel, Import: imp})
			return nil
		}
		for _, name := range exportsOf(path) {
			out = append(out, Export{Name: name, File: rel, Import: imp})
		}
		return nil
	})
	return out
}

func exportsOf(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var names []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		for _, m := range exportPattern.FindAllStringSubmatch(line, -1) {
			names = append(names, m[1])
		}
		for _, m := range exportList.FindAllStringSubmatch(line, -1) {
			for _, part := range strings.Split(m[1], ",") {
				fields := strings.Fields(part)
				if len(fields) == 0 || fields[0] == "type" {
					continue
				}
				name := fields[len(fields)-1]
				if name != "" && name[0] >= 'A' && name[0] <= 'Z' {
					names = append(names, name)
				}
			}
		}
	}
	return names
}

// Match is a proposed mapping for one Figma component.
type Match struct {
	Export     Export `json:"export"`
	Confidence string `json:"confidence"`
}

// BestMatch finds the export whose name matches a Figma component name:
// exactly once normalised, else the shortest export containing it (or
// contained in it).
func BestMatch(figmaName string, exports []Export) (Match, bool) {
	want := Normalize(figmaName)
	if want == "" {
		return Match{}, false
	}
	for _, e := range exports {
		if Normalize(e.Name) == want {
			return Match{Export: e, Confidence: "exact"}, true
		}
	}
	var best *Export
	for i, e := range exports {
		have := Normalize(e.Name)
		if have == "" || !(strings.Contains(have, want) || strings.Contains(want, have)) {
			continue
		}
		if best == nil || len(e.Name) < len(best.Name) {
			best = &exports[i]
		}
	}
	if best == nil {
		return Match{}, false
	}
	return Match{Export: *best, Confidence: "partial"}, true
}
