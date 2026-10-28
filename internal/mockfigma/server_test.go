package mockfigma

import (
	"net/http"
	"strings"
	"testing"
)

func TestAuth(t *testing.T) {
	_, ts := newTestServer(t)
	cases := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"figma token", map[string]string{"X-Figma-Token": "anything"}, http.StatusOK},
		{"bearer", map[string]string{"Authorization": "Bearer anything"}, http.StatusOK},
		{"missing", map[string]string{"X-Figma-Token": ""}, http.StatusForbidden},
		{"expired", map[string]string{"X-Figma-Token": ExpiredToken}, http.StatusForbidden},
		{"expired bearer", map[string]string{"Authorization": "Bearer " + ExpiredToken}, http.StatusForbidden},
	}
	for _, tc := range cases {
		resp := do(t, http.MethodGet, ts.URL+"/v1/me", tc.headers)
		if resp.status != tc.want {
			t.Errorf("%s: status %d", tc.name, resp.status)
			continue
		}
		if tc.want == http.StatusForbidden {
			if body := resp.json(t); body["err"] != "Invalid token" || body["status"] != float64(403) {
				t.Errorf("%s: body %s", tc.name, resp.body)
			}
		}
	}
}

func TestStatsCountAPICallsByTier(t *testing.T) {
	s, ts := newTestServer(t)
	for _, p := range []string{
		filePath + "?depth=1",
		filePath + "/nodes?ids=2:10",
		imagesPath + "?ids=2:10",
		filePath + "/images",
		filePath + "/comments",
		"/v1/me",
		filePath + "/meta",
		"/_cdn/render/2%3A10.png",
		"/_mock/stats",
		"/",
	} {
		_ = get(t, ts, p)
	}
	stats := getOK(t, ts, "/_mock/stats")
	if stats["tier1"] != float64(3) || stats["tier2"] != float64(2) || stats["tier3"] != float64(2) {
		t.Fatalf("tiers = %v %v %v", stats["tier1"], stats["tier2"], stats["tier3"])
	}
	if dig(t, stats, "by_path", filePath) != float64(1) || dig(t, stats, "by_path", "/v1/me") != float64(1) {
		t.Fatalf("by_path = %v", stats["by_path"])
	}
	if dig(t, stats, "requests", 0, "path") != filePath || dig(t, stats, "requests", 0, "query") != "depth=1" {
		t.Fatalf("requests[0] = %v", dig(t, stats, "requests", 0))
	}
	if got := s.Stats(); got.Tier1 != 3 || len(got.Requests) != 7 {
		t.Fatalf("Stats() = %+v", got)
	}

	if resp := post(t, ts, "/_mock/reset"); resp.status != http.StatusOK {
		t.Fatalf("reset: %d", resp.status)
	}
	if got := s.Stats(); got.Tier1 != 0 || len(got.Requests) != 0 {
		t.Fatalf("after reset: %+v", got)
	}
}

func TestStatsCountFaultedAndUnauthorisedCalls(t *testing.T) {
	s, ts := newTestServer(t)
	arm(t, ts, "429:tier1")
	_ = get(t, ts, filePath)
	_ = do(t, http.MethodGet, ts.URL+filePath, map[string]string{"X-Figma-Token": ExpiredToken})
	if got := s.Stats().Tier1; got != 2 {
		t.Fatalf("tier1 = %d", got)
	}
}

func TestLibraryEndpointsRejectBranchKey(t *testing.T) {
	_, ts := newTestServer(t)
	for _, p := range []string{"/components", "/component_sets", "/styles", "/dev_resources"} {
		resp := get(t, ts, "/v1/files/"+BranchKey+p)
		body := resp.json(t)
		if resp.status != http.StatusNotFound || body["status"] != float64(404) {
			t.Errorf("branch %s: %d %s", p, resp.status, resp.body)
		}
		if resp := get(t, ts, filePath+p); resp.status != http.StatusOK {
			t.Errorf("main %s: status %d", p, resp.status)
		}
	}
	for _, p := range []string{"", "/meta", "/versions", "/comments", "/images", "/variables/local"} {
		if resp := get(t, ts, "/v1/files/"+BranchKey+p+"?depth=1"); resp.status != http.StatusOK {
			t.Errorf("branch %s should be accepted, got %d", p, resp.status)
		}
	}
}

