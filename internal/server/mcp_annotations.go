package server

import (
	"sort"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
	"github.com/smart-mcp-proxy/mcpproxy-go/internal/contracts"
	"github.com/smart-mcp-proxy/mcpproxy-go/internal/runtime/stateview"
)

// retrieveToolsGuardrail is appended to every retrieve_tools usage_instructions
// payload (all routing modes). It exists because retrieve_tools is a ranked
// keyword search over CONNECTED servers only: a low-ranked or offline tool is
// invisible, and agents were reading that invisibility as proof the tool/server
// does not exist. The guardrail tells the agent, in-band, that a miss is not
// absence and how to enumerate ground truth (upstream_servers list +
// disconnected_servers). Leading space so it appends cleanly after a period.
const retrieveToolsGuardrail = " A SEARCH MISS IS NOT PROOF OF ABSENCE: this is a ranked keyword search, and tools from disconnected or offline servers are omitted from the index entirely. Before telling the user that a tool or server does not exist, call the upstream_servers tool (operation: \"list\") to enumerate every configured server and its live connection state. Servers that exist but are currently unreachable are listed under 'disconnected_servers' in this response — their tools are temporarily unavailable, not gone."

// buildDisconnectedServers lists configured servers that EXIST but are currently
// unreachable, so a searching agent never mistakes "absent from the results" for
// "does not exist". A disconnected server contributes no tools to the index and
// no hits to the BM25 search, making it otherwise invisible to retrieve_tools.
//
// Only enabled, non-quarantined servers that are not connected are reported:
//   - disabled servers were turned off by the operator (deliberately hidden);
//   - quarantined servers are gated for security review, not an outage.
//
// ToolCount is the last-known cached count (survives disconnection, PR #635), so
// the agent sees "task_reporter, ~41 tools, currently offline" rather than nothing.
// Output is sorted by server name for deterministic responses/tests.
func buildDisconnectedServers(snapshot *stateview.ServerStatusSnapshot) []map[string]interface{} {
	if snapshot == nil {
		return nil
	}

	names := make([]string, 0, len(snapshot.Servers))
	for name := range snapshot.Servers {
		names = append(names, name)
	}
	sort.Strings(names)

	var offline []map[string]interface{}
	for _, name := range names {
		server := snapshot.Servers[name]
		if server == nil || !server.Enabled || server.Quarantined || server.Connected {
			continue
		}
		entry := map[string]interface{}{"server": name}
		if server.State != "" {
			entry["state"] = server.State
		}
		if server.ToolCount > 0 {
			entry["tool_count"] = server.ToolCount
		}
		if server.LastError != "" {
			entry["last_error"] = server.LastError
		}
		offline = append(offline, entry)
	}
	return offline
}

// SessionRisk holds the result of analyzing all connected servers' tool annotations
// for the "lethal trifecta" risk combination (Spec 035 F2).
type SessionRisk struct {
	Level          string `json:"level"`           // "high", "medium", "low"
	HasOpenWorld   bool   `json:"has_open_world"`  // Any tool with openWorldHint=true or nil
	HasDestructive bool   `json:"has_destructive"` // Any tool with destructiveHint=true or nil
	HasWrite       bool   `json:"has_write"`       // Any tool with readOnlyHint=false or nil
	LethalTrifecta bool   `json:"lethal_trifecta"` // All three categories present
	Warning        string `json:"warning,omitempty"`
}

// analyzeSessionRisk examines all connected servers' tool annotations to detect
// the "lethal trifecta" risk: open-world access + destructive capabilities + write access.
// Per MCP spec, nil annotation hints default to the most permissive interpretation:
//   - openWorldHint nil → true (assumes open world)
//   - destructiveHint nil → true (assumes destructive)
//   - readOnlyHint nil → false (assumes not read-only, i.e., can write)
func analyzeSessionRisk(snapshot *stateview.ServerStatusSnapshot) SessionRisk {
	var hasOpenWorld, hasDestructive, hasWrite bool

	for _, server := range snapshot.Servers {
		if !server.Connected {
			continue
		}

		for _, tool := range server.Tools {
			classifyToolRisk(tool.Annotations, &hasOpenWorld, &hasDestructive, &hasWrite)
		}
	}

	// Count how many risk categories are present
	riskCount := 0
	if hasOpenWorld {
		riskCount++
	}
	if hasDestructive {
		riskCount++
	}
	if hasWrite {
		riskCount++
	}

	risk := SessionRisk{
		HasOpenWorld:   hasOpenWorld,
		HasDestructive: hasDestructive,
		HasWrite:       hasWrite,
	}

	switch {
	case riskCount >= 3:
		risk.Level = "high"
		risk.LethalTrifecta = true
		risk.Warning = "LETHAL TRIFECTA DETECTED: This session combines open-world access, " +
			"destructive capabilities, and write access across connected servers. " +
			"A prompt injection attack could chain these to cause significant damage. " +
			"Consider using annotation filters (read_only_only, exclude_destructive, exclude_open_world) " +
			"to restrict tool discovery."
	case riskCount == 2:
		risk.Level = "medium"
	default:
		risk.Level = "low"
	}

	return risk
}

