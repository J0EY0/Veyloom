package runtime

import "encoding/json"

// The room tools reach Claude Code as an MCP server of the turn's own (see
// toolEndpoint), which the CLI starts from --mcp-config as the stdio proxy
// (internal/mcpproxy). Tools appear on the CLI command line as
// mcp__<server>__<tool>.
const claudeMCPServerName = toolServerName

// claudeToolName is how the CLI names one of the turn's MCP tools.
func claudeToolName(tool string) string {
	return "mcp__" + claudeMCPServerName + "__" + tool
}

// claudeMCPConfig is the document passed to --mcp-config: one stdio server
// that runs the proxy pointed at the turn's endpoint.
func claudeMCPConfig(proxy, endpoint string) string {
	command, args := mcpProxyServer(proxy, endpoint)
	cfg := map[string]any{
		"mcpServers": map[string]any{
			claudeMCPServerName: map[string]any{
				"type":    "stdio",
				"command": command,
				"args":    args,
			},
		},
	}
	data, _ := json.Marshal(cfg)
	return string(data)
}
