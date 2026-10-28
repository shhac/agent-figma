package cli

import (
	"testing"

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
