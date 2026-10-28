package figmaurl

import (
	"reflect"
	"testing"
)

const fakeKey = "FAKEFILEKEY0000000000000"
const fakeBranch = "FAKEBRANCHKEY00000000000"

func TestParse(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  Ref
	}{
		{"bare key", fakeKey, Ref{FileKey: fakeKey}},
		{"design link with node", "https://www.figma.com/design/" + fakeKey + "/Example-File?node-id=12-34&p=f", Ref{FileKey: fakeKey, EditorPath: "design", NodeIDs: []string{"12:34"}}},
		{"legacy file link with colon node", "https://figma.com/file/" + fakeKey + "/Example?node-id=12%3A34", Ref{FileKey: fakeKey, EditorPath: "file", NodeIDs: []string{"12:34"}}},
		{"no scheme", "www.figma.com/design/" + fakeKey + "/Example", Ref{FileKey: fakeKey, EditorPath: "design"}},
		{"branch link", "https://www.figma.com/design/" + fakeKey + "/branch/" + fakeBranch + "/Example?node-id=1-2", Ref{FileKey: fakeKey, BranchKey: fakeBranch, EditorPath: "design", NodeIDs: []string{"1:2"}}},
		{"board link", "https://www.figma.com/board/" + fakeKey + "/Example", Ref{FileKey: fakeKey, EditorPath: "board"}},
		{"node list", "https://www.figma.com/design/" + fakeKey + "/X?node-id=1-2,3-4,1-2", Ref{FileKey: fakeKey, EditorPath: "design", NodeIDs: []string{"1:2", "3:4"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.input)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseRejects(t *testing.T) {
	for _, input := range []string{"", "https://example.com/design/" + fakeKey, "https://www.figma.com/files/recent", "not a key!", "https://www.figma.com/design/" + fakeKey + "/X?node-id=abc"} {
		if _, err := Parse(input); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", input)
		}
	}
}

func TestKeyPrefersBranch(t *testing.T) {
	if got := (Ref{FileKey: fakeKey, BranchKey: fakeBranch}).Key(); got != fakeBranch {
		t.Fatalf("Key() = %q", got)
	}
	if got := (Ref{FileKey: fakeKey}).Key(); got != fakeKey {
		t.Fatalf("Key() = %q", got)
	}
}

func TestNormalizeNodeID(t *testing.T) {
	cases := map[string]string{
		"1:2":         "1:2",
		"1-2":         "1:2",
		"I12-34;5-6":  "I12:34;5:6",
		"I12:34;I5:6": "I12:34;I5:6",
		" 100-200 ":   "100:200",
	}
	for in, want := range cases {
		got, err := NormalizeNodeID(in)
		if err != nil || got != want {
			t.Errorf("NormalizeNodeID(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"1", "a:b", "1:2:3", "1-2-3"} {
		if _, err := NormalizeNodeID(bad); err == nil {
			t.Errorf("NormalizeNodeID(%q) succeeded", bad)
		}
	}
}
