package cli

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/shhac/agent-figma/internal/mockfigma"
)

const fileKey = mockfigma.FileKey

func fileLink(node string) string {
	link := "https://www.figma.com/design/" + fileKey + "/Example-File"
	if node != "" {
		link += "?node-id=" + strings.ReplaceAll(node, ":", "-")
	}
	return link
}

// mockEnv isolates XDG dirs and points the CLI at a fresh mock Figma.
func mockEnv(t *testing.T) *mockfigma.Server {
	t.Helper()
	isolate(t)
	srv := mockfigma.New()
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	t.Setenv("AGENT_FIGMA_BASE_URL", ts.URL)
	t.Setenv("AGENT_FIGMA_TOKEN", "fake-token")
	return srv
}

func mustRun(t *testing.T, args ...string) result {
	t.Helper()
	r := run(t, args...)
	if r.code != 0 {
		t.Fatalf("%v failed: %s", args, r.stderr)
	}
	return r
}

// meta returns the value of the first @key line.
func (r result) meta(t *testing.T, key string) any {
	t.Helper()
	for _, line := range r.lines(t) {
		if v, ok := line[key]; ok {
			return v
		}
	}
	return nil
}

func (r result) metaAll(t *testing.T, key string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range r.lines(t) {
		if v, ok := line[key].(map[string]any); ok {
			out = append(out, v)
		}
	}
	return out
}

func (r result) records(t *testing.T) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, line := range r.lines(t) {
		if id, ok := line["id"].(string); ok {
			out[id] = line
		}
	}
	return out
}

func TestOutlineIsCachedAfterOneTier1Call(t *testing.T) {
	srv := mockEnv(t)
	r := mustRun(t, "file", "outline", fileLink(""))
	recs := r.records(t)
	if recs[mockfigma.NodeScreensPage]["name"] != "Screens" || recs[mockfigma.NodeCheckout] == nil {
		t.Fatalf("outline: %s", r.stdout)
	}
	if srv.Stats().Tier1 != 1 {
		t.Fatalf("tier1 = %d", srv.Stats().Tier1)
	}
	mustRun(t, "file", "outline", fileKey)
	if srv.Stats().Tier1 != 1 {
		t.Fatal("a repeated outline should come from the cache")
	}
	file, _ := r.meta(t, "@file").(map[string]any)
	if file["version"] != mockfigma.InitialVersion || file["name"] != mockfigma.FileName {
		t.Fatalf("@file: %v", file)
	}
}

func TestNodesGetSimplifiesAndCachesSubtree(t *testing.T) {
	srv := mockEnv(t)
	r := mustRun(t, "nodes", "get", fileLink(mockfigma.NodeCheckout))
	recs := r.records(t)
	inst := recs[mockfigma.NodeButtonInstance]
	comp, _ := inst["component"].(map[string]any)
	if comp["id"] != mockfigma.NodeButtonDefault || comp["set"] != "Button" {
		t.Fatalf("instance component: %v", inst)
	}
	if _, ok := recs[mockfigma.NodeHiddenNote]; ok {
		t.Fatal("hidden nodes must be dropped")
	}
	if recs[mockfigma.NodeLogoGroup]["svg"] != true {
		t.Fatalf("vector group should collapse: %v", recs[mockfigma.NodeLogoGroup])
	}
	if r.meta(t, "@styles") == nil {
		t.Fatal("expected a @styles table")
	}
	rels := r.metaAll(t, "@related")
	if !hasRel(rels, "page", mockfigma.NodeScreensPage) {
		t.Fatalf("expected the page in @related: %v", rels)
	}
	tier1 := srv.Stats().Tier1
	// A node inside the cached subtree costs nothing.
	mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCardBody)
	if srv.Stats().Tier1 != tier1 {
		t.Fatal("a cached descendant should not spend a Tier 1 call")
	}
	budget, _ := r.meta(t, "@budget").(map[string]any)
	if budget == nil {
		t.Fatal("expected @budget")
	}
}

func hasRel(rels []map[string]any, rel, id string) bool {
	for _, r := range rels {
		if r["rel"] == rel && r["id"] == id {
			return true
		}
	}
	return false
}

func TestVariantPrefetchSavesCalls(t *testing.T) {
	srv := mockEnv(t)
	// The Tier 3 library says which set each published variant is in, so the
	// first Tier 1 call for one variant is widened to the whole set...
	mustRun(t, "components", "list", fileKey)
	r := mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeButtonDefault)
	if srv.Stats().Tier1 != 1 {
		t.Fatalf("tier1 = %d", srv.Stats().Tier1)
	}
	rels := r.metaAll(t, "@related")
	if !hasRel(rels, "component_set", mockfigma.NodeButtonSet) || !hasRel(rels, "variant", mockfigma.NodeButtonDisabled) {
		t.Fatalf("expected the set and sibling variants in @related: %s", r.stdout)
	}
	// ...so its siblings are already cached.
	mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeButtonHover)
	mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeButtonDisabled)
	if srv.Stats().Tier1 != 1 {
		t.Fatalf("prefetched variants should be cached, tier1 = %d", srv.Stats().Tier1)
	}
}

