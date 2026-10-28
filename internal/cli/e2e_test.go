package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/mockfigma"
)

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

func TestUnknownNodeIsUnresolvedAndRemembered(t *testing.T) {
	srv := mockEnv(t)
	for i := 0; i < 2; i++ {
		r := mustRun(t, "nodes", "get", fileKey, "--node", "99:99")
		un := r.metaAll(t, "@unresolved")
		if len(un) != 1 || un[0]["id"] != "99:99" {
			t.Fatalf("unresolved: %s", r.stdout)
		}
	}
	if srv.Stats().Tier1 != 1 {
		t.Fatalf("a known miss should not be asked about again: tier1=%d", srv.Stats().Tier1)
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

func TestWithRelatedDownloadsCachedSiblings(t *testing.T) {
	srv := mockEnv(t)
	mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeButtonSet)
	mustRun(t, "render", fileKey, "--node", mockfigma.NodeButtonDefault)
	before := srv.Stats().Tier1
	r := mustRun(t, "render", fileKey, "--node", mockfigma.NodeButtonDefault, "--with-related")
	recs := r.records(t)
	if recs[mockfigma.NodeButtonHover]["downloaded"] != true || recs[mockfigma.NodeButtonDisabled]["downloaded"] != true {
		t.Fatalf("--with-related should download the cached siblings: %s", r.stdout)
	}
	if srv.Stats().Tier1 != before {
		t.Fatal("sibling URLs were cached; no render call needed")
	}
}

func TestMetaForbiddenFallsBackWithNotice(t *testing.T) {
	srv := mockEnv(t)
	if err := srv.SetFaults("status:403:/v1/files/" + fileKey + "/meta"); err != nil {
		t.Fatal(err)
	}
	r := mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeTotalText)
	if !strings.Contains(r.stderr, "file_metadata:read") || r.records(t)[mockfigma.NodeTotalText] == nil {
		t.Fatalf("expected an answer plus a scope notice: %s %s", r.stdout, r.stderr)
	}
	if file, _ := r.meta(t, "@file").(map[string]any); file["version"] != mockfigma.InitialVersion {
		t.Fatalf("the Tier 1 response's version should be used: %v", file)
	}
}

func TestMetaOutageUsesLastKnownVersion(t *testing.T) {
	srv := mockEnv(t)
	mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout)
	mustRun(t, "config", "set", "cache_fresh_s", "0")
	if err := srv.SetFaults("status:503:/v1/files/" + fileKey + "/meta"); err != nil {
		t.Fatal(err)
	}
	before := srv.Stats().Tier1
	r := mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout)
	if srv.Stats().Tier1 != before || !strings.Contains(r.stderr, "last known version") {
		t.Fatalf("a /meta outage should fall back to the cache: %s", r.stderr)
	}
	// With nothing cached for another file, the outage is the answer.
	mustRun(t, "cache", "clear", "--all")
	if r := run(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout); r.code == 0 || r.errJSON(t)["fixable_by"] != "retry" {
		t.Fatalf("no head and /meta down should fail retryably: %s", r.stderr)
	}
}

func TestCacheClearTargetsTheSelectedPartition(t *testing.T) {
	mockEnv(t)
	mustRun(t, "file", "outline", fileKey)
	t.Setenv("AGENT_FIGMA_TOKEN", "")
	mustRun(t, "auth", "add", "work", "--token", "fake-token")
	mustRun(t, "file", "outline", fileKey)
	scopes := map[string]bool{}
	for _, row := range mustRun(t, "cache", "status").lines(t) {
		scopes[row["scope"].(string)] = true
	}
	if !scopes["work"] {
		t.Fatalf("expected the profile's partition: %v", scopes)
	}
	mustRun(t, "cache", "clear", "--all")
	if rows := mustRun(t, "cache", "status").lines(t); len(rows) != 0 {
		t.Fatalf("profile partition should be empty: %v", rows)
	}
	t.Setenv("AGENT_FIGMA_TOKEN", "fake-token")
	if rows := mustRun(t, "cache", "status").lines(t); len(rows) != 1 || rows[0]["scope"] != "env" {
		t.Fatalf("the env partition must survive clearing the profile's: %v", rows)
	}
}

