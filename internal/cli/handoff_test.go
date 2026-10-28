package cli

import (
	"testing"

	"github.com/shhac/agent-figma/internal/codemap"

	"github.com/shhac/agent-figma/internal/fetch"
	"github.com/shhac/agent-figma/internal/simplify"
)

func TestIconTargetsDedupeByComponent(t *testing.T) {
	star := &simplify.ComponentRef{ID: "4:1", Name: "Star"}
	records := []simplify.Record{
		{ID: "1:1", SVG: true, Component: star},
		{ID: "1:2", SVG: true, Component: star},
		{ID: "1:3", SVG: true, Name: "Logo"},
		{ID: "1:4", Name: "Not an icon"},
	}
	ids, names := iconTargets(records)
	if len(ids) != 2 || ids[0] != "1:1" || names["1:1"] != "Star" || names["1:3"] != "Logo" {
		t.Fatalf("targets: %v %v", ids, names)
	}
	files := iconFiles([]fetch.Image{{ID: "1:1"}, {ID: "1:3"}}, map[string]string{"1:1": "Star", "1:3": "Star"})
	if files[0].rel != "icons/star.svg" || files[1].rel != "icons/star--1-3.svg" {
		t.Fatalf("name clash should take the id: %+v", files)
	}
}

func TestProposeMappingsWritesOnlyExactMatches(t *testing.T) {
	uses := []simplify.ComponentUse{
		{Set: "Button ⭐", SetKey: "s1", Count: 3},
		{Component: "Badge", Key: "k2", Count: 1},
		{Component: "Mystery", Key: "k3", Count: 1},
		{Set: "Mapped", SetKey: "s4", Count: 1},
	}
	exports := []codemap.Export{{Name: "Button", Import: "src/ui/button"}, {Name: "StatusBadge", Import: "src/ui/badge"}}
	f := &codemap.File{Components: []codemap.Entry{{Figma: codemap.FigmaRef{SetKey: "s4"}, Code: codemap.Code{Component: "Mapped"}}}}
	recs, written := proposeMappings(uses, exports, f, true)
	if written != 1 || len(recs) != 3 {
		t.Fatalf("written=%d recs=%v", written, recs)
	}
	if recs[0]["match"] != "exact" || recs[0]["written"] != true || recs[1]["match"] != "partial" || recs[2]["match"] != "none" {
		t.Fatalf("matches: %v", recs)
	}
	if e := f.Lookup("s1", "", ""); e == nil || e.Code.Import != "src/ui/button" {
		t.Fatal("the exact match should be added")
	}
	if f.Lookup("", "k2", "") != nil {
		t.Fatal("partial matches are proposed, not written")
	}
}
