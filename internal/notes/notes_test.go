package notes

import (
	"testing"

	"github.com/shhac/agent-figma/internal/figma"
)

func TestKeywordListsAreNotGuidance(t *testing.T) {
	if !isKeywordList("Close, Cancel, X, Delete") || isKeywordList("Use for the primary action on a page, at most once.") || isKeywordList("Entry") {
		t.Fatal("keyword list detection")
	}
}

func TestRelate(t *testing.T) {
	a := figma.Rect{X: 0, Y: 0, Width: 100, Height: 100}
	cases := map[string]figma.Rect{
		"above":       {X: 0, Y: -50, Width: 100, Height: 20},
		"below":       {X: 0, Y: 150, Width: 100, Height: 20},
		"left":        {X: -80, Y: 0, Width: 20, Height: 20},
		"right":       {X: 160, Y: 0, Width: 20, Height: 20},
		"overlapping": {X: 50, Y: 50, Width: 100, Height: 100},
	}
	for want, b := range cases {
		if _, got := relate(a, b); got != want {
			t.Errorf("%v: got %s, want %s", b, got, want)
		}
	}
}
