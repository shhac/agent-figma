package mockfigma

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const editedVersion = "1000000000000000002"

func postJSON(t *testing.T, ts *httptest.Server, path, body string) response {
	t.Helper()
	resp, err := http.Post(ts.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: raw}
}

func totalText(t *testing.T, ts *httptest.Server, query string) (version, characters any) {
	t.Helper()
	body := getOK(t, ts, filePath+"?ids="+NodeTotalText+query)
	return body["version"], findNode(body["document"], NodeTotalText)["characters"]
}

func TestEditServesEachVersionsContent(t *testing.T) {
	s, ts := newTestServer(t)
	version, err := s.Edit(FileKey, Edit{Op: EditSet, Node: NodeTotalText, Field: "characters", Value: "Total: $50.00"})
	if err != nil || version != editedVersion {
		t.Fatalf("Edit = %q, %v", version, err)
	}

	if v, chars := totalText(t, ts, ""); v != editedVersion || chars != "Total: $50.00" {
		t.Fatalf("newest = %v %v", v, chars)
	}
	if v, chars := totalText(t, ts, "&version="+InitialVersion); v != InitialVersion || chars != "Total: $42.00" {
		t.Fatalf("old version = %v %v", v, chars)
	}
	if got := dig(t, getOK(t, ts, filePath+"/meta"), "file", "version"); got != editedVersion {
		t.Fatalf("meta version = %v", got)
	}
	if got := dig(t, getOK(t, ts, filePath+"/versions"), "versions", 0, "id"); got != editedVersion {
		t.Fatalf("versions should list the edit first, got %v", got)
	}
	if _, chars := totalText(t, ts, "&version=999999999999999997"); chars != "Total: $42.00" {
		t.Fatalf("oldest version changed: %v", chars)
	}

	bumped, _ := s.BumpVersion(FileKey)
	if v, chars := totalText(t, ts, ""); v != bumped || chars != "Total: $50.00" {
		t.Fatalf("a bump should keep the edited content, got %v %v", v, chars)
	}
	branch := getOK(t, ts, "/v1/files/"+BranchKey+"?ids="+NodeTotalText)
	if findNode(branch["document"], NodeTotalText)["characters"] != "Total: $42.00" {
		t.Fatal("editing the main file should not touch the branch")
	}

	s.Reset()
	if v, chars := totalText(t, ts, ""); v != InitialVersion || chars != "Total: $42.00" {
		t.Fatalf("after reset = %v %v", v, chars)
	}
	if resp := get(t, ts, filePath+"?version="+editedVersion); resp.status != http.StatusNotFound {
		t.Fatalf("an edit version should be gone after reset: %d", resp.status)
	}
}

func TestEditNodesWithVersion(t *testing.T) {
	s, ts := newTestServer(t)
	if _, err := s.Edit(FileKey,
		Edit{Op: EditSet, Node: NodeCheckout, Field: "itemSpacing", Value: 24},
		Edit{Op: EditRemove, Node: NodeButtonInstance},
	); err != nil {
		t.Fatal(err)
	}
	nodesPath := filePath + "/nodes?ids=" + NodeCheckout
	newest := getOK(t, ts, nodesPath)
	old := getOK(t, ts, nodesPath+"&version="+InitialVersion)
	if newest["version"] != editedVersion || old["version"] != InitialVersion {
		t.Fatalf("versions = %v %v", newest["version"], old["version"])
	}
	if got := dig(t, newest, "nodes", NodeCheckout, "document", "itemSpacing"); got != float64(24) {
		t.Fatalf("newest itemSpacing = %v", got)
	}
	if got := dig(t, old, "nodes", NodeCheckout, "document", "itemSpacing"); got != float64(16) {
		t.Fatalf("old itemSpacing = %v", got)
	}
	if findNode(dig(t, newest, "nodes", NodeCheckout, "document"), NodeButtonInstance) != nil {
		t.Fatal("removed instance still served")
	}
	if findNode(dig(t, old, "nodes", NodeCheckout, "document"), NodeButtonInstance) == nil {
		t.Fatal("old version lost the instance")
	}
	// The instance of Default was the only thing using it under Checkout.
	if _, ok := dig(t, newest, "nodes", NodeCheckout, "components").(map[string]any)[NodeButtonDefault]; ok {
		t.Fatal("components should derive from the served snapshot")
	}
	if _, ok := dig(t, old, "nodes", NodeCheckout, "components").(map[string]any)[NodeButtonDefault]; !ok {
		t.Fatal("old snapshot should still reference the Default component")
	}
	if resp := get(t, ts, filePath+"/nodes?ids="+NodeButtonInstance); dig(t, resp.json(t), "nodes", NodeButtonInstance) != nil {
		t.Fatal("a removed id should map to null on /nodes")
	}
}

