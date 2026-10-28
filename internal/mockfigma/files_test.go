package mockfigma

import (
	"net/http"
	"slices"
	"testing"
)

const filePath = "/v1/files/" + FileKey

func TestFileFullDocument(t *testing.T) {
	_, ts := newTestServer(t)
	body := getOK(t, ts, filePath)

	if body["name"] != FileName || body["version"] != InitialVersion || body["editorType"] != "figma" {
		t.Fatalf("header = %v %v %v", body["name"], body["version"], body["editorType"])
	}
	if body["schemaVersion"] != float64(0) {
		t.Fatalf("schemaVersion = %v", body["schemaVersion"])
	}
	doc := body["document"]
	if got := childIDs(t, doc); !equalStrings(got, []string{NodeCoverPage, NodeComponentsPage, NodeScreensPage}) {
		t.Fatalf("pages = %v", got)
	}
	if got := keys(body["components"]); !equalStrings(got, []string{NodeButtonDefault, NodeButtonHover, NodeButtonDisabled, NodeIconStar}) {
		t.Fatalf("components = %v", got)
	}
	if dig(t, body, "components", NodeButtonDefault, "componentSetId") != NodeButtonSet {
		t.Fatal("variant component should name its set")
	}
	if got := keys(body["componentSets"]); !equalStrings(got, []string{NodeButtonSet}) {
		t.Fatalf("componentSets = %v", got)
	}
	if got := keys(body["styles"]); !equalStrings(got, []string{StyleBrandPrimary, StyleHeadingH1, StyleBodyLabel, StyleCardShadow}) {
		t.Fatalf("styles = %v", got)
	}
	if dig(t, body, "styles", StyleBrandPrimary, "styleType") != "FILL" {
		t.Fatal("Brand/Primary should be a FILL style")
	}
}

func TestFileNodeShapes(t *testing.T) {
	_, ts := newTestServer(t)
	doc := getOK(t, ts, filePath)["document"]

	set := findNode(doc, NodeButtonSet)
	if dig(t, set, "componentPropertyDefinitions", "State", "type") != "VARIANT" {
		t.Fatalf("set definitions = %v", set["componentPropertyDefinitions"])
	}
	variant := findNode(doc, NodeButtonDefault)
	for field, want := range map[string]any{
		"layoutMode": "HORIZONTAL", "itemSpacing": float64(8), "paddingLeft": float64(16),
		"primaryAxisAlignItems": "CENTER", "counterAxisAlignItems": "CENTER", "cornerRadius": float64(8),
	} {
		if variant[field] != want {
			t.Errorf("variant %s = %v, want %v", field, variant[field], want)
		}
	}
	if dig(t, variant, "styles", "fill") != StyleBrandPrimary || dig(t, variant, "fills", 0, "type") != "SOLID" {
		t.Errorf("variant fill = %v / %v", variant["styles"], variant["fills"])
	}
	if dig(t, variant, "children", 0, "type") != "TEXT" || dig(t, variant, "children", 0, "style", "fontFamily") != "Inter" {
		t.Error("variant should hold a styled TEXT label")
	}

	instance := findNode(doc, NodeButtonInstance)
	if instance["componentId"] != NodeButtonDefault || dig(t, instance, "componentProperties", "State", "value") != "Default" {
		t.Errorf("instance = %v / %v", instance["componentId"], instance["componentProperties"])
	}
	if dig(t, instance, "children", 0, "id") != NodeInstanceLabel {
		t.Errorf("instance child id = %v", dig(t, instance, "children", 0, "id"))
	}
	if findNode(doc, NodeHoverInstance)["componentId"] != NodeButtonHover {
		t.Error("second instance should use the Hover variant")
	}

	total := findNode(doc, NodeTotalText)
	if total["characters"] != "Total: $42.00" || dig(t, total, "styleOverrideTable", "1", "fontWeight") != float64(700) {
		t.Errorf("text overrides = %v", total["styleOverrideTable"])
	}
	if overrides, _ := total["characterStyleOverrides"].([]any); len(overrides) != len("Total: $42.00") {
		t.Errorf("characterStyleOverrides = %v", overrides)
	}

	hero := findNode(doc, NodeHeroImage)
	if dig(t, hero, "fills", 0, "imageRef") != ImageRefHero || dig(t, hero, "fills", 0, "scaleMode") != "FILL" {
		t.Errorf("hero fills = %v", hero["fills"])
	}
	if findNode(doc, NodeHiddenNote)["visible"] != false {
		t.Error("hidden note should be visible:false")
	}
	if _, ok := findNode(doc, NodeFrame12)["layoutMode"]; ok {
		t.Error("Frame 12 should not be auto layout")
	}
	if findNode(doc, NodeStatusDot)["layoutPositioning"] != "ABSOLUTE" {
		t.Error("status dot should be absolutely positioned")
	}
	export := findNode(doc, NodeExportFrame)
	if dig(t, export, "exportSettings", 0, "suffix") != "@2x" || dig(t, export, "exportSettings", 1, "format") != "SVG" ||
		dig(t, export, "exportSettings", 0, "constraint", "value") != float64(2) {
		t.Errorf("exportSettings = %v", export["exportSettings"])
	}
	icon := findNode(doc, NodeIconStar)
	for _, kid := range icon["children"].([]any) {
		if typ := dig(t, kid, "type"); typ != "VECTOR" && typ != "ELLIPSE" {
			t.Errorf("icon child type = %v", typ)
		}
	}
}

