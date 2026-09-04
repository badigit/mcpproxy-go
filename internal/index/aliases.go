package index

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
	"github.com/smart-mcp-proxy/mcpproxy-go/internal/storage"
)

// CollectAliases merges keywords from a server's config (SearchAliases,
// DomainTags, ToolAliases[toolName]) with any cached LLM enrichment into
// a single space-separated string suitable for indexing in the bleve
// "aliases" field. toolName is the base tool name (without the server
// prefix). enriched may be nil when enrichment is disabled or not yet
// cached.
//
// Output is deterministic for deterministic input (dedup+stable-ish);
// duplicates are preserved to let matching terms re-boost the score.
// An empty output is returned when nothing is configured — callers MUST
// handle "" as "no aliases for this tool".
func CollectAliases(serverCfg *config.ServerConfig, toolName string, enriched *storage.EnrichedToolMeta) string {
	if serverCfg == nil && enriched == nil {
		return ""
	}

	var parts []string
	if serverCfg != nil {
		parts = append(parts, serverCfg.SearchAliases...)
		parts = append(parts, serverCfg.DomainTags...)
		if tl, ok := serverCfg.ToolAliases[toolName]; ok {
			parts = append(parts, tl...)
		}
	}
	if enriched != nil {
		parts = append(parts, enriched.Keywords...)
		parts = append(parts, enriched.ExampleQueries...)
		if enriched.Domain != "" {
			parts = append(parts, enriched.Domain)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

// ResolveDomain picks the single domain tag to advertise for a tool in
// retrieve_tools responses. Priority: LLM-enriched domain > first entry
// of server DomainTags > empty.
func ResolveDomain(serverCfg *config.ServerConfig, enriched *storage.EnrichedToolMeta) string {
	if enriched != nil && enriched.Domain != "" {
		return enriched.Domain
	}
	if serverCfg != nil && len(serverCfg.DomainTags) > 0 {
		return serverCfg.DomainTags[0]
	}
	return ""
}

// AliasHash returns a stable fingerprint of an aliases string as produced by
// CollectAliases. An empty aliases string hashes to "" (not to the sha256 of
// the empty input) so that "no aliases configured" is representable and
// comparable across index documents written before aliases existed.
//
// This hash intentionally lives beside the tool hash instead of inside it:
// the tool hash describes upstream data and drives the Spec 032 quarantine,
// so folding config-derived aliases into it would make an alias edit look
// like a rug pull.
func AliasHash(aliases string) string {
	if aliases == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(aliases))
	return hex.EncodeToString(sum[:])
}
