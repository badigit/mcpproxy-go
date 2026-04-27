package server

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
	"github.com/smart-mcp-proxy/mcpproxy-go/internal/contracts"
	"github.com/smart-mcp-proxy/mcpproxy-go/internal/secret"
	"github.com/smart-mcp-proxy/mcpproxy-go/internal/upstream"
)

// newCallToolVariantTestProxy constructs an MCPProxyServer that exposes just
// enough state to drive handleCallToolVariant past intent/args parsing. The
// upstream manager has no clients registered, so any path that reaches the
// upstream lookup will hit the "no client found" branch — which is what we
// want for the regression cases.
func newCallToolVariantTestProxy() *MCPProxyServer {
	return &MCPProxyServer{
		upstreamManager: upstream.NewManager(zap.NewNop(), config.DefaultConfig(), nil, secret.NewResolver(), nil),
		logger:          zap.NewNop(),
		config:          &config.Config{},
	}
}

func toolErrorText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.NotNil(t, result)
	require.True(t, result.IsError, "expected error result")
	require.Len(t, result.Content, 1)
	textContent, ok := result.Content[0].(mcp.TextContent)
	require.True(t, ok, "expected TextContent")
	return textContent.Text
}

// TestHandleCallToolVariantRejectsBuiltinName verifies that when an agent
// passes a bare built-in tool name (e.g. "retrieve_tools") into
// call_tool_read/write/destructive, the response names the right call
// instead of the generic "expected server:tool" message. This is the
// fix for an observed agent failure mode where models retried call_tool_read
// with progressively wrong names because the error did not point at the
// real top-level MCP tool.
func TestHandleCallToolVariantRejectsBuiltinName(t *testing.T) {
	builtins := []string{
		"retrieve_tools",
		"upstream_servers",
		"quarantine_security",
		"read_cache",
		"code_execution",
		"search_servers",
		"list_registries",
		"call_tool_read",
		"call_tool_write",
		"call_tool_destructive",
	}

	proxy := newCallToolVariantTestProxy()
	ctx := context.Background()

	for _, name := range builtins {
		t.Run(name, func(t *testing.T) {
			request := mcp.CallToolRequest{}
			request.Params.Name = contracts.ToolVariantRead
			request.Params.Arguments = map[string]any{
				"name": name,
				"args": map[string]any{},
			}

			result, err := proxy.handleCallToolVariant(ctx, request, contracts.ToolVariantRead)
			require.NoError(t, err)
			errMsg := toolErrorText(t, result)
			assert.Contains(t, errMsg, "built-in mcpproxy tool",
				"error must explain that the name is a built-in tool")
			assert.Contains(t, errMsg, name,
				"error must echo the offending name so the agent can self-correct")
			assert.NotContains(t, errMsg, "expected server:tool",
				"the generic format error must not surface for built-in names")
		})
	}
}

// TestHandleCallToolVariantRejectsMcpproxyPrefixedBuiltin verifies that the
// "mcpproxy:retrieve_tools" shape (agent guesses a self-prefixed name) gets
// the same precise built-in error rather than the misleading
// "No client found for server: mcpproxy. Available servers: [...]" message.
func TestHandleCallToolVariantRejectsMcpproxyPrefixedBuiltin(t *testing.T) {
	proxy := newCallToolVariantTestProxy()
	ctx := context.Background()

	request := mcp.CallToolRequest{}
	request.Params.Name = contracts.ToolVariantRead
	request.Params.Arguments = map[string]any{
		"name": "mcpproxy:retrieve_tools",
		"args": map[string]any{"query": "obsidian search"},
	}

	result, err := proxy.handleCallToolVariant(ctx, request, contracts.ToolVariantRead)
	require.NoError(t, err)
	errMsg := toolErrorText(t, result)
	assert.Contains(t, errMsg, "built-in mcpproxy tool")
	assert.Contains(t, errMsg, "retrieve_tools")
	assert.NotContains(t, errMsg, "No client found for server",
		"the misleading upstream-not-found error must not surface for mcpproxy: prefix")
}

// TestHandleCallToolVariantUnknownBareNameKeepsLegacyError is a regression
// guard: a bare name that is NOT a built-in must still produce the existing
// "expected server:tool" error so other tooling that depends on that text
// keeps working.
func TestHandleCallToolVariantUnknownBareNameKeepsLegacyError(t *testing.T) {
	proxy := newCallToolVariantTestProxy()
	ctx := context.Background()

	request := mcp.CallToolRequest{}
	request.Params.Name = contracts.ToolVariantRead
	request.Params.Arguments = map[string]any{
		"name": "definitely_not_a_known_tool",
		"args": map[string]any{},
	}

	result, err := proxy.handleCallToolVariant(ctx, request, contracts.ToolVariantRead)
	require.NoError(t, err)
	errMsg := toolErrorText(t, result)
	assert.True(t, strings.Contains(errMsg, "expected server:tool"),
		"unknown bare names must still hit the legacy format error, got: %s", errMsg)
}

// TestHandleCallToolVariantValidUpstreamShapeBypassesBuiltinCheck is a
// regression guard: a well-formed server:tool name whose server is NOT the
// special "mcpproxy" prefix must skip the built-in early-rejection branch
// and continue down the upstream dispatch path. The bare proxy in this test
// lacks storage, so the call panics and is caught by the handler's
// recover() — what we assert is that the response is NOT the built-in
// error, proving our new check did not steal the request.
func TestHandleCallToolVariantValidUpstreamShapeBypassesBuiltinCheck(t *testing.T) {
	proxy := newCallToolVariantTestProxy()
	ctx := context.Background()

	request := mcp.CallToolRequest{}
	request.Params.Name = contracts.ToolVariantRead
	request.Params.Arguments = map[string]any{
		"name": "github:get_user",
		"args": map[string]any{},
	}

	result, err := proxy.handleCallToolVariant(ctx, request, contracts.ToolVariantRead)
	require.NoError(t, err)
	errMsg := toolErrorText(t, result)
	assert.NotContains(t, errMsg, "built-in mcpproxy tool",
		"upstream shape 'server:tool' must not be intercepted by the built-in check")
}

// TestIsBuiltinToolName documents the canonical set of names recognised as
// mcpproxy's own top-level MCP tools. If a future change adds or removes a
// built-in tool, update this test alongside builtinToolNames.
func TestIsBuiltinToolName(t *testing.T) {
	cases := map[string]bool{
		"retrieve_tools":          true,
		"upstream_servers":        true,
		"quarantine_security":     true,
		"read_cache":              true,
		"code_execution":          true,
		"search_servers":          true,
		"list_registries":         true,
		"call_tool":               true,
		"call_tool_read":          true,
		"call_tool_write":         true,
		"call_tool_destructive":   true,
		"github:get_user":         false,
		"":                        false,
		"mcpproxy:retrieve_tools": false, // prefixed form is handled separately
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, want, isBuiltinToolName(name))
		})
	}
}
