package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shhac/agent-figma/internal/output"
)

// isolate points every XDG dir at a temp dir and keeps tokens out of the
// keychain, so tests never see the developer's real profiles or cache.
func isolate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir+"/config")
	t.Setenv("XDG_CACHE_HOME", dir+"/cache")
	t.Setenv("XDG_STATE_HOME", dir+"/state")
	t.Setenv("AGENT_FIGMA_NO_KEYCHAIN", "1")
	for _, name := range []string{"AGENT_FIGMA_TOKEN", "FIGMA_TOKEN", "AGENT_FIGMA_PROFILE", "AGENT_FIGMA_BASE_URL"} {
		t.Setenv(name, "")
	}
}

type result struct {
	code   int
	stdout string
	stderr string
}

func run(t *testing.T, args ...string) result {
	t.Helper()
	var out, errBuf bytes.Buffer
	restore := output.SetWritersForTest(&out, &errBuf)
	defer restore()
	code := RunForTest("test", args)
	return result{code: code, stdout: out.String(), stderr: errBuf.String()}
}

// lines decodes NDJSON stdout.
func (r result) lines(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(r.stdout), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("stdout line is not JSON: %q", line)
		}
		out = append(out, m)
	}
	return out
}

func (r result) errJSON(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(lastLine(r.stderr))), &m); err != nil {
		t.Fatalf("stderr is not a JSON error: %q", r.stderr)
	}
	return m
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