func TestNoPrefetchFetchesOnlyWhatWasAsked(t *testing.T) {
	srv := mockEnv(t)
	mustRun(t, "components", "list", fileKey)
	mustRun(t, "--no-prefetch", "nodes", "get", fileKey, "--node", mockfigma.NodeButtonDefault)
	mustRun(t, "--no-prefetch", "nodes", "get", fileKey, "--node", mockfigma.NodeButtonHover)
	if srv.Stats().Tier1 != 2 {
		t.Fatalf("tier1 = %d", srv.Stats().Tier1)
	}
}

func TestRenderWidensToSiblingVariants(t *testing.T) {
	srv := mockEnv(t)
	mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeButtonSet)
	before := srv.Stats().Tier1
	r := mustRun(t, "render", fileKey, "--node", mockfigma.NodeButtonDefault)
	if srv.Stats().Tier1 != before+1 {
		t.Fatalf("render should be one Tier 1 call")
	}
	recs := r.records(t)
	img := recs[mockfigma.NodeButtonDefault]
	path, _ := img["path"].(string)
	if !strings.Contains(path, "/renders/components/") || !strings.HasSuffix(path, "@2x.png") {
		t.Fatalf("render path: %v", img)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("render not on disk: %v", err)
	}
	if fileRef, _ := img["file"].(map[string]any); fileRef["root"] != "cache" {
		t.Fatalf("expected a cache FileRef: %v", img)
	}
	rels := r.metaAll(t, "@related")
	if !hasRel(rels, "render", mockfigma.NodeButtonHover) {
		t.Fatalf("sibling variants should be rendered alongside: %s", r.stdout)
	}
	// The sibling's URL is cached, so rendering it costs no API call.
	r = mustRun(t, "render", fileKey, "--node", mockfigma.NodeButtonHover)
	if srv.Stats().Tier1 != before+1 {
		t.Fatal("rendering a prefetched sibling should not call Figma")
	}
	if r.records(t)[mockfigma.NodeButtonHover]["downloaded"] != true {
		t.Fatal("the sibling should have been downloaded now")
	}
}

func TestRenderNullIsUnresolvedNotFailure(t *testing.T) {
	mockEnv(t)
	r := mustRun(t, "render", fileKey, "--node", mockfigma.NodeHiddenNote, "--no-prefetch")
	un := r.metaAll(t, "@unresolved")
	if len(un) != 1 || un[0]["id"] != mockfigma.NodeHiddenNote {
		t.Fatalf("unresolved: %s", r.stdout)
	}
}

func TestRenderTimeoutRetriesWithoutExtras(t *testing.T) {
	srv := mockEnv(t)
	mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeButtonSet)
	if err := srv.SetFaults("render-timeout:min_ids=2"); err != nil {
		t.Fatal(err)
	}
	r := mustRun(t, "render", fileKey, "--node", mockfigma.NodeButtonDefault)
	if r.records(t)[mockfigma.NodeButtonDefault] == nil {
		t.Fatalf("the requested node should still render: %s", r.stdout)
	}
}

func TestLongRateLimitFailsFastAndIsShared(t *testing.T) {
	srv := mockEnv(t)
	if err := srv.SetFaults("429:tier1:retry=7200:type=low:plan=pro"); err != nil {
		t.Fatal(err)
	}
	r := run(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout)
	e := r.errJSON(t)
	if r.code == 0 || e["fixable_by"] != "human" || e["retry_after_seconds"].(float64) != 7200 {
		t.Fatalf("429: %s", r.stderr)
	}
	calls := srv.Stats().Tier1
	r = run(t, "render", fileKey, "--node", mockfigma.NodeCheckout)
	if r.code == 0 || srv.Stats().Tier1 != calls {
		t.Fatalf("a second process should honour the cooldown without calling Figma: %s", r.stderr)
	}
	// Tier 3 is a different bucket and still works.
	mustRun(t, "file", "meta", fileKey)
}

func TestShortRateLimitIsWaitedOut(t *testing.T) {
	srv := mockEnv(t)
	if err := srv.SetFaults("429:tier3:retry=1:count=1"); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "file", "meta", fileKey)
}