func TestFileIDsReturnAncestorChainAndAllPages(t *testing.T) {
	_, ts := newTestServer(t)
	body := getOK(t, ts, filePath+"?ids="+NodePriceRow)
	doc := body["document"]

	if got := childIDs(t, doc); !equalStrings(got, []string{NodeCoverPage, NodeComponentsPage, NodeScreensPage}) {
		t.Fatalf("pages = %v", got)
	}
	if got := childIDs(t, dig(t, doc, "children", 0)); len(got) != 0 {
		t.Fatalf("off-path page children = %v", got)
	}
	chain := []string{NodeScreensPage, NodeCheckout, NodeCard, NodeCardBody, NodePriceRow}
	n := dig(t, doc, "children", 2)
	for i, id := range chain {
		if dig(t, n, "id") != id {
			t.Fatalf("chain[%d] = %v, want %s", i, dig(t, n, "id"), id)
		}
		if i == len(chain)-1 {
			break
		}
		kids := childIDs(t, n)
		if len(kids) != 1 || kids[0] != chain[i+1] {
			t.Fatalf("%s children = %v, want only %s", id, kids, chain[i+1])
		}
		n = dig(t, n, "children", 0)
	}
	if got := childIDs(t, n); !equalStrings(got, []string{NodePriceText, NodeStatusDot}) {
		t.Fatalf("listed node should carry its full subtree, got %v", got)
	}
	if got := keys(body["components"]); len(got) != 0 {
		t.Fatalf("components = %v", got)
	}
}

func TestFileIDsIncludeLocalComponentDependencies(t *testing.T) {
	_, ts := newTestServer(t)
	body := getOK(t, ts, filePath+"?ids="+NodeButtonInstance)
	doc := body["document"]

	components := dig(t, doc, "children", 1)
	if got := childIDs(t, components); !equalStrings(got, []string{NodeButtonSet}) {
		t.Fatalf("Components page children = %v", got)
	}
	set := dig(t, components, "children", 0)
	if got := childIDs(t, set); !equalStrings(got, []string{NodeButtonDefault}) {
		t.Fatalf("set should carry only the instanced variant, got %v", got)
	}
	if got := childIDs(t, dig(t, set, "children", 0)); len(got) != 1 {
		t.Fatalf("main component subtree = %v", got)
	}
	if got := childIDs(t, findNode(doc, NodeCheckout)); !equalStrings(got, []string{NodeButtonInstance}) {
		t.Fatalf("Checkout children = %v", got)
	}
	if got := keys(body["components"]); !equalStrings(got, []string{NodeButtonDefault}) {
		t.Fatalf("components = %v", got)
	}
	if got := keys(body["componentSets"]); !equalStrings(got, []string{NodeButtonSet}) {
		t.Fatalf("componentSets = %v", got)
	}
}

func TestFileIDsIgnoreUnknownIDs(t *testing.T) {
	_, ts := newTestServer(t)
	doc := getOK(t, ts, filePath+"?ids=99:99")["document"]
	if countNodes(doc) != 4 {
		t.Fatalf("expected document plus three empty pages, got %d nodes", countNodes(doc))
	}
}

