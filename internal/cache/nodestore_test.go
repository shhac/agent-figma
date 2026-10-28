package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/shhac/agent-figma/internal/figma"
)

// A synthetic document: 0:0 → page 1:1 (frame 1:2 → text 1:3), page 2:1.
const doc = `{"id":"0:0","type":"DOCUMENT","name":"Document","children":[
 {"id":"1:1","type":"CANVAS","name":"Page A","children":[
  {"id":"1:2","type":"FRAME","name":"Card","children":[
   {"id":"1:3","type":"TEXT","name":"Title","characters":"Hi"}]}]},
 {"id":"2:1","type":"CANVAS","name":"Page B","children":[]}]}`

func TestMergeFileWithIDsMarksAncestorsPartial(t *testing.T) {
	// What GET /files?ids=1:2 returns: the subtree, plus ancestors and pages
	// with only the children on the path.
	partial := `{"id":"0:0","type":"DOCUMENT","name":"Document","children":[
	 {"id":"1:1","type":"CANVAS","name":"Page A","children":[
	  {"id":"1:2","type":"FRAME","name":"Card","children":[
	   {"id":"1:3","type":"TEXT","name":"Title"}]}]},
	 {"id":"2:1","type":"CANVAS","name":"Page B"}]}`
	s := NewNodeStore("v1")
	if err := s.MergeFile(&figma.FileResponse{Document: json.RawMessage(partial)}, []string{"1:2"}, Full); err != nil {
		t.Fatal(err)
	}
	if !s.Has("1:2", Full) || !s.Has("1:3", Full) {
		t.Fatal("the requested subtree should be complete")
	}
	if s.Has("1:1", 1) || s.Has("0:0", 1) {
		t.Fatal("ancestors must not look complete")
	}
	if s.Nodes["1:3"].Page != "1:1" || s.Nodes["1:2"].Parent != "1:1" {
		t.Fatalf("placement: %+v", s.Nodes["1:3"])
	}
	if got := s.Ancestors("1:3"); len(got) != 3 || got[0] != "1:2" {
		t.Fatalf("ancestors = %v", got)
	}
}

func TestMergeFileDepthFromRoot(t *testing.T) {
	s := NewNodeStore("v1")
	if err := s.MergeFile(&figma.FileResponse{Document: json.RawMessage(doc)}, nil, 2); err != nil {
		t.Fatal(err)
	}
	if !s.Has("0:0", 2) || s.Has("0:0", 3) || !s.Has("1:1", 1) || s.Has("1:2", 1) {
		t.Fatalf("known depths: root=%d page=%d frame=%d", s.Nodes["0:0"].KnownDepth, s.Nodes["1:1"].KnownDepth, s.Nodes["1:2"].KnownDepth)
	}
}

func TestDeeperKnowledgeWins(t *testing.T) {
	s := NewNodeStore("v1")
	_ = s.MergeFile(&figma.FileResponse{Document: json.RawMessage(doc)}, nil, Full)
	// A later shallow outline must not erase the full subtree.
	_ = s.MergeFile(&figma.FileResponse{Document: json.RawMessage(doc)}, nil, 1)
	if !s.Has("1:2", Full) || len(s.Nodes["1:1"].Children) != 1 {
		t.Fatal("a shallower merge overwrote deeper knowledge")
	}
}

func TestMergeNodesKeepsPlacementAndReportsNulls(t *testing.T) {
	s := NewNodeStore("v1")
	_ = s.MergeFile(&figma.FileResponse{Document: json.RawMessage(doc)}, nil, 2)
	resp := &figma.NodesResponse{Nodes: map[string]*figma.NodeEntry{
		"1:2": {Document: json.RawMessage(`{"id":"1:2","type":"FRAME","name":"Card","children":[{"id":"1:3","type":"TEXT","name":"Title"}]}`)},
		"9:9": nil,
	}}
	missing, err := s.MergeNodes(resp, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0] != "9:9" {
		t.Fatalf("missing = %v", missing)
	}
	if s.Nodes["1:2"].Parent != "1:1" || s.Nodes["1:3"].Page != "1:1" || !s.Has("1:2", 1) {
		t.Fatalf("placement lost: %+v %+v", s.Nodes["1:2"], s.Nodes["1:3"])
	}
}

func TestUpdateNodesRoundTripAndPrune(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	f := OpenFile("work", "FAKEFILEKEY0000000000000")
	for _, v := range []string{"1", "2", "3", "4"} {
		if _, err := f.UpdateNodes(v, func(s *NodeStore) error {
			return s.MergeFile(&figma.FileResponse{Document: json.RawMessage(doc)}, nil, Full)
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(f.Versions()); got != KeepVersions {
		t.Fatalf("kept %d versions", got)
	}
	s, err := f.LoadNodes("4")
	if err != nil || !s.Has("0:0", Full) {
		t.Fatalf("reload: %v", err)
	}
}

func TestSlug(t *testing.T) {
	if Slug("Button / Primary — Hover") != "button-primary-hover" || Slug("!!!") != "untitled" {
		t.Fatal("slug")
	}
	if NodeIDSegment("I1:2;3:4") != "I1-2_3-4" {
		t.Fatal(NodeIDSegment("I1:2;3:4"))
	}
}

func TestMergeNodesPropagatesPageIntoNewSubtrees(t *testing.T) {
	s := NewNodeStore("v1")
	// Only the outline is known: 1:2's children are not.
	_ = s.MergeFile(&figma.FileResponse{Document: json.RawMessage(doc)}, nil, 2)
	resp := &figma.NodesResponse{Nodes: map[string]*figma.NodeEntry{
		"1:2": {Document: json.RawMessage(`{"id":"1:2","type":"FRAME","name":"Card","children":[
			{"id":"5:1","type":"FRAME","name":"New","children":[{"id":"5:2","type":"TEXT","name":"Deep"}]}]}`)},
	}}
	if _, err := s.MergeNodes(resp, Full); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"5:1", "5:2"} {
		if got := s.Nodes[id].Page; got != "1:1" {
			t.Fatalf("%s page = %q, want 1:1", id, got)
		}
	}
	if s.Nodes["5:2"].Parent != "5:1" || !s.Has("1:2", Full) {
		t.Fatal("subtree placement or depth wrong")
	}
}

func TestCorruptNodeStoreStartsFresh(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	f := OpenFile("work", "FAKEFILEKEY0000000000000")
	if _, err := f.UpdateNodes("1", func(*NodeStore) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.nodesPath("1"), []byte("not gzip"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := f.UpdateNodes("1", func(s *NodeStore) error {
		return s.MergeFile(&figma.FileResponse{Document: json.RawMessage(doc)}, nil, Full)
	})
	if err != nil || !s.Has("0:0", Full) {
		t.Fatalf("merge over a corrupt store: %v", err)
	}
}

func TestConcurrentUpdatesMerge(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	f := OpenFile("work", "FAKEFILEKEY0000000000000")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("7:%d", i)
			_, _ = f.UpdateNodes("1", func(s *NodeStore) error {
				_, err := s.MergeNodes(&figma.NodesResponse{Nodes: map[string]*figma.NodeEntry{
					id: {Document: json.RawMessage(`{"id":"` + id + `","type":"FRAME","name":"N"}`)},
				}}, Full)
				return err
			})
		}(i)
	}
	wg.Wait()
	s, _ := f.LoadNodes("1")
	if len(s.Nodes) != 8 {
		t.Fatalf("concurrent merges lost writes: %d nodes", len(s.Nodes))
	}
}