func TestRefetchAfterEditAsksOnlyForTheNode(t *testing.T) {
	srv := mockEnv(t)
	mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCard)
	srv.BumpVersion(fileKey)
	mustRun(t, "--refresh", "nodes", "get", fileKey, "--node", mockfigma.NodeCard)
	reqs := srv.Stats().Requests
	last := reqs[len(reqs)-1]
	if last.Path != "/v1/files/"+fileKey || !strings.Contains(last.Query, "ids=") {
		t.Fatalf("refetch after an edit must name the node, got %s?%s", last.Path, last.Query)
	}
}

func TestSubtreeCompleteOnceChildrenAre(t *testing.T) {
	srv := mockEnv(t)
	// The outline lists 2:30's children completely, one level deep...
	mustRun(t, "file", "outline", fileKey, "--node", mockfigma.NodeCheckout, "--depth", "2")
	// ...and its only child's subtree is then cached in full,
	mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCardBody)
	// so 2:30 is complete too.
	before := srv.Stats().Tier1
	mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCard)
	if srv.Stats().Tier1 != before {
		t.Fatal("a node whose children are all cached should not be refetched")
	}
}

func TestInstancesFoldAndComponentsSummarise(t *testing.T) {
	mockEnv(t)
	r := mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout)
	recs := r.records(t)
	if _, ok := recs[mockfigma.NodeInstanceLabel]; ok {
		t.Fatal("instance insides should be folded by default")
	}
	if recs[mockfigma.NodeButtonInstance]["instance"] != true {
		t.Fatalf("folded instance: %v", recs[mockfigma.NodeButtonInstance])
	}
	uses, _ := r.meta(t, "@components").([]any)
	if len(uses) != 1 || uses[0].(map[string]any)["set"] != "Button" || uses[0].(map[string]any)["count"].(float64) != 2 {
		t.Fatalf("@components: %v", uses)
	}
	r = mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout, "--view", "components")
	// @file, one component record, @budget.
	if lines := r.lines(t); len(lines) != 3 || lines[1]["set"] != "Button" {
		t.Fatalf("--view components: %s", r.stdout)
	}
	r = mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout, "--expand-instances")
	if _, ok := r.records(t)[mockfigma.NodeInstanceLabel]; !ok {
		t.Fatal("--expand-instances should show insides")
	}
}

func TestHandoffWritesAFolderWithinBudget(t *testing.T) {
	srv := mockEnv(t)
	out := t.TempDir()
	r := mustRun(t, "handoff", fileLink(mockfigma.NodeCheckout), "--out", out)
	kinds := map[string]int{}
	for _, rec := range r.lines(t) {
		kind, _ := rec["kind"].(string)
		if kind == "" {
			continue
		}
		kinds[kind]++
		if path, _ := rec["path"].(string); path != "" {
			if info, err := os.Stat(path); err != nil || info.Size() == 0 {
				t.Fatalf("%s missing: %v", path, err)
			}
		}
	}
	for _, k := range []string{"screenshot", "icon", "image", "tree", "components", "styles", "manifest"} {
		if kinds[k] == 0 {
			t.Fatalf("handoff lacks %s: %v\n%s", k, kinds, r.stdout)
		}
	}
	stats := srv.Stats()
	if stats.Tier1 > 3 || stats.Tier2 > 2 {
		t.Fatalf("handoff budget: tier1=%d tier2=%d", stats.Tier1, stats.Tier2)
	}
	mustRun(t, "handoff", fileLink(mockfigma.NodeCheckout), "--out", t.TempDir())
	if after := srv.Stats(); after.Tier1 != stats.Tier1 || after.Tier2 != stats.Tier2 {
		t.Fatal("a repeated handoff should come from the cache")
	}
}

