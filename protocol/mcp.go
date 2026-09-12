package protocol

// McpServerConfig is the process-transport union for an MCP server passed via
// --mcp-config. The Type field selects the transport: "stdio" | "sse" |
// "http" | "sdk". Only the fields relevant to the selected type need be set.
type McpServerConfig struct {
	Type    string            `json:"type"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Timeout *int              `json:"timeout,omitempty"`
	Name    string            `json:"name,omitempty"` // sdk type only
}

// NewMcpStdioConfig builds a stdio MCP server config.
func NewMcpStdioConfig(command string, args []string, env map[string]string) McpServerConfig {
	return McpServerConfig{Type: "stdio", Command: command, Args: args, Env: env}
}

// NewMcpHTTPConfig builds a streamable-HTTP MCP server config.
func NewMcpHTTPConfig(url string, headers map[string]string) McpServerConfig {
	return McpServerConfig{Type: "http", URL: url, Headers: headers}
}

// NewMcpSSEConfig builds an SSE MCP server config.
func NewMcpSSEConfig(url string, headers map[string]string) McpServerConfig {
	return McpServerConfig{Type: "sse", URL: url, Headers: headers}
}

// NewMcpSdkConfig references an in-process SDK MCP server by name. The name
// must also be declared via InitializeRequest.SdkMcpServers; tool calls are
// proxied back through mcp_message control requests.
func NewMcpSdkConfig(name string) McpServerConfig {
	return McpServerConfig{Type: "sdk", Name: name}
}
