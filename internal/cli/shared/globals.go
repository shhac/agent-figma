// Package shared holds what every agent-figma command needs: the persistent
// flags, and resolving them into an authenticated, rate-limited API client.
package shared

import libcli "github.com/shhac/lib-agent-cli/cli"

// GlobalFlags carries the persistent flags. --format/--timeout/--debug live in
// the embedded libcli.Globals; the rest are agent-figma's.
type GlobalFlags struct {
	libcli.Globals

	Profile    string
	Token      string
	BaseURL    string
	MaxRetries int
	MaxWaitS   int
	Refresh    bool
	Offline    bool
	NoPrefetch bool
	// ComponentsFile overrides where the component-to-code mapping is read.
	ComponentsFile string
}