func TestRequestTooLargeFallsBackToDepth(t *testing.T) {
	srv := mockEnv(t)
	if err := srv.SetFaults("too-large:/v1/files/" + fileKey + ":min_nodes=20"); err != nil {
		t.Fatal(err)
	}
	r := mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout, "--view", "structure")
	if r.records(t)[mockfigma.NodeCheckout] == nil || !strings.Contains(r.stderr, "too large") {
		t.Fatalf("fallback should still answer: %s %s", r.stdout, r.stderr)
	}
}

func TestVersionChangeAndStaleServing(t *testing.T) {
	srv := mockEnv(t)
	mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout)
	if _, ok := srv.BumpVersion(fileKey); !ok {
		t.Fatal("bump")
	}
	// Re-check the version on every command, without --refresh.
	mustRun(t, "config", "set", "cache_fresh_s", "0")
	before := srv.Stats().Tier1
	mustRun(t, "config", "set", "serve_stale", "on")
	r := mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout)
	file, _ := r.meta(t, "@file").(map[string]any)
	if file["stale"] != true || file["latest_version"] == mockfigma.InitialVersion || srv.Stats().Tier1 != before {
		t.Fatalf("stale answer expected: %v (tier1 %d→%d)", file, before, srv.Stats().Tier1)
	}
	// --refresh asks for the current version even when stale answers are on.
	r = mustRun(t, "--refresh", "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout)
	file, _ = r.meta(t, "@file").(map[string]any)
	if file["stale"] == true || srv.Stats().Tier1 != before+1 {
		t.Fatalf("--refresh should refetch: %v", file)
	}
}

func TestServeStaleOffRefetches(t *testing.T) {
	srv := mockEnv(t)
	mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout)
	srv.BumpVersion(fileKey)
	mustRun(t, "config", "set", "cache_fresh_s", "0")
	mustRun(t, "config", "set", "serve_stale", "off")
	before := srv.Stats().Tier1
	r := mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout)
	if file, _ := r.meta(t, "@file").(map[string]any); file["stale"] == true || srv.Stats().Tier1 != before+1 {
		t.Fatalf("a fresh fetch expected: %v", file)
	}
}

func TestOffline(t *testing.T) {
	srv := mockEnv(t)
	r := run(t, "--offline", "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout)
	if r.code == 0 || r.errJSON(t)["fixable_by"] != "agent" {
		t.Fatalf("offline with nothing cached: %s", r.stderr)
	}
	mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout)
	calls := srv.Stats()
	mustRun(t, "--offline", "nodes", "get", fileKey, "--node", mockfigma.NodeTotalText)
	after := srv.Stats()
	if after.Tier1+after.Tier2+after.Tier3 != calls.Tier1+calls.Tier2+calls.Tier3 {
		t.Fatal("--offline must not call Figma")
	}
}

func TestUnknownNodeIsUnresolved(t *testing.T) {
	mockEnv(t)
	r := mustRun(t, "nodes", "get", fileKey, "--node", "99:99")
	un := r.metaAll(t, "@unresolved")
	if len(un) != 1 || un[0]["id"] != "99:99" {
		t.Fatalf("unresolved: %s", r.stdout)
	}
}

func TestAssets(t *testing.T) {
	mockEnv(t)
	r := mustRun(t, "assets", "fills", fileKey, "--node", mockfigma.NodeCheckout)
	recs := r.lines(t)
	found := false
	for _, rec := range recs {
		if rec["image_ref"] == mockfigma.ImageRefHero {
			found = true
			if rec["mime"] != "image/png" {
				t.Fatalf("fill: %v", rec)
			}
		}
	}
	if !found {
		t.Fatalf("expected the hero fill: %s", r.stdout)
	}
	r = mustRun(t, "assets", "export", fileKey, "--node", mockfigma.NodeCheckout)
	var formats []string
	for _, rec := range r.lines(t) {
		if rec["id"] == mockfigma.NodeExportFrame {
			formats = append(formats, rec["format"].(string))
		}
	}
	if len(formats) != 2 {
		t.Fatalf("export settings PNG@2x + SVG: %s", r.stdout)
	}
	out := t.TempDir()
	r = mustRun(t, "render", fileKey, "--node", mockfigma.NodeCheckout, "--out", out)
	if path, _ := r.records(t)[mockfigma.NodeCheckout]["path"].(string); !strings.HasPrefix(path, out+"/screens/") {
		t.Fatalf("--out path: %s", r.stdout)
	}
}