// classifyToolRisk updates the risk flags based on a single tool's annotations.
// Nil hints are treated as their MCP spec defaults (most permissive).
func classifyToolRisk(annotations *config.ToolAnnotations, hasOpenWorld, hasDestructive, hasWrite *bool) {
	if annotations == nil {
		// No annotations at all — apply MCP spec defaults (all permissive)
		*hasOpenWorld = true
		*hasDestructive = true
		*hasWrite = true
		return
	}

	// openWorldHint: nil or true → open world
	if annotations.OpenWorldHint == nil || *annotations.OpenWorldHint {
		*hasOpenWorld = true
	}

	// destructiveHint: nil or true → destructive
	if annotations.DestructiveHint == nil || *annotations.DestructiveHint {
		*hasDestructive = true
	}

	// readOnlyHint: nil or false → not read-only (write capable)
	if annotations.ReadOnlyHint == nil || !*annotations.ReadOnlyHint {
		*hasWrite = true
	}
}

// annotatedSearchResult pairs a search result with its resolved annotations
// for use in annotation-based filtering (Spec 035 F4).
type annotatedSearchResult struct {
	serverName  string
	toolName    string
	annotations *config.ToolAnnotations
	resultIndex int // Index into the original search results slice
}

// filterByAnnotations filters annotated search results based on annotation criteria.
// Returns only the results that pass all active filters.
//
// Filter semantics (per MCP spec, nil hints default to most permissive):
//   - readOnlyOnly: keep tools with readOnlyHint=true (explicit). When readOnlyHint
//     is ABSENT, fall back to the verb-based READ classification used to compute
//     call_with (search/read/list/get/... => read). Only tools explicitly annotated
//     readOnlyHint=false, or classified write/destructive by name, are excluded.
//   - excludeDestructive: exclude tools with destructiveHint=true or nil
//   - excludeOpenWorld: exclude tools with openWorldHint=true or nil
func filterByAnnotations(tools []annotatedSearchResult, readOnlyOnly, excludeDestructive, excludeOpenWorld bool) []annotatedSearchResult {
	// Fast path: no filters active
	if !readOnlyOnly && !excludeDestructive && !excludeOpenWorld {
		return tools
	}

	var filtered []annotatedSearchResult
	for _, tool := range tools {
		if shouldExclude(tool.toolName, tool.annotations, readOnlyOnly, excludeDestructive, excludeOpenWorld) {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

// shouldExclude returns true if a tool should be excluded based on its name,
// annotations and active filters.
func shouldExclude(toolName string, annotations *config.ToolAnnotations, readOnlyOnly, excludeDestructive, excludeOpenWorld bool) bool {
	if readOnlyOnly {
		if !isReadOnlyForFilter(toolName, annotations) {
			return true
		}
	}

	if excludeDestructive {
		// Exclude if destructiveHint is true or nil (default is true per spec).
		// However, a tool with readOnlyHint=true is inherently non-destructive,
		// so treat destructiveHint as false when readOnlyHint is explicitly true.
		isReadOnly := annotations != nil && annotations.ReadOnlyHint != nil && *annotations.ReadOnlyHint
		if !isReadOnly {
			if annotations == nil || annotations.DestructiveHint == nil || *annotations.DestructiveHint {
				return true
			}
		}
	}

	if excludeOpenWorld {
		// Exclude if openWorldHint is true or nil (default is true per spec)
		if annotations == nil || annotations.OpenWorldHint == nil || *annotations.OpenWorldHint {
			return true
		}
	}

	return false
}

// isReadOnlyForFilter decides whether a tool qualifies as read-only for the
// read_only_only discovery filter.
//
//   - Explicit readOnlyHint=true  => read-only (keep).
//   - Explicit readOnlyHint=false => NOT read-only (exclude), regardless of name.
//   - readOnlyHint absent (nil annotations, or annotations without the hint) =>
//     fall back to the verb-based name classifier (the same one behind call_with).
//     Only names classified as read are kept; write/destructive names are excluded.
//
// This resolves the inconsistency where retrieve_tools recommended call_tool_read
// for an unannotated tool yet read_only_only dropped that very tool (Bug:
// retrieve-tools-read-only-filter-drops-unannotated-tools).
func isReadOnlyForFilter(toolName string, annotations *config.ToolAnnotations) bool {
	if annotations != nil && annotations.ReadOnlyHint != nil {
		// Server provided an explicit hint — trust it.
		return *annotations.ReadOnlyHint
	}
	// No explicit readOnlyHint. An explicit destructiveHint=true still means the
	// tool mutates state, so it is not read-only regardless of its name.
	if annotations != nil && annotations.DestructiveHint != nil && *annotations.DestructiveHint {
		return false
	}
	// Otherwise defer to verb-based classification (the same heuristic behind
	// call_with): only names classified as read qualify as read-only.
	return contracts.ClassifyOperationByName(toolName) == contracts.OperationTypeRead
}