func TestEditControl(t *testing.T) {
	_, ts := newTestServer(t)
	body := `[
		{"op": "set", "node": "` + NodeFrame12 + `", "field": "name", "value": "Promo"},
		{"op": "set", "node": "` + NodeFrame12 + `", "field": "fills", "value": [{"type": "SOLID", "blendMode": "NORMAL", "color": {"r": 1, "g": 0, "b": 0, "a": 1}}]},
		{"op": "remove", "node": "` + NodeHiddenNote + `"},
		{"op": "add", "parent": "` + NodeCheckout + `", "index": 0, "node_json": {"id": "3:1", "type": "FRAME", "name": "Banner",
			"absoluteBoundingBox": {"x": 0, "y": 0, "width": 375, "height": 40},
			"children": [{"id": "3:2", "type": "RECTANGLE", "name": "Bar", "absoluteBoundingBox": {"x": 0, "y": 0, "width": 375, "height": 4}}]}},
		{"op": "move", "node": "` + NodeBadge + `", "parent": "` + NodeCard + `", "index": -1}
	]`
	resp := postJSON(t, ts, "/_mock/edit", body)
	if resp.status != http.StatusOK || resp.json(t)["version"] != editedVersion || resp.json(t)["key"] != FileKey {
		t.Fatalf("edit: %d %s", resp.status, resp.body)
	}

	doc := getOK(t, ts, filePath+"?geometry=paths")["document"]
	checkout := findNode(doc, NodeCheckout)
	ids := childIDs(t, checkout)
	if ids[0] != "3:1" || strings.Contains(strings.Join(ids, ","), NodeHiddenNote) {
		t.Fatalf("checkout children = %v", ids)
	}
	if findNode(doc, NodeFrame12)["name"] != "Promo" || dig(t, findNode(doc, NodeFrame12), "fills", 0, "color", "r") != float64(1) {
		t.Fatalf("frame 12 = %v", findNode(doc, NodeFrame12))
	}
	if card := childIDs(t, findNode(doc, NodeCard)); card[len(card)-1] != NodeBadge {
		t.Fatalf("card children = %v", card)
	}
	if len(childIDs(t, findNode(doc, NodeFrame12))) != 0 {
		t.Fatal("badge should have left Frame 12")
	}
	if findNode(doc, "3:2")["fillGeometry"] == nil {
		t.Fatal("an added vector should get geometry")
	}
	if dig(t, getOK(t, ts, filePath+"/nodes?ids=3:2&version="+editedVersion), "nodes", "3:2", "document", "name") != "Bar" {
		t.Fatal("added node not served by /nodes")
	}

	rejected := map[string]string{
		`[{"op": "set", "node": "99:99", "field": "name", "value": "x"}]`:                                                 "unknown node",
		`[{"op": "remove", "node": "99:99"}]`:                                                                             "unknown node",
		`[{"op": "move", "node": "` + NodeBadge + `", "parent": "99:99"}]`:                                                "unknown parent",
		`[{"op": "move", "node": "` + NodeCard + `", "parent": "` + NodePriceRow + `"}]`:                                  "cycle",
		`[{"op": "add", "parent": "` + NodeTotalText + `", "node_json": {"id": "4:1", "type": "RECTANGLE"}}]`:             "non-container parent",
		`[{"op": "add", "parent": "` + NodeCheckout + `", "node_json": {"id": "` + NodeCard + `", "type": "RECTANGLE"}}]`: "duplicate id",
		`[{"op": "set", "node": "` + NodeCard + `", "field": "id", "value": "x"}]`:                                        "read-only field",
		`[{"op": "rename", "node": "` + NodeCard + `"}]`:                                                                  "unknown op",
		`[{"op": "set", "nodeId": "` + NodeCard + `"}]`:                                                                   "unknown body field",
		`[]`:       "no edits",
		`not json`: "bad body",
	}
	for edit, why := range rejected {
		if resp := postJSON(t, ts, "/_mock/edit", edit); resp.status != http.StatusBadRequest {
			t.Errorf("%s: status %d (%s)", why, resp.status, resp.body)
		}
	}
	if got := getOK(t, ts, filePath+"/meta"); dig(t, got, "file", "version") != editedVersion {
		t.Fatalf("a rejected edit must not add a version, meta = %v", dig(t, got, "file", "version"))
	}
	if resp := postJSON(t, ts, "/_mock/edit?key=NOPE", `[{"op": "remove", "node": "`+NodeCard+`"}]`); resp.status != http.StatusNotFound {
		t.Fatalf("unknown key: %d", resp.status)
	}
	resp = postJSON(t, ts, "/_mock/edit?key="+BranchKey, `[{"op": "remove", "node": "`+NodeCard+`"}]`)
	if resp.status != http.StatusOK || resp.json(t)["version"] != "1000000000000000102" {
		t.Fatalf("branch edit: %d %s", resp.status, resp.body)
	}
}