func TestDiffAfterAnEdit(t *testing.T) {
	srv := mockEnv(t)
	mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout)
	if _, err := srv.Edit(fileKey,
		mockfigma.Edit{Op: mockfigma.EditSet, Node: mockfigma.NodeTotalText, Field: "characters", Value: "Total: $50.00"},
		mockfigma.Edit{Op: mockfigma.EditRemove, Node: mockfigma.NodeHeroImage},
		mockfigma.Edit{Op: mockfigma.EditAdd, Parent: mockfigma.NodeCheckout, Index: mockfigma.AppendIndex, NodeJSON: json.RawMessage(`{"id":"9:1","type":"FRAME","name":"Promo","absoluteBoundingBox":{"x":24,"y":900,"width":327,"height":40},"children":[{"id":"9:2","type":"TEXT","name":"t","characters":"Sale"}]}`)},
	); err != nil {
		t.Fatal(err)
	}
	before := srv.Stats().Tier1
	r := mustRun(t, "diff", fileKey, "--node", mockfigma.NodeCheckout)
	if srv.Stats().Tier1 != before+1 {
		t.Fatalf("diff after an edit should cost one Tier 1 call, cost %d", srv.Stats().Tier1-before)
	}
	got := map[string]map[string]any{}
	for _, rec := range r.lines(t) {
		if c, ok := rec["change"].(string); ok {
			got[c+" "+rec["id"].(string)] = rec
		}
	}
	text := got["changed "+mockfigma.NodeTotalText]
	if text == nil || !strings.Contains(fmt.Sprint(text["fields"]), "$50.00") {
		t.Fatalf("text change missing: %s", r.stdout)
	}
	if got["removed "+mockfigma.NodeHeroImage] == nil || got["added 9:1"] == nil || got["added 9:2"] != nil {
		t.Fatalf("add/remove: %s", r.stdout)
	}
	sum, _ := r.meta(t, "@diff").(map[string]any)
	if sum["from"] != mockfigma.InitialVersion || sum["to"] == mockfigma.InitialVersion {
		t.Fatalf("@diff versions: %v", sum)
	}
	// Both versions are cached now: diffing again is free.
	mustRun(t, "diff", fileKey, "--node", mockfigma.NodeCheckout)
	if srv.Stats().Tier1 != before+1 {
		t.Fatal("a repeated diff should be answered from the cache")
	}
}

func TestDiffWithRenderAndExplicitFrom(t *testing.T) {
	srv := mockEnv(t)
	if _, err := srv.Edit(fileKey, mockfigma.Edit{Op: mockfigma.EditSet, Node: mockfigma.NodeStatusDot, Field: "fills", Value: []map[string]any{{"type": "SOLID", "color": map[string]any{"r": 1, "g": 0, "b": 0, "a": 1}}}}); err != nil {
		t.Fatal(err)
	}
	// Nothing cached: the baseline comes from --from via ?version=.
	r := mustRun(t, "diff", fileKey, "--node", mockfigma.NodeCard, "--from", mockfigma.InitialVersion, "--render")
	pairs := r.metaAll(t, "@render")
	if len(pairs) == 0 || pairs[0]["before"] == nil || pairs[0]["after"] == nil {
		t.Fatalf("expected before/after renders: %s", r.stdout)
	}
	if head, _ := r.meta(t, "@file").(map[string]any); head["version"] == mockfigma.InitialVersion {
		t.Fatalf("fetching an old version must not move the head: %v", head)
	}
	_ = srv
}

func TestDiffWithoutBaselineExplainsWhatToDo(t *testing.T) {
	mockEnv(t)
	r := run(t, "diff", fileKey, "--node", mockfigma.NodeCheckout)
	if r.code == 0 || !strings.Contains(r.errJSON(t)["hint"].(string), "--from") {
		t.Fatalf("no baseline: %s", r.stderr)
	}
}

