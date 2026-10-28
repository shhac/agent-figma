package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-figma/internal/ratelimit"
)

// Decode parses a Figma response body, reporting one it cannot read as a
// retryable error rather than as empty fields.
func Decode[T any](raw []byte, what string) (T, error) {
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, agenterrors.Wrap(err, agenterrors.FixableByRetry).WithHint("Figma returned an unexpected " + what + " response")
	}
	return v, nil
}

// HTTPError is the Cause of every mapped non-2xx response, so callers can
// branch on what Figma said (a too-large request, a failed render) without
// parsing the agent-facing message.
type HTTPError struct {
	Status  int
	Message string
	Path    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("Figma returned HTTP %d: %s", e.Status, e.Message)
}

// AsHTTPError finds the HTTPError behind err.
func AsHTTPError(err error) (*HTTPError, bool) {
	var he *HTTPError
	ok := errors.As(err, &he)
	return he, ok
}

// IsRequestTooLarge reports Figma's "Request too large" refusal, which means
// the same data must be asked for in smaller pieces.
func IsRequestTooLarge(err error) bool {
	he, ok := AsHTTPError(err)
	return ok && he.Status == http.StatusBadRequest && strings.Contains(strings.ToLower(he.Message), "too large")
}

// IsRenderFailure reports a render that timed out or failed server-side.
func IsRenderFailure(err error) bool {
	he, ok := AsHTTPError(err)
	if !ok {
		return false
	}
	msg := strings.ToLower(he.Message)
	return he.Status >= 500 || (he.Status == http.StatusBadRequest && (strings.Contains(msg, "timeout") || strings.Contains(msg, "render")))
}

// figmaMessage reads both error shapes Figma uses: {status, err} and
// {error: true, status, message}.
func figmaMessage(raw []byte) string {
	var body struct {
		Err     string `json:"err"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &body); err == nil {
		if body.Err != "" {
			return body.Err
		}
		if body.Message != "" {
			return body.Message
		}
	}
	text := strings.TrimSpace(string(raw))
	if len(text) > 300 || strings.HasPrefix(text, "<") {
		return ""
	}
	return text
}

func mapError(status int, raw []byte, path string) error {
	msg := figmaMessage(raw)
	if msg == "" {
		msg = http.StatusText(status)
	}
	cause := &HTTPError{Status: status, Message: msg, Path: path}
	lower := strings.ToLower(msg)
	var e *agenterrors.Error
	switch {
	case status == http.StatusForbidden && (strings.Contains(lower, "invalid token") || strings.Contains(lower, "expired")):
		e = agenterrors.New("Figma rejected the token: "+msg, agenterrors.FixableByHuman).
			WithHint("The token is invalid or expired. Generate a new one in Figma (Settings > Security > Personal access tokens) and run 'agent-figma auth update <profile> --form'.")
	case status == http.StatusForbidden && strings.Contains(lower, "scope"):
		e = agenterrors.New("The token is missing a scope for this request: "+msg, agenterrors.FixableByHuman).
			WithHint("Regenerate the token with the scope this endpoint needs; 'agent-figma usage' lists the recommended scopes.")
	case status == http.StatusForbidden && (strings.Contains(lower, "plan") || strings.Contains(lower, "account type")):
		e = agenterrors.New("Figma limits this endpoint by plan or seat: "+msg, agenterrors.FixableByHuman).
			WithHint("Variables endpoints need an Enterprise plan and a full member; use 'styles list' on other plans.")
	case status == http.StatusForbidden:
		e = agenterrors.New("Permission denied: "+msg, agenterrors.FixableByHuman).
			WithHint("Check the token can open this file in Figma, and that it has the needed scopes ('agent-figma auth check').")
	case status == http.StatusNotFound && libraryKeyPath.MatchString(path):
		e = agenterrors.New(notFound(msg), agenterrors.FixableByAgent).
			WithHint("Check the key with 'agent-figma components|component-sets|styles list <ref>'; keys are not node ids.")
	case status == http.StatusNotFound:
		e = agenterrors.New(notFound(msg), agenterrors.FixableByAgent).
			WithHint("Check the file key ('agent-figma resolve <url>') and that the token's account can open the file.")
	case status == http.StatusBadRequest && strings.Contains(lower, "too large"):
		e = agenterrors.New("Figma refused the request as too large", agenterrors.FixableByAgent).
			WithHint("Ask for a narrower subtree with --node, or a smaller --depth.")
	case status >= 500:
		e = agenterrors.New(fmt.Sprintf("Figma server error (%d): %s", status, msg), agenterrors.FixableByRetry)
	default:
		e = agenterrors.New(msg, agenterrors.FixableByStatus(status))
	}
	e.Cause = cause
	return e
}

var libraryKeyPath = regexp.MustCompile(`^/v1/(components|component_sets|styles)/`)

// notFound avoids "Not found: Not found" when Figma's message is just the status text.
func notFound(msg string) string {
	if strings.EqualFold(msg, http.StatusText(http.StatusNotFound)) {
		return "Not found"
	}
	return "Not found: " + msg
}

// limitError turns a refused or exhausted rate limit into the agent-facing
// error, naming what Figma reported about the budget.
func (c *Client) limitError(err error, fileKey string) error {
	var rle *ratelimit.RateLimitedError
	if !errors.As(err, &rle) {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return timedOut(err)
		}
		return agenterrors.Wrap(err, agenterrors.FixableByRetry)
	}
	fixable := agenterrors.FixableByRetry
	parts := []string{}
	if rle.Local {
		parts = append(parts, "No request was sent: an earlier 429 started a cooldown that other agent-figma processes share.")
	}
	if rle.RateLimitType == ratelimit.SeatLow {
		fixable = agenterrors.FixableByHuman
		parts = append(parts, "Figma reports a low (Collab/View seat) limit: Tier 1 calls are a small monthly allowance, so waiting may not help; a Full or Dev seat raises it.")
	}
	if rle.PlanTier != "" {
		parts = append(parts, "Plan tier: "+rle.PlanTier+".")
	}
	if rle.UpgradeLink != "" {
		parts = append(parts, "Upgrade: "+rle.UpgradeLink)
	}
	if fileKey != "" {
		parts = append(parts, "Data already cached for this file can still be read with --offline.")
	}
	e := agenterrors.New(rle.Error(), fixable).WithHint(strings.Join(parts, " ")).WithRetryAfter(rle.RetryAfter)
	e.Cause = rle
	return e
}
