package mockfigma

import (
	"net/http"
	"net/url"
	"testing"
)

func TestParseRule(t *testing.T) {
	f, err := parseRule("null-render:I2:11;1:14:count=2")
	if err != nil || f.kind != faultNullRender || f.target != "I2:11;1:14" || f.count != 2 {
		t.Fatalf("null-render = %+v, %v", f, err)
	}
	f, err = parseRule("429:tier1:retry=7:type=low:plan=starter")
	if err != nil || f.target != "tier1" || f.retry != 7 || f.limitType != "low" || f.plan != "starter" || f.count != 0 {
		t.Fatalf("429 = %+v, %v", f, err)
	}
	f, err = parseRule("400-too-large:/v1/files:min_nodes=5")
	if err != nil || f.kind != faultTooLarge || f.minNodes != 5 {
		t.Fatalf("too-large alias = %+v, %v", f, err)
	}
	for _, bad := range []string{
		"bogus", "429", "429:nowhere", "429:tier1:type=medium", "429:tier1:plan=free", "429:tier1:color=red",
		"status:200:/v1", "status:500", "too-large", "render-timeout:/v1", "plan-limited:files", "render-500:count=x",
	} {
		if _, err := parseRule(bad); err == nil {
			t.Errorf("%q should not parse", bad)
		}
	}
}

func TestArmRejectsBadRules(t *testing.T) {
	_, ts := newTestServer(t)
	if resp := post(t, ts, "/_mock/faults?rules=bogus"); resp.status != http.StatusBadRequest {
		t.Fatalf("status %d", resp.status)
	}
	resp := do(t, http.MethodGet, ts.URL+"/v1/me", map[string]string{"X-Mock-Fault": "bogus"})
	if resp.status != http.StatusBadRequest {
		t.Fatalf("bad header: status %d", resp.status)
	}
}

