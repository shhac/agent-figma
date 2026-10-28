package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/mockfigma"
)

func main() {
	var addr string
	var routes bool

	cmd := &cobra.Command{
		Use:   "mockfigma",
		Short: "Local mock Figma REST API server for agent-figma tests",
		Long:  "Local mock Figma REST API server for agent-figma tests (synthetic data only).\n\nRoutes:\n" + routeHelp(),
		RunE: func(cmd *cobra.Command, args []string) error {
			if routes {
				for _, line := range mockfigma.Routes() {
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), line); err != nil {
						return err
					}
				}
				return nil
			}
			server := &http.Server{
				Addr:    addr,
				Handler: mockfigma.New(),
			}
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
				"status":   "listening",
				"base_url": "http://" + addr,
				"file_key": mockfigma.FileKey,
			})
			return server.ListenAndServe()
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:12112", "Address to listen on")
	cmd.Flags().BoolVar(&routes, "routes", false, "Print mock route map and exit")

	if err := cmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func routeHelp() string {
	var b strings.Builder
	for _, line := range mockfigma.Routes() {
		b.WriteString("  " + line + "\n")
	}
	return b.String()
}