func TestFileDepthCountsFromRoot(t *testing.T) {
	_, ts := newTestServer(t)

	doc := getOK(t, ts, filePath+"?depth=1")["document"]
	for _, page := range doc.(map[string]any)["children"].([]any) {
		if hasChildrenKey(page) {
			t.Fatalf("depth=1: page %v should have no children key", dig(t, page, "id"))
		}
	}

	doc = getOK(t, ts, filePath+"?depth=2")["document"]
	screens := dig(t, doc, "children", 2)
	if got := childIDs(t, screens); !equalStrings(got, []string{NodeCheckout}) {
		t.Fatalf("depth=2 Screens children = %v", got)
	}
	if hasChildrenKey(dig(t, screens, "children", 0)) {
		t.Fatal("depth=2: top-level frame should have no children key")
	}

	doc = getOK(t, ts, filePath+"?ids="+NodePriceRow+"&depth=3")["document"]
	card := findNode(doc, NodeCard)
	if card == nil || hasChildrenKey(card) {
		t.Fatalf("ids+depth=3: Card (level 3) should be present without children, got %v", card)
	}
}

func TestFileRejectsBadDepth(t *testing.T) {
	_, ts := newTestServer(t)
	for _, depth := range []string{"0", "-1", "two"} {
		if resp := get(t, ts, filePath+"?depth="+depth); resp.status != http.StatusBadRequest {
			t.Errorf("depth=%s: status %d", depth, resp.status)
		}
	}
}

func TestNodesDepthIsRelativeToEachNode(t *testing.T) {
	_, ts := newTestServer(t)
	body := getOK(t, ts, filePath+"/nodes?ids="+NodeCheckout+","+NodeCard+",99:99&depth=1")
	nodes := body["nodes"].(map[string]any)

	if v, ok := nodes["99:99"]; !ok || v != nil {
		t.Fatalf("unknown id should map to null, got %v (present %v)", v, ok)
	}
	checkout := dig(t, nodes, NodeCheckout, "document")
	if dig(t, checkout, "id") != NodeCheckout || len(childIDs(t, checkout)) != 9 {
		t.Fatalf("Checkout children = %v", childIDs(t, checkout))
	}
	for _, kid := range checkout.(map[string]any)["children"].([]any) {
		if hasChildrenKey(kid) {
			t.Fatalf("depth=1: child %v should have no children key", dig(t, kid, "id"))
		}
	}
	if got := childIDs(t, dig(t, nodes, NodeCard, "document")); !equalStrings(got, []string{NodeCardBody}) {
		t.Fatalf("Card children = %v", got)
	}

	deep := getOK(t, ts, filePath+"/nodes?ids="+NodeCheckout+"&depth=4")
	if findNode(dig(t, deep, "nodes", NodeCheckout, "document"), NodePriceText) == nil {
		t.Fatal("depth=4 should reach the price text four levels down")
	}
	shallow := getOK(t, ts, filePath+"/nodes?ids="+NodeCheckout+"&depth=3")
	row := findNode(dig(t, shallow, "nodes", NodeCheckout, "document"), NodePriceRow)
	if row == nil || hasChildrenKey(row) {
		t.Fatalf("depth=3: price row should be the cut, got %v", row)
	}
}

func TestNodesCarryPerNodeDependencies(t *testing.T) {
	_, ts := newTestServer(t)
	body := getOK(t, ts, filePath+"/nodes?ids="+NodeCheckout+","+NodeIconStar)

	entry := dig(t, body, "nodes", NodeCheckout)
	if got := keys(dig(t, entry, "components")); !equalStrings(got, []string{NodeButtonDefault, NodeButtonHover}) {
		t.Fatalf("Checkout components = %v", got)
	}
	if got := keys(dig(t, entry, "componentSets")); !equalStrings(got, []string{NodeButtonSet}) {
		t.Fatalf("Checkout componentSets = %v", got)
	}
	if got := keys(dig(t, entry, "styles")); !equalStrings(got, []string{StyleBrandPrimary, StyleBodyLabel, StyleCardShadow}) {
		t.Fatalf("Checkout styles = %v", got)
	}
	if got := keys(dig(t, body, "nodes", NodeIconStar, "components")); !equalStrings(got, []string{NodeIconStar}) {
		t.Fatalf("Icon components = %v", got)
	}
	for _, field := range []string{"name", "role", "lastModified", "editorType", "thumbnailUrl", "version"} {
		if _, ok := body[field]; !ok {
			t.Errorf("nodes response missing %s", field)
		}
	}
}

func TestNodesRequireIDs(t *testing.T) {
	_, ts := newTestServer(t)
	if resp := get(t, ts, filePath+"/nodes"); resp.status != http.StatusBadRequest {
		t.Fatalf("status %d", resp.status)
	}
}

