package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func meServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Figma-Token") != "fake-token" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"status":403,"err":"Invalid token"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"1","handle":"Example User","email":"user@example.com"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAuthLifecycle(t *testing.T) {
	isolate(t)
	srv := meServer(t)
	t.Setenv("AGENT_FIGMA_BASE_URL", srv.URL)

	expires := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	r := run(t, "auth", "add", "work", "--token", "fake-token", "--expires", expires, "--seat", "full")
	if r.code != 0 {
		t.Fatalf("auth add: %s", r.stderr)
	}
	added := r.lines(t)[0]
	if added["storage"] != "file" || added["default"] != true || strings.Contains(r.stdout, "fake-token") {
		t.Fatalf("auth add output: %s", r.stdout)
	}

	r = run(t, "auth", "check")
	if r.code != 0 {
		t.Fatalf("auth check: %s", r.stderr)
	}
	check := r.lines(t)[0]
	if check["user"].(map[string]any)["handle"] != "Example User" || check["expires_in_days"].(float64) != 3 {
		t.Fatalf("auth check output: %s", r.stdout)
	}
	if !strings.Contains(r.stderr, "expires in 3 day") {
		t.Fatalf("expected an expiry notice, got %q", r.stderr)
	}

	r = run(t, "auth", "update", "work", "--token", "wrong-token")
	if r.code != 0 {
		t.Fatalf("auth update: %s", r.stderr)
	}
	r = run(t, "auth", "check")
	if r.code == 0 || r.errJSON(t)["fixable_by"] != "human" {
		t.Fatalf("check with a bad token should be human-fixable: %s", r.stderr)
	}

	r = run(t, "auth", "list")
	if r.code != 0 || len(r.lines(t)) != 1 || strings.Contains(r.stdout, "token\":\"") {
		t.Fatalf("auth list: %s %s", r.stdout, r.stderr)
	}
	r = run(t, "auth", "remove", "work")
	if r.code != 0 {
		t.Fatalf("auth remove: %s", r.stderr)
	}
	r = run(t, "auth", "check")
	if r.code == 0 || r.errJSON(t)["fixable_by"] != "human" {
		t.Fatalf("check with no profile should ask a human for a token: %s", r.stderr)
	}
}

func TestAuthAddRejectsBadInput(t *testing.T) {
	isolate(t)
	for _, args := range [][]string{
		{"auth", "add", "../escape", "--token", "x"},
		{"auth", "add", "work", "--token", "x", "--seat", "admin"},
		{"auth", "add", "work", "--token", "x", "--expires", "next week"},
		{"auth", "add", "work"},
	} {
		r := run(t, args...)
		if r.code == 0 || r.errJSON(t)["fixable_by"] != "agent" {
			t.Errorf("%v: want an agent-fixable error, got %d %s", args, r.code, r.stderr)
		}
	}
}

func TestResolveAndTokenPrecedence(t *testing.T) {
	isolate(t)
	r := run(t, "resolve", "https://www.figma.com/design/FAKEFILEKEY0000000000000/Example?node-id=1-2")
	if r.code != 0 || r.lines(t)[0]["api_key"] != "FAKEFILEKEY0000000000000" {
		t.Fatalf("resolve: %s %s", r.stdout, r.stderr)
	}
	srv := meServer(t)
	t.Setenv("FIGMA_TOKEN", "fake-token")
	r = run(t, "--base-url", srv.URL, "api", "get", "/v1/me")
	if r.code != 0 || !strings.Contains(r.stdout, "Example User") {
		t.Fatalf("FIGMA_TOKEN should work with no profile: %s", r.stderr)
	}
}
