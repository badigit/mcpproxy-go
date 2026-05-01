package server

import (
	"sort"
	"strings"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
)

// serverListPlaceholder is the marker that buildInstructions substitutes with
// the auto-generated upstream server catalog when present in a custom
// instructions string. If absent, the catalog is appended at the end.
const serverListPlaceholder = "{{ServerList}}"

// defaultInstructionsTemplate is used when no custom instructions are
// configured. The {{ServerList}} placeholder is replaced with a one-line
// summary per enabled upstream server.
const defaultInstructionsTemplate = "This is mcpproxy-go, an MCP aggregator proxy that connects multiple upstream MCP servers. " +
	"WORKFLOW: Use 'retrieve_tools' to search for tools by description across all connected upstream servers. " +
	"Then call tools via 'call_tool_read', 'call_tool_write', or 'call_tool_destructive' based on the 'call_with' field in results. " +
	"Do NOT use 'search_servers' to find existing tools — it searches external registries for adding NEW servers only. " +
	"Use 'upstream_servers' with operation 'list' to see currently connected servers and their status." +
	"\n\n{{ServerList}}"

// buildInstructions assembles the MCP initialize-response instructions by
// merging a (possibly empty) custom template with an auto-generated catalog
// of enabled upstream servers. Substitution rules:
//   - custom == ""                  → use defaultInstructionsTemplate
//   - custom contains {{ServerList}} → placeholder replaced with catalog
//   - custom without placeholder    → catalog appended after a blank line
func buildInstructions(custom string, servers []*config.ServerConfig) string {
	template := custom
	if template == "" {
		template = defaultInstructionsTemplate
	}

	catalog := buildServerCatalog(servers)

	if strings.Contains(template, serverListPlaceholder) {
		return strings.ReplaceAll(template, serverListPlaceholder, catalog)
	}
	if catalog == "" {
		return template
	}
	return template + "\n\n" + catalog
}

// buildServerCatalog renders enabled upstream servers as a Markdown bullet
// list. Each line shows the server name, an optional description, and any
// search aliases or domain tags that would help an agent route requests.
// Returns "" when no enabled servers exist.
func buildServerCatalog(servers []*config.ServerConfig) string {
	enabled := make([]*config.ServerConfig, 0, len(servers))
	for _, s := range servers {
		if s != nil && s.Enabled {
			enabled = append(enabled, s)
		}
	}
	if len(enabled) == 0 {
		return ""
	}

	sort.Slice(enabled, func(i, j int) bool {
		return enabled[i].Name < enabled[j].Name
	})

	var b strings.Builder
	b.WriteString("Available upstream servers:\n")
	for _, s := range enabled {
		b.WriteString("- ")
		b.WriteString(s.Name)
		if s.Description != "" {
			b.WriteString(" — ")
			b.WriteString(s.Description)
		}
		if len(s.DomainTags) > 0 {
			b.WriteString(" [domains: ")
			b.WriteString(strings.Join(s.DomainTags, ", "))
			b.WriteString("]")
		}
		if len(s.SearchAliases) > 0 {
			b.WriteString(" (search: ")
			b.WriteString(strings.Join(s.SearchAliases, ", "))
			b.WriteString(")")
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
