// Package figmaurl turns the ways people point at Figma content (share links,
// branch links, bare file keys, node ids in URL or API form) into the file key
// and node ids the REST API wants, without touching the network.
package figmaurl

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Ref is a parsed Figma reference.
type Ref struct {
	// FileKey is the main file's key.
	FileKey string `json:"file_key"`
	// BranchKey is set when the link points at a branch. The API accepts it
	// in place of the file key for reads.
	BranchKey string `json:"branch_key,omitempty"`
	// EditorPath is the URL's file-type segment (design, file, proto, board,
	// slides, deck, ...), empty for a bare key.
	EditorPath string   `json:"editor_path,omitempty"`
	NodeIDs    []string `json:"node_ids,omitempty"`
}

// Key is the key to send to the API: the branch key when there is one.
func (r Ref) Key() string {
	if r.BranchKey != "" {
		return r.BranchKey
	}
	return r.FileKey
}

// keyPattern is looser than Figma's documented 22-128 characters because the
// docs' own examples use shorter keys.
var keyPattern = regexp.MustCompile(`^[0-9A-Za-z]{10,128}$`)

var nodeIDPattern = regexp.MustCompile(`^I?\d+:\d+(;I?\d+:\d+)*$`)

// urlNodeID matches the hyphenated form links use ("12-34", "I12-34;5-6").
var urlNodeID = regexp.MustCompile(`(\d+)-(\d+)`)

// Parse accepts a figma.com link or a bare file key.
func Parse(input string) (Ref, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return Ref{}, fmt.Errorf("empty Figma reference")
	}
	if keyPattern.MatchString(input) {
		return Ref{FileKey: input}, nil
	}
	if !strings.Contains(input, "://") && strings.Contains(input, "figma.com/") {
		input = "https://" + input
	}
	u, err := url.Parse(input)
	if err != nil || u.Host == "" {
		return Ref{}, fmt.Errorf("%q is neither a Figma file key nor a figma.com link", input)
	}
	if !isFigmaHost(u.Hostname()) {
		return Ref{}, fmt.Errorf("%q is not a figma.com link", u.Hostname())
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) < 2 || !keyPattern.MatchString(segments[1]) {
		return Ref{}, fmt.Errorf("link path %q has no file key; expected figma.com/<type>/<file_key>", u.Path)
	}
	ref := Ref{EditorPath: segments[0], FileKey: segments[1]}
	if len(segments) >= 4 && segments[2] == "branch" && keyPattern.MatchString(segments[3]) {
		ref.BranchKey = segments[3]
	}
	for _, raw := range u.Query()["node-id"] {
		ids, err := ParseNodeIDs(raw)
		if err != nil {
			return Ref{}, err
		}
		ref.NodeIDs = appendUnique(ref.NodeIDs, ids...)
	}
	return ref, nil
}

func isFigmaHost(host string) bool {
	host = strings.ToLower(host)
	return host == "figma.com" || strings.HasSuffix(host, ".figma.com")
}

// ParseNodeIDs splits a comma-separated list of node ids, accepting the URL
// form ("1-2") and the API form ("1:2"), and returns them in API form.
func ParseNodeIDs(raw string) ([]string, error) {
	var ids []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := NormalizeNodeID(part)
		if err != nil {
			return nil, err
		}
		ids = appendUnique(ids, id)
	}
	return ids, nil
}

// NormalizeNodeID converts a node id to the colon form the API uses.
func NormalizeNodeID(id string) (string, error) {
	normalized := urlNodeID.ReplaceAllString(strings.TrimSpace(id), "$1:$2")
	if !nodeIDPattern.MatchString(normalized) {
		return "", fmt.Errorf("%q is not a Figma node id; expected forms like 1:2, 1-2, or I1:2;3:4", id)
	}
	return normalized, nil
}

func appendUnique(list []string, values ...string) []string {
	for _, v := range values {
		if !contains(list, v) {
			list = append(list, v)
		}
	}
	return list
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
