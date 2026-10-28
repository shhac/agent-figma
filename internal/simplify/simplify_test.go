package simplify

import (
	"encoding/json"
	"testing"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/figma"
)

const doc = `{"id":"0:0","type":"DOCUMENT","name":"Document","children":[
 {"id":"1:1","type":"CANVAS","name":"Page","children":[
  {"id":"1:2","type":"FRAME","name":"Card","layoutMode":"VERTICAL","itemSpacing":8,"paddingTop":16,"paddingBottom":16,"paddingLeft":24,"paddingRight":24,
   "counterAxisAlignItems":"CENTER","absoluteBoundingBox":{"x":100,"y":100,"width":320,"height":200},
   "fills":[{"type":"SOLID","color":{"r":1,"g":1,"b":1,"a":1}}],"styles":{"fill":"S:1"},"cornerRadius":8,
   "children":[
    {"id":"1:3","type":"TEXT","name":"Title text layer","characters":"Hello world","absoluteBoundingBox":{"x":124,"y":116,"width":100,"height":20},
     "style":{"fontFamily":"Example Sans","fontWeight":400,"fontSize":16,"lineHeightPx":24},
     "characterStyleOverrides":[0,0,0,0,0,0,1,1,1,1,1],"styleOverrideTable":{"1":{"fontWeight":700}},
     "fills":[{"type":"SOLID","color":{"r":0,"g":0,"b":0,"a":1}}],"layoutSizingHorizontal":"FILL"},
    {"id":"1:4","type":"FRAME","name":"Frame 12","absoluteBoundingBox":{"x":124,"y":140,"width":50,"height":50},
     "fills":[{"type":"SOLID","color":{"r":1,"g":1,"b":1,"a":1}}],
     "children":[{"id":"1:5","type":"RECTANGLE","name":"Rectangle 1","absoluteBoundingBox":{"x":134,"y":150,"width":10,"height":10},
       "fills":[{"type":"IMAGE","imageRef":"fakeimageref0001","scaleMode":"FILL"}]}]},
    {"id":"1:6","type":"GROUP","name":"Icon","absoluteBoundingBox":{"x":200,"y":140,"width":24,"height":24},
     "children":[{"id":"1:7","type":"VECTOR","name":"Vector 1"},{"id":"1:8","type":"ELLIPSE","name":"Ellipse 2"}]},
    {"id":"1:9","type":"RECTANGLE","name":"Hidden","visible":false}]}]}]}`

func store(t *testing.T) *cache.NodeStore {
	t.Helper()
	s := cache.NewNodeStore("v1")
	s.Styles = map[string]figma.Style{"S:1": {Name: "Surface/White", StyleType: "FILL"}}
	if err := s.MergeFile(&figma.FileResponse{Document: json.RawMessage(doc)}, nil, cache.Full); err != nil {
		t.Fatal(err)
	}
	return s
}

func byID(recs []Record) map[string]Record {
	out := map[string]Record{}
	for _, r := range recs {
		out[r.ID] = r
	}
	return out
}

func TestSimplifyAll(t *testing.T) {
	res := Simplify(store(t), []string{"1:2"}, Options{View: ViewAll, MaxDepth: cache.Full})
	recs := byID(res.Records)
	if _, ok := recs["1:9"]; ok {
		t.Fatal("invisible nodes must be dropped")
	}
	if _, ok := recs["1:7"]; ok {
		t.Fatal("vector-only groups should collapse")
	}
	if !recs["1:6"].SVG || recs["1:6"].Type != "GROUP" || recs["1:6"].Children != 0 {
		t.Fatalf("icon group: %+v", recs["1:6"])
	}
	card := recs["1:2"]
	layout, _ := card.Layout.(map[string]any)
	if layout["mode"] != "column" || layout["gap"] != "8px" || layout["padding"] != "16px 24px" || layout["align"] != "center" {
		t.Fatalf("layout: %+v", card.Layout)
	}
	if card.Children != 3 || card.Radius != 8.0 {
		t.Fatalf("card: %+v", card)
	}
	// The card's white fill is a named style, so it goes to the table; the
	// frame's identical unnamed fill shares the same entry.
	id, ok := card.Fills.(string)
	if !ok || res.Styles[id].Name != "Surface/White" || res.Styles[id].Value != "#FFFFFF" {
		t.Fatalf("fill table: %v %+v", card.Fills, res.Styles)
	}
	if recs["1:4"].Name != "" || len(recs["1:4"].Pos) != 0 {
		// 1:4 sits in an auto-layout parent, so no position; auto name dropped.
		t.Fatalf("auto-named frame: %+v", recs["1:4"])
	}
	if pos := recs["1:5"].Pos; len(pos) != 2 || pos[0] != 10 || pos[1] != 10 {
		t.Fatalf("absolute child position: %+v", recs["1:5"])
	}
	text := recs["1:3"]
	if text.Text != "Hello **world**" || text.Name != "" {
		t.Fatalf("text: %+v", text)
	}
	if img, _ := recs["1:5"].Fills.(map[string]any); img["image_ref"] != "fakeimageref0001" {
		t.Fatalf("image fill: %+v", recs["1:5"].Fills)
	}
}

func TestMaxNodesCutsBreadthFirst(t *testing.T) {
	res := Simplify(store(t), []string{"1:1"}, Options{View: ViewStructure, MaxDepth: cache.Full, MaxNodes: 3})
	if len(res.Records) != 3 {
		t.Fatalf("got %d records", len(res.Records))
	}
	if res.Records[0].ID != "1:1" || res.Records[1].ID != "1:2" {
		t.Fatalf("order: %+v", res.Records)
	}
	// Continuing from the cursor prints only what was left out, and the
	// chain ends.
	printed := map[string]bool{}
	for _, r := range res.Records {
		printed[r.ID] = true
	}
	next := res.Next
	for round := 0; len(next) > 0; round++ {
		if round > 5 {
			t.Fatalf("continuation does not terminate: %v", next)
		}
		more := Simplify(store(t), next, Options{View: ViewStructure, MaxDepth: cache.Full, MaxNodes: 3})
		for _, r := range more.Records {
			if printed[r.ID] {
				t.Fatalf("%s printed twice", r.ID)
			}
			printed[r.ID] = true
		}
		next = more.Next
	}
	// 1:1, 1:2, 1:3, 1:4, 1:5, and the collapsed icon 1:6.
	if len(printed) != 6 {
		t.Fatalf("continuation covered %d of 6 listable nodes", len(printed))
	}
}

func TestMaxDepth(t *testing.T) {
	res := Simplify(store(t), []string{"1:2"}, Options{View: ViewStructure, MaxDepth: 0})
	if len(res.Records) != 1 || res.Records[0].Children != 3 || len(res.Next) != 0 {
		t.Fatalf("depth 0: %+v next=%v", res.Records, res.Next)
	}
}

func TestStructureViewPreviewsText(t *testing.T) {
	res := Simplify(store(t), []string{"1:2"}, Options{View: ViewStructure, MaxDepth: cache.Full})
	if got := byID(res.Records)["1:3"].Text; got != "Hello world" {
		t.Fatalf("text preview = %q", got)
	}
	if preview("first line\nsecond", 80) != "first line…" || preview("abcdef", 4) != "abc…" {
		t.Fatal("preview cuts")
	}
}