func TestLibraryLists(t *testing.T) {
	_, ts := newTestServer(t)
	components := getOK(t, ts, filePath+"/components")
	if components["error"] != false || components["status"] != float64(200) {
		t.Fatalf("envelope = %v", components)
	}
	list := dig(t, components, "meta", "components").([]any)
	if len(list) != 4 {
		t.Fatalf("components = %d", len(list))
	}
	first := list[0]
	if dig(t, first, "containing_frame", "containingComponentSet", "nodeId") != NodeButtonSet ||
		dig(t, first, "containing_frame", "pageName") != "Components" || dig(t, first, "file_key") != FileKey {
		t.Fatalf("first component = %v", first)
	}
	if got := dig(t, getOK(t, ts, filePath+"/component_sets"), "meta", "component_sets", 0, "key"); got != ButtonSetKey {
		t.Fatalf("component set key = %v", got)
	}
	styles := dig(t, getOK(t, ts, filePath+"/styles"), "meta", "styles").([]any)
	if len(styles) != 4 || dig(t, styles[0], "name") != "Brand/Primary" || dig(t, styles[0], "style_type") != "FILL" {
		t.Fatalf("styles = %v", styles)
	}
}

func TestLibraryGetByKey(t *testing.T) {
	_, ts := newTestServer(t)
	if got := dig(t, getOK(t, ts, "/v1/components/"+ButtonHoverKey), "meta", "node_id"); got != NodeButtonHover {
		t.Fatalf("component node_id = %v", got)
	}
	if got := dig(t, getOK(t, ts, "/v1/component_sets/"+ButtonSetKey), "meta", "name"); got != "Button" {
		t.Fatalf("component set name = %v", got)
	}
	if got := dig(t, getOK(t, ts, "/v1/styles/"+HeadingH1Key), "meta", "style_type"); got != "TEXT" {
		t.Fatalf("style type = %v", got)
	}
	for _, p := range []string{"/v1/components/nope", "/v1/component_sets/nope", "/v1/styles/nope"} {
		if resp := get(t, ts, p); resp.status != http.StatusNotFound {
			t.Errorf("%s: status %d", p, resp.status)
		}
	}
}

func TestDevResourcesFilter(t *testing.T) {
	_, ts := newTestServer(t)
	all := getOK(t, ts, filePath+"/dev_resources")["dev_resources"].([]any)
	if len(all) != 2 {
		t.Fatalf("dev resources = %v", all)
	}
	some := getOK(t, ts, filePath+"/dev_resources?node_ids="+NodeCheckout)["dev_resources"].([]any)
	if len(some) != 1 || dig(t, some[0], "node_id") != NodeCheckout {
		t.Fatalf("filtered = %v", some)
	}
}

func TestPublishedVariables(t *testing.T) {
	_, ts := newTestServer(t)
	body := getOK(t, ts, filePath+"/variables/published")
	if dig(t, body, "meta", "variables", variableBrandID, "resolvedDataType") != "COLOR" {
		t.Fatalf("published = %v", body)
	}
}

func TestVersionBump(t *testing.T) {
	_, ts := newTestServer(t)
	before := getOK(t, ts, filePath+"?depth=1")

	resp := post(t, ts, "/_mock/version/bump")
	if resp.status != http.StatusOK || resp.json(t)["version"] != "1000000000000000002" {
		t.Fatalf("bump: %d %s", resp.status, resp.body)
	}
	after := getOK(t, ts, filePath+"?depth=1")
	if after["version"] != "1000000000000000002" {
		t.Fatalf("version = %v", after["version"])
	}
	if after["lastModified"].(string) <= before["lastModified"].(string) {
		t.Fatalf("lastModified did not advance: %v -> %v", before["lastModified"], after["lastModified"])
	}
	if got := dig(t, getOK(t, ts, filePath+"/meta"), "file", "version"); got != "1000000000000000002" {
		t.Fatalf("meta version = %v", got)
	}
	if got := dig(t, getOK(t, ts, filePath+"/versions"), "versions", 0, "id"); got != "1000000000000000002" {
		t.Fatalf("newest version = %v", got)
	}
	if got := getOK(t, ts, "/v1/files/"+BranchKey+"?depth=1")["version"]; got != InitialBranchVersion {
		t.Fatalf("branch version should not move, got %v", got)
	}

	_ = post(t, ts, "/_mock/reset")
	if got := getOK(t, ts, filePath+"?depth=1")["version"]; got != InitialVersion {
		t.Fatalf("after reset version = %v", got)
	}
}

func TestIndexAndUnknownRoutes(t *testing.T) {
	_, ts := newTestServer(t)
	resp := do(t, http.MethodGet, ts.URL+"/", map[string]string{"X-Figma-Token": ""})
	body := resp.json(t)
	if resp.status != http.StatusOK || body["service"] != "mockfigma" || len(body["routes"].([]any)) == 0 {
		t.Fatalf("index = %d %s", resp.status, resp.body)
	}
	if resp := get(t, ts, "/v1/nope"); resp.status != http.StatusNotFound {
		t.Fatalf("unknown route: %d", resp.status)
	}
	joined := strings.Join(Routes(), "\n")
	for _, want := range []string{"/v1/files/{key}/nodes", "/_mock/stats", "null-render:<node-id>"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Routes() missing %q", want)
		}
	}
}