func TestCompareMatchesAndLocatesDifferences(t *testing.T) {
	srv := mockEnv(t)
	r := mustRun(t, "render", fileKey, "--node", mockfigma.NodeCard, "--scale", "2", "--no-prefetch")
	screenshot := r.records(t)[mockfigma.NodeCard]["path"].(string)

	r = mustRun(t, "compare", fileKey, "--node", mockfigma.NodeCard, "--image", screenshot)
	rec := r.records(t)[mockfigma.NodeCard]
	if rec["match_percent"].(float64) != 100 || rec["scale"].(float64) != 2 {
		t.Fatalf("the design's own render should match at the inferred scale: %v", rec)
	}

	// The designer recolours the status dot; the app (the old screenshot) did not follow.
	if _, err := srv.Edit(fileKey, mockfigma.Edit{Op: mockfigma.EditSet, Node: mockfigma.NodeStatusDot, Field: "fills", Value: []map[string]any{{"type": "SOLID", "color": map[string]any{"r": 1, "g": 0, "b": 0, "a": 1}}}}); err != nil {
		t.Fatal(err)
	}
	before := srv.Stats().Tier1
	r = mustRun(t, "--refresh", "compare", fileKey, "--node", mockfigma.NodeCard, "--image", screenshot)
	rec = r.records(t)[mockfigma.NodeCard]
	regions, _ := rec["regions"].([]any)
	if rec["match_percent"].(float64) >= 100 || len(regions) == 0 {
		t.Fatalf("expected a difference: %v", rec)
	}
	hit, _ := regions[0].(map[string]any)["node"].(map[string]any)
	if hit["id"] != mockfigma.NodeStatusDot {
		t.Fatalf("difference should be attributed to the status dot: %v", regions[0])
	}
	if diffImg, _ := rec["diff_image"].(map[string]any); diffImg["path"] == nil {
		t.Fatalf("expected a diff image: %v", rec)
	}
	if spent := srv.Stats().Tier1 - before; spent > 2 {
		t.Fatalf("compare spent %d Tier 1 calls", spent)
	}
}

func TestSettingsNudges(t *testing.T) {
	srv := mockEnv(t)
	t.Setenv("AGENT_FIGMA_TOKEN", "")
	mustRun(t, "auth", "add", "work", "--token", "fake-token", "--seat", "full", "--plan", "starter")
	if err := srv.SetFaults("429:tier1:retry=7200:type=low:plan=org"); err != nil {
		t.Fatal(err)
	}
	r := run(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout)
	hint, _ := r.errJSON(t)["hint"].(string)
	if !strings.Contains(hint, "auth update work --seat view") || !strings.Contains(hint, "auth update work --plan org") {
		t.Fatalf("the 429 should suggest fixing the declarations: %s", hint)
	}
	r = mustRun(t, "auth", "check")
	if s, _ := r.lines(t)[0]["suggestions"].([]any); len(s) != 2 {
		t.Fatalf("auth check should list suggestions: %s", r.stdout)
	}
	mustRun(t, "auth", "update", "work", "--seat", "view", "--plan", "org")
	if r = mustRun(t, "auth", "list"); strings.Contains(r.stdout, "suggestions") {
		t.Fatalf("fixed declarations should clear suggestions: %s", r.stdout)
	}
}

func TestShortRateLimitPrintsNudgeAsNotice(t *testing.T) {
	srv := mockEnv(t)
	t.Setenv("AGENT_FIGMA_TOKEN", "")
	mustRun(t, "auth", "add", "work", "--token", "fake-token")
	if err := srv.SetFaults("429:tier3:retry=1:count=1:plan=pro"); err != nil {
		t.Fatal(err)
	}
	r := mustRun(t, "file", "meta", fileKey)
	if !strings.Contains(r.stderr, "auth update work --plan pro") {
		t.Fatalf("a waited-out 429 should still nudge: %s", r.stderr)
	}
}

