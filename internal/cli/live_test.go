//go:build live

package cli

import (
	"os"
	"testing"
)

// TestLive reads a real file the runner's token can open. It needs
// FIGMA_TOKEN and AGENT_FIGMA_LIVE_URL, skips without them, spends at most
// three Tier 1 calls, and checks only the shape of what comes back.
//
//	FIGMA_TOKEN=... AGENT_FIGMA_LIVE_URL='https://www.figma.com/design/<key>/...?node-id=1-2' go test -tags live ./internal/cli -run TestLive -v
func TestLive(t *testing.T) {
	token, link := os.Getenv("FIGMA_TOKEN"), os.Getenv("AGENT_FIGMA_LIVE_URL")
	if token == "" || link == "" {
		t.Skip("set FIGMA_TOKEN and AGENT_FIGMA_LIVE_URL to run live tests")
	}
	isolate(t)
	t.Setenv("AGENT_FIGMA_TOKEN", token)

	check := mustRun(t, "auth", "check")
	if check.lines(t)[0]["status"] != "ok" {
		t.Fatalf("auth check: %s", check.stdout)
	}
	resolved := mustRun(t, "resolve", link).lines(t)[0]

	meta := mustRun(t, "file", "meta", link).lines(t)[0]
	if meta["version"] == "" || meta["name"] == "" {
		t.Fatalf("file meta lacks version or name: %v", meta)
	}

	outline := mustRun(t, "file", "outline", link, "--depth", "1")
	if len(outline.records(t)) == 0 {
		t.Fatal("outline returned no pages")
	}
	// A second outline must be answered from the cache.
	again := mustRun(t, "file", "outline", link, "--depth", "1")
	if b, _ := again.meta(t, "@budget").(map[string]any); b["calls"].(map[string]any)["tier1"].(float64) != 0 {
		t.Fatalf("repeated outline spent a Tier 1 call: %v", b)
	}

	nodes, _ := resolved["node_ids"].([]any)
	if len(nodes) == 0 {
		t.Log("link has no node-id; skipping node and render checks")
		return
	}
	got := mustRun(t, "nodes", "get", link, "--view", "structure", "--max-nodes", "50")
	if got.meta(t, "@file") == nil || len(got.records(t)) == 0 {
		t.Fatalf("nodes get: %s", got.stdout)
	}
	render := mustRun(t, "render", link, "--scale", "1", "--no-prefetch")
	for _, rec := range render.records(t) {
		if path, _ := rec["path"].(string); path != "" {
			if info, err := os.Stat(path); err != nil || info.Size() == 0 {
				t.Fatalf("rendered file missing: %v", rec)
			}
		}
	}
}