func TestGeometryOnlyWithPaths(t *testing.T) {
	_, ts := newTestServer(t)
	plain := getOK(t, ts, filePath+"/nodes?ids=1:21")
	if _, ok := dig(t, plain, "nodes", "1:21", "document").(map[string]any)["fillGeometry"]; ok {
		t.Fatal("fillGeometry without geometry=paths")
	}
	paths := getOK(t, ts, filePath+"/nodes?ids=1:21&geometry=paths")
	if dig(t, paths, "nodes", "1:21", "document", "fillGeometry", 0, "windingRule") != "NONZERO" {
		t.Fatal("geometry=paths should add fillGeometry")
	}
}

func TestUnknownFileKeyIsNotFound(t *testing.T) {
	_, ts := newTestServer(t)
	for _, path := range []string{"/v1/files/NOPE", "/v1/files/NOPE/nodes?ids=1:1", "/v1/images/NOPE?ids=1:1", "/v1/files/NOPE/meta"} {
		resp := get(t, ts, path)
		body := resp.json(t)
		if resp.status != http.StatusNotFound || body["err"] != "Not found" || body["status"] != float64(404) {
			t.Errorf("%s: %d %s", path, resp.status, resp.body)
		}
	}
}

func TestFileVersionParam(t *testing.T) {
	_, ts := newTestServer(t)
	if v := getOK(t, ts, filePath+"?depth=1&version=999999999999999999")["version"]; v != "999999999999999999" {
		t.Fatalf("version = %v", v)
	}
	if resp := get(t, ts, filePath+"?version=123"); resp.status != http.StatusNotFound {
		t.Fatalf("unknown version: status %d", resp.status)
	}
}

func TestBranchData(t *testing.T) {
	_, ts := newTestServer(t)
	main := getOK(t, ts, filePath+"?depth=1&branch_data=true")
	if dig(t, main, "branches", 0, "key") != BranchKey {
		t.Fatalf("branches = %v", main["branches"])
	}
	branch := getOK(t, ts, "/v1/files/"+BranchKey+"?depth=1&branch_data=true")
	if branch["mainFileKey"] != FileKey || branch["name"] != BranchName || branch["version"] != InitialBranchVersion {
		t.Fatalf("branch = %v %v %v", branch["mainFileKey"], branch["name"], branch["version"])
	}
	if _, ok := getOK(t, ts, filePath+"?depth=1")["branches"]; ok {
		t.Fatal("branches without branch_data")
	}
}

func TestMetaVersionsAndComments(t *testing.T) {
	_, ts := newTestServer(t)
	meta := getOK(t, ts, filePath+"/meta")
	if dig(t, meta, "file", "version") != InitialVersion || dig(t, meta, "file", "url") != "https://www.figma.com/design/"+FileKey+"/Example-File" {
		t.Fatalf("meta = %v", meta["file"])
	}

	page := getOK(t, ts, filePath+"/versions?page_size=2")
	if got := len(page["versions"].([]any)); got != 2 {
		t.Fatalf("page 1 has %d versions", got)
	}
	next, _ := dig(t, page, "pagination", "next_page").(string)
	if next == "" {
		t.Fatalf("pagination = %v", page["pagination"])
	}
	if _, ok := dig(t, page, "pagination").(map[string]any)["prev_page"]; ok {
		t.Fatal("first page should have no prev_page")
	}
	resp := do(t, http.MethodGet, next, nil)
	page2 := resp.json(t)
	if dig(t, page2, "versions", 0, "id") != "999999999999999999" {
		t.Fatalf("page 2 starts at %v", dig(t, page2, "versions", 0, "id"))
	}
	if _, ok := dig(t, page2, "pagination").(map[string]any)["prev_page"]; !ok {
		t.Fatal("page 2 should link back")
	}
	newer := getOK(t, ts, filePath+"/versions?page_size=1&after=999999999999999999")
	if dig(t, newer, "versions", 0, "id") != "1000000000000000000" {
		t.Fatalf("after= should return the closest newer version, got %v", dig(t, newer, "versions", 0, "id"))
	}

	comments := getOK(t, ts, filePath+"/comments")["comments"].([]any)
	pinned := slices.IndexFunc(comments, func(c any) bool {
		meta, _ := c.(map[string]any)["client_meta"].(map[string]any)
		return meta["node_id"] == NodePriceRow
	})
	if len(comments) < 2 || pinned < 0 {
		t.Fatalf("comments = %v", comments)
	}
}

func TestMe(t *testing.T) {
	_, ts := newTestServer(t)
	me := getOK(t, ts, "/v1/me")
	for _, field := range []string{"id", "handle", "email", "img_url"} {
		if me[field] == "" || me[field] == nil {
			t.Errorf("me.%s missing", field)
		}
	}
}
