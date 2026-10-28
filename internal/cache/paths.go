// Package cache stores what Figma returned, per profile and file version, so
// a Tier 1 call is spent once and answered from disk afterwards.
package cache

import (
	"os"
	"path/filepath"

	"github.com/shhac/lib-agent-cli/xdg"

	"github.com/shhac/agent-figma/internal/config"
)

// Root is agent-figma's cache directory.
func Root() string { return xdg.CacheDir(config.AppName) }

// ProfileDir is one profile's (or the env token's) cache partition. Callers
// pass validated profile names, so the name is safe as a path segment.
func ProfileDir(scope string) string { return filepath.Join(Root(), scope) }

// PurgeProfile removes everything cached for a profile.
func PurgeProfile(scope string) error { return os.RemoveAll(ProfileDir(scope)) }
