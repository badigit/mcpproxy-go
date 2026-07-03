package server

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/runtime/stateview"
)

// TestBuildDisconnectedServers is the core regression for the "phantom absence"
// bug: a configured server that exists but is currently offline contributes NO
// tools to the index or BM25 search, so a searching agent cannot see it and may
// wrongly conclude it does not exist. buildDisconnectedServers must surface such
// servers (enabled, not quarantined, not connected) with their last-known tool
// count, while hiding servers the operator deliberately turned off.
func TestBuildDisconnectedServers(t *testing.T) {
	now := time.Now()
	snapshot := &stateview.ServerStatusSnapshot{
		Timestamp: now,
		Servers: map[string]*stateview.ServerStatus{
			// Connected — has live tools in the index, not a blind spot.
			"b24": {
				Name:      "b24",
				Enabled:   true,
				Connected: true,
				State:     "connected",
				ToolCount: 83,
			},
			// Enabled but offline — THE blind spot. Must appear with last-known count.
			"task_reporter": {
				Name:      "task_reporter",
				Enabled:   true,
				Connected: false,
				State:     "error",
				ToolCount: 41,
				LastError: "dial tcp 127.0.0.1:9002: connect: connection refused",
			},
			// Operator disabled it — intentionally hidden.
			"old-server": {
				Name:      "old-server",
				Enabled:   false,
				Connected: false,
				State:     "idle",
				ToolCount: 5,
			},
			// Quarantined — intentionally hidden (security gate, not an outage).
			"suspicious": {
				Name:        "suspicious",
				Enabled:     true,
				Quarantined: true,
				Connected:   false,
				State:       "error",
				ToolCount:   3,
			},
		},
	}

	offline := buildDisconnectedServers(snapshot)

	require.Len(t, offline, 1, "only the enabled, non-quarantined, offline server should be listed")
	entry := offline[0]
	assert.Equal(t, "task_reporter", entry["server"])
	assert.Equal(t, "error", entry["state"])
	assert.Equal(t, 41, entry["tool_count"], "last-known tool count must survive disconnection")
	assert.Contains(t, entry["last_error"], "connection refused")
}

// TestBuildDisconnectedServers_NilSnapshot guards the handler path where the
// runtime/supervisor is not yet wired (returns a nil snapshot).
func TestBuildDisconnectedServers_NilSnapshot(t *testing.T) {
	assert.Nil(t, buildDisconnectedServers(nil))
}

// TestBuildDisconnectedServers_AllHealthy verifies the field is empty (nil) on a
// healthy fleet, so a normal response carries zero extra payload.
func TestBuildDisconnectedServers_AllHealthy(t *testing.T) {
	snapshot := &stateview.ServerStatusSnapshot{
		Servers: map[string]*stateview.ServerStatus{
			"a": {Name: "a", Enabled: true, Connected: true},
			"b": {Name: "b", Enabled: true, Connected: true},
		},
	}
	assert.Empty(t, buildDisconnectedServers(snapshot))
}

// TestBuildDisconnectedServers_Deterministic ensures a stable (sorted) order so
// output and tests do not flake on Go's randomized map iteration.
func TestBuildDisconnectedServers_Deterministic(t *testing.T) {
	snapshot := &stateview.ServerStatusSnapshot{
		Servers: map[string]*stateview.ServerStatus{
			"zeta":  {Name: "zeta", Enabled: true, Connected: false, State: "error"},
			"alpha": {Name: "alpha", Enabled: true, Connected: false, State: "error"},
			"mid":   {Name: "mid", Enabled: true, Connected: false, State: "error"},
		},
	}
	offline := buildDisconnectedServers(snapshot)
	require.Len(t, offline, 3)
	assert.Equal(t, "alpha", offline[0]["server"])
	assert.Equal(t, "mid", offline[1]["server"])
	assert.Equal(t, "zeta", offline[2]["server"])
}

// TestRetrieveTools_GuardrailAlwaysPresent asserts the epistemic guardrail is
// attached to usage_instructions on EVERY retrieve_tools response, so any agent
// using the proxy is told that a search miss is not proof of absence and how to
// enumerate ground truth.
func TestRetrieveTools_GuardrailAlwaysPresent(t *testing.T) {
	proxy := createTestMCPProxyServer(t)
	indexObsidianSearchNotes(t, proxy)

	// A query that DOES match — guardrail must still be present.
	resp := retrieveTools(t, proxy, map[string]interface{}{
		"query": "search notes",
	})
	instr, _ := resp["usage_instructions"].(string)
	assert.Contains(t, strings.ToLower(instr), "upstream_servers",
		"guardrail must point the agent at upstream_servers list")
	assert.Contains(t, instr, "disconnected_servers",
		"guardrail must reference the disconnected_servers field")

	// A query that matches nothing — guardrail must also be present.
	resp = retrieveTools(t, proxy, map[string]interface{}{
		"query": "zzzzz nonexistent gibberish query",
	})
	instr, _ = resp["usage_instructions"].(string)
	assert.Contains(t, strings.ToLower(instr), "upstream_servers",
		"guardrail must be present even on an empty result set")
}
