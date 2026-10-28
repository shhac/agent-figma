package mockfigma

import (
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

const testToken = "fake-token"

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	s := New()
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	return s, ts
}

type response struct {
	status int
	header http.Header
	body   []byte
}

func (r response) json(t *testing.T) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(r.body, &out); err != nil {
		t.Fatalf("decode %q: %v", r.body, err)
	}
	return out
}

// do sends a request with the test token unless headers set their own auth.
func do(t *testing.T, method, url string, headers map[string]string) response {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, hasToken := headers["X-Figma-Token"]
	_, hasAuth := headers["Authorization"]
	if !hasToken && !hasAuth {
		req.Header.Set("X-Figma-Token", testToken)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: body}
}

func get(t *testing.T, ts *httptest.Server, path string) response {
	t.Helper()
	return do(t, http.MethodGet, ts.URL+path, nil)
}

func getOK(t *testing.T, ts *httptest.Server, path string) map[string]any {
	t.Helper()
	resp := get(t, ts, path)
	if resp.status != http.StatusOK {
		t.Fatalf("GET %s: status %d, body %s", path, resp.status, resp.body)
	}
	return resp.json(t)
}

func post(t *testing.T, ts *httptest.Server, path string) response {
	t.Helper()
	return do(t, http.MethodPost, ts.URL+path, nil)
}

func arm(t *testing.T, ts *httptest.Server, rules string) {
	t.Helper()
	resp := post(t, ts, "/_mock/faults?rules="+rules)
	if resp.status != http.StatusOK {
		t.Fatalf("arm %q: status %d, body %s", rules, resp.status, resp.body)
	}
}

// dig walks decoded JSON by object keys (string) and array indexes (int).
func dig(t *testing.T, v any, path ...any) any {
	t.Helper()
	for _, step := range path {
		switch key := step.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				t.Fatalf("dig %v: %T is not an object at %q", path, v, key)
			}
			v = m[key]
		case int:
			list, ok := v.([]any)
			if !ok || key >= len(list) {
				t.Fatalf("dig %v: no index %d in %T", path, key, v)
			}
			v = list[key]
		}
	}
	return v
}

func childIDs(t *testing.T, n any) []string {
	t.Helper()
	kids, ok := dig(t, n, "children").([]any)
	if !ok {
		t.Fatalf("node %v has no children list", dig(t, n, "id"))
	}
	ids := make([]string, 0, len(kids))
	for _, k := range kids {
		id, _ := dig(t, k, "id").(string)
		ids = append(ids, id)
	}
	return ids
}

func hasChildrenKey(n any) bool {
	m, _ := n.(map[string]any)
	_, ok := m["children"]
	return ok
}

// findNode searches a decoded subtree depth-first.
func findNode(n any, id string) map[string]any {
	m, _ := n.(map[string]any)
	if m == nil {
		return nil
	}
	if m["id"] == id {
		return m
	}
	kids, _ := m["children"].([]any)
	for _, k := range kids {
		if found := findNode(k, id); found != nil {
			return found
		}
	}
	return nil
}

func countNodes(n any) int {
	m, _ := n.(map[string]any)
	if m == nil {
		return 0
	}
	total := 1
	kids, _ := m["children"].([]any)
	for _, k := range kids {
		total += countNodes(k)
	}
	return total
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func keys(m any) []string {
	obj, _ := m.(map[string]any)
	return slices.Sorted(maps.Keys(obj))
}
