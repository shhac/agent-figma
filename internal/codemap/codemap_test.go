package codemap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocateWalksUpAndFallsBackToRepoRoot(t *testing.T) {
	t.Setenv(EnvPath, "")
	root := t.TempDir()
	deep := filepath.Join(root, "packages", "app", "src")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if path, exists := Locate("", deep); exists || path != filepath.Join(root, Dir, FileName) {
		t.Fatalf("no mapping yet: propose one at the repo root, got %s", path)
	}
	pkg := filepath.Join(root, "packages", "app")
	if err := os.MkdirAll(filepath.Join(pkg, Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if path, _ := Locate("", deep); path != filepath.Join(pkg, Dir, FileName) {
		t.Fatalf("the nearest .agent-figma wins: %s", path)
	}
}

func TestLookupAndCodeProps(t *testing.T) {
	f := &File{Components: []Entry{
		{Figma: FigmaRef{SetKey: "setkey", Name: "Button"}, Code: Code{Component: "Button"}, Props: map[string]PropMap{
			"State": {Ignore: true},
			"Size":  {Prop: "size", Values: map[string]string{"md": "default"}},
		}},
		{Figma: FigmaRef{Name: "Badge ⭐"}, Code: Code{Component: "Badge"}},
	}}
	e := f.Lookup("setkey", "", "anything")
	if e == nil || e.Code.Component != "Button" {
		t.Fatal("set key lookup")
	}
	props := e.CodeProps(map[string]any{"State": "Hover", "Size": "md", "Tone": "loud"})
	if props["size"] != "default" || props["Tone"] != "loud" || props["State"] != nil {
		t.Fatalf("props: %v", props)
	}
	if f.Lookup("", "", "badge") == nil || f.Lookup("otherset", "", "Button") != nil {
		t.Fatal("name fallback applies only to keyless entries")
	}
}

func TestSuggestFindsExports(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(body), 0o644)
	}
	write("src/ui/button.tsx", "export function Button() {}\nexport const ButtonGroup = () => null\n")
	write("src/ui/index.ts", "export { Input, type InputProps } from './input'\n")
	write("node_modules/lib/button.tsx", "export function Button() {}\n")
	exports := ScanExports(root)
	if len(exports) != 3 {
		t.Fatalf("exports: %+v", exports)
	}
	m, ok := BestMatch("Button ⭐", exports)
	if !ok || m.Confidence != "exact" || m.Export.Import != "src/ui/button" {
		t.Fatalf("match: %+v", m)
	}
	if _, ok := BestMatch("Input", exports); !ok {
		t.Fatal("re-exports count")
	}
}

func TestLoadRefusesNonMappingsAndNewerFormats(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "tsconfig.json")
	_ = os.WriteFile(other, []byte(`{"compilerOptions":{}}`), 0o644)
	if _, err := Load(other); err == nil {
		t.Fatal("a JSON file without components must not load as a mapping")
	}
	newer := filepath.Join(dir, "components.json")
	_ = os.WriteFile(newer, []byte(`{"version":2,"components":[]}`), 0o644)
	if _, err := Load(newer); err == nil {
		t.Fatal("a newer format must not be silently downgraded")
	}
}

func TestSaveRoundTripsAndUpsertMerges(t *testing.T) {
	path := filepath.Join(t.TempDir(), Dir, FileName)
	f, _ := Load(path)
	f.Upsert(Entry{Figma: FigmaRef{SetKey: "s", Name: "Button"}, Code: Code{Component: "Button", Import: "@/ui/button"},
		Props: map[string]PropMap{"Size": {Prop: "size", Values: map[string]string{"md": "default"}}}, Notes: "keep me"})
	if err := Save(path, f); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Upsert(Entry{Figma: FigmaRef{SetKey: "s"}, Code: Code{Component: "Button"}, Props: map[string]PropMap{"State": {Ignore: true}}})
	e := f.Lookup("s", "", "")
	if len(f.Components) != 1 || e.Code.Import != "@/ui/button" || e.Notes != "keep me" || e.Props["Size"].Prop != "size" || !e.Props["State"].Ignore {
		t.Fatalf("re-adding should merge, not replace: %+v", f.Components)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("a checked-in file should be 0644, got %v", info.Mode().Perm())
	}
}
