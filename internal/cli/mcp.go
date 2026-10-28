package cli

import (
	"os"

	"github.com/shhac/lib-agent-cli/xdg"
	agentmcp "github.com/shhac/lib-agent-mcp"
	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/credential"
)

// cacheRootName is the MCP file root that downloaded images are read through.
const cacheRootName = "cache"

func cacheRoots() []agenterrors.FileRoot {
	return []agenterrors.FileRoot{xdg.Root(cacheRootName, cache.Root())}
}

// registerMCP exposes the agent-facing command groups as an MCP server. Added
// last so it reflects the complete tree. Credential, config, cache, and
// reference commands are not agent tasks and stay off the tool surface.
func registerMCP(root *cobra.Command) {
	skip := map[string]bool{"auth": true, "config": true, "usage": true, "cache": true, "completion": true, "help": true}
	for _, c := range root.Commands() {
		if !skip[c.Name()] && !c.Hidden {
			agentmcp.Expose(c)
		}
	}
	mcpCmd := agentmcp.Command(root,
		agentmcp.WithHiddenFlags("color", "token", "base-url"),
		agentmcp.WithFileRoots(cacheRoots()...),
		agentmcp.WithOAuthKeyringService(credential.MCPKeychainService()))
	serve := mcpCmd.RunE
	mcpCmd.RunE = func(cmd *cobra.Command, args []string) error {
		// Tool calls run as subprocesses that inherit this, so --out is refused
		// and manifests address files through the cache root.
		if err := os.Setenv(ViaMCPEnv, "1"); err != nil {
			return err
		}
		return serve(cmd, args)
	}
	root.AddCommand(mcpCmd)
}

// fileFields adds where a downloaded file is: a FileRef under the cache root
// (readable through the MCP fs tool), and the absolute path unless running
// under MCP, where host paths are not shown.
func fileFields(rec map[string]any, abs string) {
	if ref, ok := agenterrors.FileRefFor(cacheRoots(), abs); ok {
		rec["file"] = ref
	}
	if os.Getenv(ViaMCPEnv) == "" {
		rec["path"] = abs
	} else {
		delete(rec, "path")
	}
}