func TestRevokedTokenBeforeExpiryNudges(t *testing.T) {
	mockEnv(t)
	t.Setenv("AGENT_FIGMA_TOKEN", "")
	future := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	mustRun(t, "auth", "add", "work", "--token", mockfigma.ExpiredToken, "--expires", future)
	r := run(t, "file", "meta", fileKey)
	if hint, _ := r.errJSON(t)["hint"].(string); !strings.Contains(hint, "auth update work --form --expires") {
		t.Fatalf("expected a re-auth nudge: %s", r.stderr)
	}
}

func TestGenericNamesGetLabels(t *testing.T) {
	mockEnv(t)
	r := mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout, "--view", "structure")
	card := r.records(t)[mockfigma.NodeCard]
	if card["label"] == nil || card["name"] != "Card" {
		t.Fatalf("a frame named Card should carry a label: %v", card)
	}
	label := card["label"].(string)
	r = mustRun(t, "render", fileKey, "--node", mockfigma.NodeCard, "--no-prefetch")
	if path := r.records(t)[mockfigma.NodeCard]["path"].(string); !strings.Contains(path, cache.Slug(label)+"--") {
		t.Fatalf("render file should be named by the label %q: %s", label, path)
	}
	r = mustRun(t, "nodes", "find", fileKey, "--name", strings.ToLower(label))
	if r.records(t)[mockfigma.NodeCard] == nil {
		t.Fatalf("find should match the label: %s", r.stdout)
	}
}

func TestComponentMapping(t *testing.T) {
	mockEnv(t)
	repo := t.TempDir()
	mapFile := filepath.Join(repo, ".agent-figma", "components.json")
	t.Setenv("AGENT_FIGMA_COMPONENTS", mapFile)

	// Without a mapping file, output is unannotated.
	r := mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout, "--view", "components")
	if strings.Contains(r.stdout, `"mapped"`) {
		t.Fatalf("no mapping file, no annotations: %s", r.stdout)
	}
	r = mustRun(t, "components", "map", "status", fileKey, "--node", mockfigma.NodeCheckout)
	if !strings.Contains(r.stdout, `"mapped":false`) || !strings.Contains(r.stdout, mockfigma.ButtonSetKey) {
		t.Fatalf("status should list the unmapped set with its key: %s", r.stdout)
	}

	// suggest finds the code component; --write saves exact matches.
	src := filepath.Join(repo, "src", "ui", "button.tsx")
	_ = os.MkdirAll(filepath.Dir(src), 0o755)
	_ = os.WriteFile(src, []byte("export function Button() { return null }\n"), 0o644)
	r = mustRun(t, "components", "map", "suggest", fileKey, "--node", mockfigma.NodeCheckout, "--src", repo, "--write")
	if !strings.Contains(r.stdout, `"match":"exact"`) {
		t.Fatalf("suggest: %s", r.stdout)
	}

	// add refines the entry with prop mappings.
	mustRun(t, "components", "map", "add", "--set-key", mockfigma.ButtonSetKey, "--name", "Button", "--component", "Button",
		"--import", "@/ui/button", "--prop", "Size=size", "--value", "Size.md=default", "--ignore", "State")
	r = mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeCheckout)
	uses, _ := r.meta(t, "@components").([]any)
	use := uses[0].(map[string]any)
	code, _ := use["code"].(map[string]any)
	if use["mapped"] != true || code["import"] != "@/ui/button" {
		t.Fatalf("@components should carry the mapping: %v", use)
	}
	props := use["variants"].([]any)[0].(map[string]any)["code_props"].(map[string]any)
	if props["size"] != "default" || props["State"] != nil {
		t.Fatalf("code props: %v", props)
	}
	inst := r.records(t)[mockfigma.NodeButtonInstance]
	if instCode, _ := inst["code"].(map[string]any); instCode["component"] != "Button" {
		t.Fatalf("instance records should carry the mapped component: %v", inst)
	}
}