func TestLibraryAndIndex(t *testing.T) {
	mockEnv(t)
	r := mustRun(t, "components", "list", fileLink(""))
	if len(r.lines(t)) < 4 {
		t.Fatalf("components: %s", r.stdout)
	}
	r = mustRun(t, "components", "get", mockfigma.ButtonDefaultKey, "missingkey")
	lines := r.lines(t)
	if len(lines) != 2 || lines[0]["key"] != mockfigma.ButtonDefaultKey || lines[1]["@unresolved"] == nil {
		t.Fatalf("components get: %s", r.stdout)
	}
	// Branch links still reach library endpoints through the main key.
	branch := "https://www.figma.com/design/" + fileKey + "/branch/" + mockfigma.BranchKey + "/Example"
	mustRun(t, "styles", "list", branch)

	r = mustRun(t, "file", "index", fileKey)
	var set map[string]any
	kinds := map[string]int{}
	for _, rec := range r.lines(t) {
		kind, _ := rec["kind"].(string)
		kinds[kind]++
		if kind == "component_set" && rec["name"] == "Button" {
			set = rec
		}
	}
	if kinds["page"] != 3 || kinds["style"] != 4 || set == nil || len(set["variants"].([]any)) != 3 {
		t.Fatalf("index: %v %s", kinds, r.stdout)
	}
}

func TestOtherReads(t *testing.T) {
	srv := mockEnv(t)
	r := mustRun(t, "versions", "list", fileKey, "--limit", "2")
	if len(r.metaAll(t, "@pagination")) != 1 {
		t.Fatalf("versions should paginate: %s", r.stdout)
	}
	cursor := r.metaAll(t, "@pagination")[0]["next_cursor"].(string)
	mustRun(t, "versions", "list", fileKey, "--limit", "2", "--cursor", cursor)
	r = mustRun(t, "comments", "list", fileKey)
	if !strings.Contains(r.stdout, mockfigma.NodePriceRow) {
		t.Fatalf("comments should carry node ids: %s", r.stdout)
	}
	mustRun(t, "variables", "list", fileKey)
	mustRun(t, "dev-resources", "list", fileKey)
	if err := srv.SetFaults("plan-limited:variables"); err != nil {
		t.Fatal(err)
	}
	r = run(t, "variables", "list", fileKey)
	if r.code == 0 || r.errJSON(t)["fixable_by"] != "human" {
		t.Fatalf("plan-limited variables: %s", r.stderr)
	}
}

func TestNodesFind(t *testing.T) {
	mockEnv(t)
	r := mustRun(t, "nodes", "find", fileKey, "--type", "CANVAS")
	if len(r.records(t)) != 3 {
		t.Fatalf("find pages: %s", r.stdout)
	}
	mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout)
	r = mustRun(t, "nodes", "find", fileKey, "--name", "price*")
	if r.records(t)[mockfigma.NodePriceRow] == nil {
		t.Fatalf("find by name: %s", r.stdout)
	}
}

func TestExpiredRenderURLIsForgotten(t *testing.T) {
	srv := mockEnv(t)
	if err := srv.SetFaults("cdn-expired"); err != nil {
		t.Fatal(err)
	}
	r := mustRun(t, "render", fileKey, "--node", mockfigma.NodeCheckout, "--no-prefetch")
	un := r.metaAll(t, "@unresolved")
	if len(un) != 1 || un[0]["fixable_by"] != "retry" {
		t.Fatalf("expired download should be retryable: %s", r.stdout)
	}
	if err := srv.SetFaults(""); err != nil {
		t.Fatal(err)
	}
	before := srv.Stats().ByPath["/v1/images/"+fileKey]
	r = mustRun(t, "render", fileKey, "--node", mockfigma.NodeCheckout, "--no-prefetch")
	if srv.Stats().ByPath["/v1/images/"+fileKey] != before+1 {
		t.Fatal("the dead URL should have been forgotten, forcing a fresh render")
	}
	if r.records(t)[mockfigma.NodeCheckout]["downloaded"] != true {
		t.Fatalf("second render should download: %s", r.stdout)
	}
}

func TestMaxNodesContinuationTerminates(t *testing.T) {
	srv := mockEnv(t)
	seen := map[string]bool{}
	node := mockfigma.NodeCheckout
	for round := 0; node != ""; round++ {
		if round > 10 {
			t.Fatal("continuation does not terminate")
		}
		r := mustRun(t, "nodes", "get", fileKey, "--node", node, "--max-nodes", "4", "--view", "structure")
		for id := range r.records(t) {
			if seen[id] {
				t.Fatalf("%s printed twice", id)
			}
			seen[id] = true
		}
		node = ""
		if p := r.metaAll(t, "@pagination"); len(p) == 1 {
			node, _ = p[0]["next_cursor"].(string)
		}
	}
	if srv.Stats().Tier1 != 1 || !seen[mockfigma.NodePriceText] {
		t.Fatalf("continuation should walk the cached subtree: tier1=%d seen=%v", srv.Stats().Tier1, seen)
	}
}
