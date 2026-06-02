package server

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
	"github.com/smart-mcp-proxy/mcpproxy-go/internal/secret"
	"github.com/smart-mcp-proxy/mcpproxy-go/internal/upstream"
)

// TestIsSelfHealableBuiltin documents which names self-heal when called with a
// server prefix: the gateway's own built-ins, but never the call_tool_* transport
// variants or genuine upstream tool names.
func TestIsSelfHealableBuiltin(t *testing.T) {
	builtins := []string{
		"retrieve_tools", "upstream_servers", "quarantine_security",
		"code_execution", "list_registries", "search_servers", "read_cache", "doctor",
	}
	for _, name := range builtins {
		assert.Truef(t, isSelfHealableBuiltin(name), "%q should be self-healable", name)
	}

	notHealable := []string{
		"call_tool_read", "call_tool_write", "call_tool_destructive", // transport, not target
		"list_licenses", "get_user", "create_issue", "", "unknown_tool",
	}
	for _, name := range notHealable {
		assert.Falsef(t, isSelfHealableBuiltin(name), "%q should NOT be self-healable", name)
	}
}

// TestSelfHealServerPrefixedBuiltin reproduces the bug: a host namespaces the
// gateway's built-in tools with the connection name, so the agent calls
// call_tool_read(name="<gateway>:list_registries"). Before the fix this returned
// "No client found for server: <gateway>"; now it runs the built-in.
func TestSelfHealServerPrefixedBuiltin(t *testing.T) {
	proxy := &MCPProxyServer{
		upstreamManager: upstream.NewManager(zap.NewNop(), config.DefaultConfig(), nil, secret.NewResolver(), nil),
		logger:          zap.NewNop(),
		config:          config.DefaultConfig(),
	}

	// "any-gateway" is not a configured upstream — it's the host's connection name.
	// For call_tool_read the upstream tool name is the "name" *argument*, while the
	// MCP tool name is "call_tool_read".
	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "call_tool_read",
			Arguments: map[string]interface{}{
				"name":          "any-gateway:list_registries",
				"intent_reason": "discovering registries",
			},
		},
	}

	result, err := proxy.handleCallToolRead(context.Background(), request)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.IsError, "self-healed built-in must not error")

	text := toolResultText(t, result)
	assert.Contains(t, text, "registries", "should return list_registries output")
	assert.NotContains(t, text, "No client found", "must not be routed to upstream")
}

// toolResultText extracts the first text content block from a tool result.
func toolResultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}