func TestImagesWithVersionRenderThatVersion(t *testing.T) {
	s, ts := newTestServer(t)
	red := paints(solid(rgba(1, 0, 0, 1)))
	if _, err := s.Edit(FileKey, Edit{Op: EditSet, Node: NodeCheckout, Field: "fills", Value: red}); err != nil {
		t.Fatal(err)
	}
	old := renderPixels(t, ts, NodeCheckout, "&version="+InitialVersion+"&scale=1")
	newest := renderPixels(t, ts, NodeCheckout, "&scale=1")
	if got := rgbAt(old, 1, 1); got != [3]uint8{255, 255, 255} {
		t.Fatalf("old render background = %v", got)
	}
	if got := rgbAt(newest, 1, 1); got != [3]uint8{255, 0, 0} {
		t.Fatalf("newest render background = %v", got)
	}
}

func TestEditValueTakesFixtureShapes(t *testing.T) {
	s, ts := newTestServer(t)
	typed := []map[string]any{{"type": "SOLID", "color": map[string]float64{"r": 0, "g": 0, "b": 1, "a": 1}}}
	if _, err := s.Edit(FileKey, Edit{Op: EditSet, Node: NodeCard, Field: "fills", Value: typed}); err != nil {
		t.Fatal(err)
	}
	if got := rgbAt(renderPixels(t, ts, NodeCard, "&scale=1"), 1, 1); got != [3]uint8{0, 0, 255} {
		t.Fatalf("typed Go fills should render, got %v", got)
	}
	raw, _ := json.Marshal(Edit{Op: EditAdd, Parent: NodeCard, Index: AppendIndex, NodeJSON: json.RawMessage(`{"id":"5:1","type":"TEXT"}`)})
	if !bytes.Contains(raw, []byte(`"node_json":{"id":"5:1"`)) {
		t.Fatalf("Edit JSON = %s", raw)
	}
	if _, err := s.Edit(FileKey, Edit{Op: EditSet, Node: "99:99", Field: "name", Value: "x"}); err == nil || !strings.Contains(err.Error(), "99:99") {
		t.Fatalf("unknown node error = %v", err)
	}
	if _, err := s.Edit("NOPE", Edit{Op: EditRemove, Node: NodeCard}); err == nil {
		t.Fatal("unknown key should fail")
	}
}
