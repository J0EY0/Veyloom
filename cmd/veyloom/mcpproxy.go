package main

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/J0EY0/veyloom/internal/mcpproxy"
)

// newMCPProxyCmd builds `veyloom mcp-proxy`, the stdio MCP server an agent
// CLI spawns to reach the worker that started it. It is hidden because
// people never run it by hand: the worker writes the command line into
// the CLI's MCP configuration.
func newMCPProxyCmd() *cobra.Command {
	var endpoint string
	cmd := &cobra.Command{
		Use:    "mcp-proxy",
		Short:  "Serve MCP on stdio by forwarding to a worker's HTTP endpoint",
		Hidden: true,
		Args:   cobra.NoArgs,
		// No configuration is needed, and stdout belongs to the protocol,
		// so skip the root's config loading.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, _ []string) error {
			return mcpproxy.Run(cmd.Context(), endpoint, &mcp.StdioTransport{})
		},
	}
	cmd.Flags().StringVar(&endpoint, "url", "", "streamable HTTP MCP endpoint to forward to")
	_ = cmd.MarkFlagRequired("url")
	return cmd
}