func TestDesignerNotes(t *testing.T) {
	mockEnv(t)
	bySource := func(r result) map[string][]map[string]any {
		out := map[string][]map[string]any{}
		for _, rec := range r.lines(t) {
			if s, ok := rec["source"].(string); ok {
				out[s] = append(out[s], rec)
			}
		}
		return out
	}
	// Siblings unknown yet: comments and descriptions, plus a coverage warning.
	r := mustRun(t, "nodes", "notes", fileKey, "--node", mockfigma.NodeCheckout)
	notes := bySource(r)
	if len(notes["comment"]) == 0 || len(notes["description"]) == 0 || r.meta(t, "@notes_coverage") == nil {
		t.Fatalf("before an outline: %s", r.stdout)
	}
	mustRun(t, "file", "outline", fileKey)
	r = mustRun(t, "nodes", "notes", fileKey, "--node", mockfigma.NodeCheckout)
	notes = bySource(r)
	ids := map[string]string{}
	for _, n := range notes["canvas"] {
		ids[n["node_id"].(string)] = n["position"].(string)
	}
	if ids[mockfigma.NodeCheckoutNote] != "right" || ids[mockfigma.NodeStickyNote] != "above" {
		t.Fatalf("canvas notes beside Checkout: %v", notes["canvas"])
	}
	if _, far := ids[mockfigma.NodeFarText]; far || r.meta(t, "@notes_coverage") != nil {
		t.Fatalf("far text should be out of radius and coverage complete: %s", r.stdout)
	}
	r = mustRun(t, "nodes", "notes", fileKey, "--node", mockfigma.NodeConfirmation, "--no-comments")
	if sec := bySource(r)["section"]; len(sec) != 1 || sec[0]["node_id"] != mockfigma.NodeSectionNote {
		t.Fatalf("section text describes its frames: %s", r.stdout)
	}
	out := t.TempDir()
	mustRun(t, "handoff", fileKey, "--node", mockfigma.NodeCheckout, "--out", out)
	raw, err := os.ReadFile(filepath.Join(out, "notes.json"))
	if err != nil || !strings.Contains(string(raw), mockfigma.NodeStickyNote) {
		t.Fatalf("handoff notes.json: %v %s", err, raw)
	}
}

func TestMappingWritesRefusedUnderMCPAndBrokenMappingTolerated(t *testing.T) {
	mockEnv(t)
	repo := t.TempDir()
	mapFile := filepath.Join(repo, ".agent-figma", "components.json")
	t.Setenv("AGENT_FIGMA_COMPONENTS", mapFile)
	t.Setenv(ViaMCPEnv, "1")
	r := run(t, "components", "map", "add", "--set-key", mockfigma.ButtonSetKey, "--component", "Button")
	if r.code == 0 || r.errJSON(t)["fixable_by"] != "human" {
		t.Fatalf("map add under MCP should be refused: %s", r.stderr)
	}
	if _, err := os.Stat(mapFile); err == nil {
		t.Fatal("nothing should be written under MCP")
	}
	t.Setenv(ViaMCPEnv, "")

	_ = os.MkdirAll(filepath.Dir(mapFile), 0o755)
	_ = os.WriteFile(mapFile, []byte("{bad"), 0o644)
	r = mustRun(t, "nodes", "get", fileKey, "--node", mockfigma.NodeButtonInstance)
	if !strings.Contains(r.stderr, "Ignoring the component mapping") {
		t.Fatalf("a broken mapping should be a notice on reads: %s", r.stderr)
	}
	if r = run(t, "components", "map", "status", fileKey); r.code == 0 {
		t.Fatal("map commands should report a broken mapping")
	}

	_ = os.Remove(mapFile)
	mustRun(t, "components", "map", "add", "--set-key", mockfigma.ButtonSetKey, "--component", "Button")
	r = mustRun(t, "file", "outline", fileKey, "--node", mockfigma.NodeCheckout)
	if !strings.Contains(r.stdout, `"mapped":true`) {
		t.Fatalf("file outline should apply the mapping: %s", r.stdout)
	}
}
