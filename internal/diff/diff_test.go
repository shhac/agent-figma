package diff

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/figma"
)

func store(t *testing.T, doc string) *cache.NodeStore {
	t.Helper()
	s := cache.NewNodeStore("v")
	if err := s.MergeFile(&figma.FileResponse{Document: json.RawMessage(doc)}, nil, cache.Full); err != nil {
		t.Fatal(err)
	}
	return s
}

const before = `{"id":"0:0","type":"DOCUMENT","name":"Document","children":[{"id":"1:1","type":"CANVAS","name":"Page","children":[
 {"id":"2:1","type":"FRAME","name":"Checkout","children":[
  {"id":"2:2","type":"TEXT","name":"t","characters":"Total: $42.00"},
  {"id":"2:3","type":"RECTANGLE","name":"Old badge"},
  {"id":"2:4","type":"FRAME","name":"Card","children":[{"id":"2:5","type":"TEXT","name":"t","characters":"Price"}]},
  {"id":"2:6","type":"TEXT","name":"t","characters":"Footer"}]}]}]}`

const after = `{"id":"0:0","type":"DOCUMENT","name":"Document","children":[{"id":"1:1","type":"CANVAS","name":"Page","children":[
 {"id":"2:1","type":"FRAME","name":"Checkout","children":[
  {"id":"2:2","type":"TEXT","name":"t","characters":"Total: $50.00"},
  {"id":"2:4","type":"FRAME","name":"Card","children":[{"id":"2:5","type":"TEXT","name":"t","characters":"Price"},{"id":"2:6","type":"TEXT","name":"t","characters":"Footer"}]},
  {"id":"5:1","type":"FRAME","name":"Promo","children":[{"id":"5:2","type":"TEXT","name":"t","characters":"Sale"},{"id":"5:3","type":"TEXT","name":"t","characters":"Now"}]}]}]}]}`

func TestCompare(t *testing.T) {
	changes, sum := Compare(store(t, before), store(t, after), []string{"2:1"})
	byKey := map[string]Change{}
	for _, c := range changes {
		byKey[c.Change+" "+c.ID] = c
	}
	text, ok := byKey["changed 2:2"]
	if !ok || text.Fields["text"].From != "Total: $42.00" || text.Fields["text"].To != "Total: $50.00" || text.Path != "Checkout" {
		t.Fatalf("text change: %+v", changes)
	}
	if c, ok := byKey["added 5:1"]; !ok || c.Descendants != 2 {
		t.Fatalf("added subtree should be one record with descendants: %+v", changes)
	}
	if _, ok := byKey["added 5:2"]; ok {
		t.Fatal("descendants of an added subtree should not be listed")
	}
	if _, ok := byKey["removed 2:3"]; !ok {
		t.Fatalf("removal: %+v", changes)
	}
	if c, ok := byKey["moved 2:6"]; !ok || !strings.Contains(c.Path, "Price") {
		t.Fatalf("move: %+v", changes)
	}
	if sum.Added != 1 || sum.Removed != 1 || sum.Changed != 1 || sum.Moved != 1 {
		t.Fatalf("summary: %+v", sum)
	}
	if len(sum.Frames) == 0 {
		t.Fatal("frames with changes should be listed")
	}
}

func TestIdenticalVersionsHaveNoChanges(t *testing.T) {
	changes, sum := Compare(store(t, before), store(t, before), []string{"2:1"})
	if len(changes) != 0 || len(sum.Frames) != 0 {
		t.Fatalf("no-op diff: %+v", changes)
	}
}
