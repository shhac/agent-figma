package mockfigma

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/shhac/agent-figma/internal/ratelimit"
)

// Fault injection. Rules are comma-separated; each is colon-separated
// positional parts followed by key=value options. Node ids keep their colons
// because options are recognised by the "=". count defaults to unlimited.
//
//	429:<tier1|tier2|tier3|/path-prefix>[:retry=<s>][:type=low|high][:plan=<tier>][:count=<n>]
//	too-large:<path-prefix>[:min_nodes=<n>][:count=<n>]
//	render-timeout[:min_ids=<n>][:count=<n>]
//	render-500[:count=<n>]
//	null-render:<node-id>[:count=<n>]
//	status:<code>:<path-prefix>[:count=<n>]
//	plan-limited:variables[:count=<n>]
//	cdn-expired[:<path-prefix>][:count=<n>]
//
// Standing rules are armed with POST /_mock/faults?rules=...; an X-Mock-Fault
// header carries rules for that one request instead (its counts are tracked
// by rule text across requests).
type faultKind string

const (
	faultRateLimit     faultKind = "429"
	faultTooLarge      faultKind = "too-large"
	faultRenderTimeout faultKind = "render-timeout"
	faultRender500     faultKind = "render-500"
	faultNullRender    faultKind = "null-render"
	faultStatus        faultKind = "status"
	faultPlanLimited   faultKind = "plan-limited"
	faultCDNExpired    faultKind = "cdn-expired"
)

type fault struct {
	text      string
	key       string
	kind      faultKind
	target    string
	status    int
	retry     int
	limitType string
	plan      string
	minNodes  int
	minIDs    int
	count     int
}

type faultSpec struct {
	positional func([]string) bool
	options    []string
}

var faultSpecs = map[faultKind]faultSpec{
	faultRateLimit:     {exactly(1), []string{"retry", "type", "plan", "count"}},
	faultTooLarge:      {exactly(1), []string{"min_nodes", "count"}},
	faultRenderTimeout: {exactly(0), []string{"min_ids", "count"}},
	faultRender500:     {exactly(0), []string{"count"}},
	faultNullRender:    {func(p []string) bool { return len(p) >= 1 }, []string{"count"}},
	faultStatus:        {exactly(2), []string{"count"}},
	faultPlanLimited:   {func(p []string) bool { return len(p) == 1 && p[0] == "variables" }, []string{"count"}},
	faultCDNExpired:    {func(p []string) bool { return len(p) <= 1 }, []string{"count"}},
}

func exactly(n int) func([]string) bool {
	return func(p []string) bool { return len(p) == n }
}

var planTiers = map[string]bool{"enterprise": true, "org": true, "pro": true, "starter": true, "student": true}