func TestRateLimitFaultByTier(t *testing.T) {
	_, ts := newTestServer(t)
	arm(t, ts, "429:tier1:retry=2:type=low:plan=pro:count=1")

	if resp := get(t, ts, "/v1/me"); resp.status != http.StatusOK {
		t.Fatalf("tier3 should be unaffected, got %d", resp.status)
	}
	resp := get(t, ts, filePath+"?depth=1")
	if resp.status != http.StatusTooManyRequests {
		t.Fatalf("status %d", resp.status)
	}
	for header, want := range map[string]string{
		"Retry-After": "2", "X-Figma-Plan-Tier": "pro", "X-Figma-Rate-Limit-Type": "low", "X-Figma-Upgrade-Link": "https://www.figma.com/settings",
	} {
		if got := resp.header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if body := resp.json(t); body["status"] != float64(429) || body["err"] == nil {
		t.Errorf("body = %v", body)
	}
	if resp := get(t, ts, filePath+"?depth=1"); resp.status != http.StatusOK {
		t.Fatalf("count=1 should be spent, got %d", resp.status)
	}
}

func TestRateLimitFaultByPathUnlimited(t *testing.T) {
	_, ts := newTestServer(t)
	arm(t, ts, "429:/v1/files/"+FileKey+"/comments")
	for range 3 {
		if resp := get(t, ts, filePath+"/comments"); resp.status != http.StatusTooManyRequests {
			t.Fatalf("status %d", resp.status)
		}
	}
	if resp := get(t, ts, filePath+"/versions"); resp.status != http.StatusOK {
		t.Fatalf("other path: status %d", resp.status)
	}
}

func TestTooLargeFaultHonoursMinNodes(t *testing.T) {
	_, ts := newTestServer(t)
	arm(t, ts, "too-large:/v1/files:min_nodes=10")

	resp := get(t, ts, filePath)
	if body := resp.json(t); resp.status != http.StatusBadRequest || body["err"] != "Request too large. If applicable, filter by query params." {
		t.Fatalf("%d %s", resp.status, resp.body)
	}
	if resp := get(t, ts, filePath+"?depth=2"); resp.status != http.StatusOK {
		t.Fatalf("small response should pass, got %d", resp.status)
	}
	if resp := get(t, ts, filePath+"/nodes?ids="+NodeCheckout+"&depth=2"); resp.status != http.StatusBadRequest {
		t.Fatalf("large nodes response should fail, got %d", resp.status)
	}
	if resp := get(t, ts, filePath+"/nodes?ids="+NodePriceRow); resp.status != http.StatusOK {
		t.Fatalf("small nodes response should pass, got %d", resp.status)
	}
}

func TestRenderTimeoutFaultHonoursMinIDs(t *testing.T) {
	_, ts := newTestServer(t)
	arm(t, ts, "render-timeout:min_ids=3")

	if resp := get(t, ts, imagesPath+"?ids=2:10,2:11"); resp.status != http.StatusOK {
		t.Fatalf("2 ids: status %d", resp.status)
	}
	resp := get(t, ts, imagesPath+"?ids=2:10,2:11,2:12")
	if body := resp.json(t); resp.status != http.StatusBadRequest || body["err"] != "Render timeout, try requesting fewer or smaller images" {
		t.Fatalf("%d %s", resp.status, resp.body)
	}
}

func TestRender500FaultCount(t *testing.T) {
	_, ts := newTestServer(t)
	arm(t, ts, "render-500:count=1")
	if resp := get(t, ts, imagesPath+"?ids=2:10"); resp.status != http.StatusInternalServerError {
		t.Fatalf("status %d", resp.status)
	}
	if resp := get(t, ts, imagesPath+"?ids=2:10"); resp.status != http.StatusOK {
		t.Fatalf("retry: status %d", resp.status)
	}
}

func TestNullRenderFault(t *testing.T) {
	_, ts := newTestServer(t)
	arm(t, ts, url.QueryEscape("null-render:"+NodeCheckout+",null-render:"+NodeInstanceLabel))
	body := getOK(t, ts, imagesPath+"?ids="+NodeCheckout+","+NodeCard+","+url.QueryEscape(NodeInstanceLabel))
	images := body["images"].(map[string]any)
	if images[NodeCheckout] != nil || images[NodeInstanceLabel] != nil || images[NodeCard] == nil {
		t.Fatalf("images = %v", images)
	}
}

func TestStatusFault(t *testing.T) {
	_, ts := newTestServer(t)
	arm(t, ts, "status:503:/v1/files/"+FileKey+"/versions:count=2")
	for range 2 {
		resp := get(t, ts, filePath+"/versions")
		if body := resp.json(t); resp.status != http.StatusServiceUnavailable || body["status"] != float64(503) {
			t.Fatalf("%d %s", resp.status, resp.body)
		}
	}
	if resp := get(t, ts, filePath+"/versions"); resp.status != http.StatusOK {
		t.Fatalf("after count: status %d", resp.status)
	}
}

func TestPlanLimitedVariables(t *testing.T) {
	_, ts := newTestServer(t)
	if body := getOK(t, ts, filePath+"/variables/local"); dig(t, body, "meta", "variableCollections", variableCollectionID, "name") != "Tokens" {
		t.Fatalf("local variables = %v", body)
	}
	arm(t, ts, "plan-limited:variables")
	for _, p := range []string{"/variables/local", "/variables/published"} {
		resp := get(t, ts, filePath+p)
		body := resp.json(t)
		if resp.status != http.StatusForbidden || body["message"] != "Limited by Figma plan" || body["error"] != true {
			t.Errorf("%s: %d %s", p, resp.status, resp.body)
		}
	}
	if resp := get(t, ts, filePath+"/styles"); resp.status != http.StatusOK {
		t.Errorf("styles: status %d", resp.status)
	}
}

func TestCDNExpiredFault(t *testing.T) {
	_, ts := newTestServer(t)
	arm(t, ts, "cdn-expired:/_cdn/render:count=1")
	if resp := get(t, ts, "/_cdn/fill/"+ImageRefHero+".png"); resp.status != http.StatusOK {
		t.Fatalf("fill: status %d", resp.status)
	}
	if resp := get(t, ts, "/_cdn/render/2%3A10.png"); resp.status != http.StatusForbidden {
		t.Fatalf("render: status %d", resp.status)
	}
	if resp := get(t, ts, "/_cdn/render/2%3A10.png"); resp.status != http.StatusOK {
		t.Fatalf("after count: status %d", resp.status)
	}
}

func TestHeaderFaultAppliesToOneRequest(t *testing.T) {
	_, ts := newTestServer(t)
	resp := do(t, http.MethodGet, ts.URL+filePath+"?depth=1", map[string]string{"X-Mock-Fault": "429:tier1:retry=5"})
	if resp.status != http.StatusTooManyRequests || resp.header.Get("Retry-After") != "5" {
		t.Fatalf("%d %v", resp.status, resp.header)
	}
	if resp := get(t, ts, filePath+"?depth=1"); resp.status != http.StatusOK {
		t.Fatalf("without header: status %d", resp.status)
	}
	counted := map[string]string{"X-Mock-Fault": "render-500:count=1"}
	if resp := do(t, http.MethodGet, ts.URL+imagesPath+"?ids=2:10", counted); resp.status != http.StatusInternalServerError {
		t.Fatalf("header count first: %d", resp.status)
	}
	if resp := do(t, http.MethodGet, ts.URL+imagesPath+"?ids=2:10", counted); resp.status != http.StatusOK {
		t.Fatalf("header count spent: %d", resp.status)
	}
}

func TestClearAndDescribeFaults(t *testing.T) {
	_, ts := newTestServer(t)
	arm(t, ts, "429:tier3")
	described := getOK(t, ts, "/_mock/faults")
	if described["faults"] != "429:tier3" {
		t.Fatalf("faults = %v", described)
	}
	if resp := do(t, http.MethodDelete, ts.URL+"/_mock/faults", nil); resp.status != http.StatusOK {
		t.Fatalf("delete: status %d", resp.status)
	}
	if resp := get(t, ts, "/v1/me"); resp.status != http.StatusOK {
		t.Fatalf("after delete: status %d", resp.status)
	}
}