func parseRules(spec string) ([]*fault, error) {
	out := []*fault{}
	for _, raw := range strings.Split(spec, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		f, err := parseRule(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func parseRule(raw string) (*fault, error) {
	parts := strings.Split(raw, ":")
	kind := faultKind(parts[0])
	if kind == "400-too-large" {
		kind = faultTooLarge
	}
	spec, ok := faultSpecs[kind]
	if !ok {
		return nil, fmt.Errorf("unknown fault kind %q in rule %q", parts[0], raw)
	}
	positional := []string{}
	options := map[string]string{}
	for _, p := range parts[1:] {
		if k, v, isOption := strings.Cut(p, "="); isOption {
			options[k] = v
			continue
		}
		positional = append(positional, p)
	}
	if !spec.positional(positional) {
		return nil, fmt.Errorf("fault rule %q has the wrong arguments", raw)
	}
	f := &fault{text: raw, kind: kind, retry: 1, limitType: ratelimit.SeatHigh, plan: "pro"}
	if err := f.applyOptions(spec.options, options); err != nil {
		return nil, fmt.Errorf("fault rule %q: %w", raw, err)
	}
	if err := f.applyPositional(positional); err != nil {
		return nil, fmt.Errorf("fault rule %q: %w", raw, err)
	}
	return f, nil
}

func (f *fault) applyPositional(positional []string) error {
	switch f.kind {
	case faultStatus:
		code, err := strconv.Atoi(positional[0])
		if err != nil || code < 400 || code > 599 {
			return fmt.Errorf("status must be 400-599, got %q", positional[0])
		}
		f.status = code
		f.target = positional[1]
	case faultNullRender:
		f.target = strings.Join(positional, ":")
	default:
		if len(positional) > 0 {
			f.target = positional[0]
		}
	}
	if f.kind == faultRateLimit && isTierName(f.target) {
		return nil
	}
	if f.kind == faultRateLimit || f.kind == faultTooLarge || f.kind == faultStatus {
		if !strings.HasPrefix(f.target, "/") {
			return fmt.Errorf("target %q must be a tier name or a path prefix starting with /", f.target)
		}
	}
	return nil
}

func (f *fault) applyOptions(allowed []string, options map[string]string) error {
	for k, v := range options {
		if !contains(allowed, k) {
			return fmt.Errorf("unknown option %q", k)
		}
		if err := f.setOption(k, v); err != nil {
			return err
		}
	}
	return nil
}

func (f *fault) setOption(k, v string) error {
	switch k {
	case "type":
		if v != ratelimit.SeatLow && v != ratelimit.SeatHigh {
			return fmt.Errorf("type must be low or high, got %q", v)
		}
		f.limitType = v
		return nil
	case "plan":
		if !planTiers[v] {
			return fmt.Errorf("unknown plan %q", v)
		}
		f.plan = v
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return fmt.Errorf("%s must be a non-negative integer, got %q", k, v)
	}
	switch k {
	case "retry":
		f.retry = n
	case "count":
		f.count = n
	case "min_nodes":
		f.minNodes = n
	case "min_ids":
		f.minIDs = n
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func isTierName(s string) bool {
	return s == ratelimit.Tier1.String() || s == ratelimit.Tier2.String() || s == ratelimit.Tier3.String()
}

// matchesPath is the target test for 429 and status rules.
func (f *fault) matchesPath(path string) bool {
	if isTierName(f.target) {
		return ratelimit.TierFor(path).String() == f.target
	}
	return strings.HasPrefix(path, f.target)
}

type faultSet struct {
	mu       sync.Mutex
	standing []*fault
	spec     string
	used     map[string]int
}

func newFaultSet() *faultSet {
	return &faultSet{standing: []*fault{}, used: map[string]int{}}
}

func (fs *faultSet) arm(spec string) error {
	rules, err := parseRules(spec)
	if err != nil {
		return err
	}
	for i, f := range rules {
		f.key = "standing:" + strconv.Itoa(i) + ":" + f.text
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.standing = rules
	fs.spec = spec
	fs.used = map[string]int{}
	return nil
}

func (fs *faultSet) clear() {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.standing = []*fault{}
	fs.spec = ""
	fs.used = map[string]int{}
}

func (fs *faultSet) describe() props {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	rules := make([]any, 0, len(fs.standing))
	for _, f := range fs.standing {
		rules = append(rules, props{"rule": f.text, "fired": fs.used[f.key], "count": f.count})
	}
	return props{"faults": fs.spec, "rules": rules}
}

// forRequest returns the rules that apply to r: its X-Mock-Fault header when
// present, otherwise the standing rules.
func (fs *faultSet) forRequest(r *http.Request) ([]*fault, error) {
	header := r.Header.Get("X-Mock-Fault")
	if header == "" {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		return fs.standing, nil
	}
	rules, err := parseRules(header)
	if err != nil {
		return nil, err
	}
	for _, f := range rules {
		f.key = "header:" + f.text
	}
	return rules, nil
}

// fire spends one use of f and reports whether it was still live.
func (fs *faultSet) fire(f *fault) bool {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if f.count > 0 && fs.used[f.key] >= f.count {
		return false
	}
	fs.used[f.key]++
	return true
}

// take returns the first rule of one of kinds that matches and is still live,
// spending a use of it.
func (fs *faultSet) take(rules []*fault, match func(*fault) bool, kinds ...faultKind) *fault {
	for _, f := range rules {
		if !containsKind(kinds, f.kind) || !match(f) {
			continue
		}
		if fs.fire(f) {
			return f
		}
	}
	return nil
}

func containsKind(kinds []faultKind, k faultKind) bool {
	for _, v := range kinds {
		if v == k {
			return true
		}
	}
	return false
}
