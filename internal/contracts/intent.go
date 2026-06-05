package contracts

import (
	"fmt"
	"strings"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
)

// Operation type constants for intent declaration
const (
	OperationTypeRead        = "read"
	OperationTypeWrite       = "write"
	OperationTypeDestructive = "destructive"
)

// ValidOperationTypes contains all valid operation_type values
var ValidOperationTypes = []string{
	OperationTypeRead,
	OperationTypeWrite,
	OperationTypeDestructive,
}

// Data sensitivity constants for intent declaration
const (
	DataSensitivityPublic   = "public"
	DataSensitivityInternal = "internal"
	DataSensitivityPrivate  = "private"
	DataSensitivityUnknown  = "unknown"
)

// ValidDataSensitivities contains all valid data_sensitivity values
var ValidDataSensitivities = []string{
	DataSensitivityPublic,
	DataSensitivityInternal,
	DataSensitivityPrivate,
	DataSensitivityUnknown,
}

// Tool variant constants for intent-based tool calling
const (
	ToolVariantRead        = "call_tool_read"
	ToolVariantWrite       = "call_tool_write"
	ToolVariantDestructive = "call_tool_destructive"
)

// ToolVariantToOperationType maps tool variants to their expected operation types
var ToolVariantToOperationType = map[string]string{
	ToolVariantRead:        OperationTypeRead,
	ToolVariantWrite:       OperationTypeWrite,
	ToolVariantDestructive: OperationTypeDestructive,
}

// MaxReasonLength is the maximum allowed length for the reason field
const MaxReasonLength = 1000

// IntentDeclaration represents the agent's declared intent for a tool call.
// The operation_type is automatically inferred from the tool variant used
// (call_tool_read/write/destructive), so agents only need to provide optional
// metadata fields for audit and compliance purposes.
type IntentDeclaration struct {
	// OperationType is automatically inferred from the tool variant.
	// Valid values: "read", "write", "destructive"
	// This field is populated by the server based on which tool variant is called.
	OperationType string `json:"operation_type"`

	// DataSensitivity is optional classification of data being accessed/modified.
	// Valid values: "public", "internal", "private", "unknown"
	// Default: "unknown" if not provided
	DataSensitivity string `json:"data_sensitivity,omitempty"`

	// Reason is optional human-readable explanation for the operation.
	// Max length: 1000 characters
	Reason string `json:"reason,omitempty"`
}

// IntentValidationError represents intent validation failures
type IntentValidationError struct {
	Code    string                 `json:"code"`                         // Error code for programmatic handling
	Message string                 `json:"message"`                      // Human-readable error message
	Details map[string]interface{} `json:"details" swaggertype:"object"` // Additional context
}

// Error codes for intent validation
const (
	IntentErrorCodeMissing              = "MISSING_INTENT"
	IntentErrorCodeMissingOperationType = "MISSING_OPERATION_TYPE"
	IntentErrorCodeInvalidOperationType = "INVALID_OPERATION_TYPE"
	IntentErrorCodeMismatch             = "INTENT_MISMATCH"
	IntentErrorCodeServerMismatch       = "SERVER_MISMATCH"
	IntentErrorCodeInvalidSensitivity   = "INVALID_SENSITIVITY"
	IntentErrorCodeReasonTooLong        = "REASON_TOO_LONG"
)

// Error implements the error interface
func (e *IntentValidationError) Error() string {
	return e.Message
}

// NewIntentValidationError creates a new IntentValidationError
func NewIntentValidationError(code, message string, details map[string]interface{}) *IntentValidationError {
	return &IntentValidationError{
		Code:    code,
		Message: message,
		Details: details,
	}
}

// Validate validates the IntentDeclaration optional fields.
// Note: operation_type is not validated here as it's inferred from tool variant.
func (i *IntentDeclaration) Validate() *IntentValidationError {
	// Check data_sensitivity if provided
	if i.DataSensitivity != "" && !isValidDataSensitivity(i.DataSensitivity) {
		return NewIntentValidationError(
			IntentErrorCodeInvalidSensitivity,
			fmt.Sprintf("Invalid intent.data_sensitivity '%s': must be public, internal, private, or unknown", i.DataSensitivity),
			map[string]interface{}{
				"provided":     i.DataSensitivity,
				"valid_values": ValidDataSensitivities,
			},
		)
	}

	// Check reason length
	if len(i.Reason) > MaxReasonLength {
		return NewIntentValidationError(
			IntentErrorCodeReasonTooLong,
			fmt.Sprintf("intent.reason exceeds maximum length of %d characters", MaxReasonLength),
			map[string]interface{}{
				"provided_length": len(i.Reason),
				"max_length":      MaxReasonLength,
			},
		)
	}

	return nil
}

// ValidateForToolVariant validates the intent and sets operation_type from tool variant.
// The operation_type is automatically inferred from the tool variant, so agents
// don't need to provide it explicitly.
func (i *IntentDeclaration) ValidateForToolVariant(toolVariant string) *IntentValidationError {
	// Get operation type for this tool variant
	opType, ok := ToolVariantToOperationType[toolVariant]
	if !ok {
		return NewIntentValidationError(
			IntentErrorCodeMismatch,
			fmt.Sprintf("Unknown tool variant: %s", toolVariant),
			map[string]interface{}{
				"tool_variant": toolVariant,
			},
		)
	}

	// Set operation_type from tool variant (inferring it)
	i.OperationType = opType

	// Validate the optional fields
	return i.Validate()
}

// ValidateAgainstServerAnnotations validates intent against server-provided annotations
func (i *IntentDeclaration) ValidateAgainstServerAnnotations(toolVariant, serverTool string, annotations *config.ToolAnnotations, strict bool) *IntentValidationError {
	// call_tool_destructive is the most permissive - skip server validation
	if toolVariant == ToolVariantDestructive {
		return nil
	}

	// No annotations means no server-side hints to validate against
	if annotations == nil {
		return nil
	}

	// Check for destructive tool being called with non-destructive variant
	if annotations.DestructiveHint != nil && *annotations.DestructiveHint {
		if toolVariant == ToolVariantRead || toolVariant == ToolVariantWrite {
			if strict {
				return NewIntentValidationError(
					IntentErrorCodeServerMismatch,
					fmt.Sprintf("Tool '%s' is marked destructive by server, use call_tool_destructive", serverTool),
					map[string]interface{}{
						"server_tool":      serverTool,
						"destructive_hint": true,
						"tool_variant":     toolVariant,
						"recommended":      ToolVariantDestructive,
					},
				)
			}
			// Non-strict mode: return nil but caller should log warning
		}
	}

	// Note: A write variant calling a read-only tool is allowed (informational mismatch)
	// The caller (server/mcp.go:validateIntentAgainstServer) may log a warning for this case

	return nil
}

// DeriveCallWith derives the recommended tool variant from server annotations.
// Defaults to call_tool_read as the safest option when intent is unclear.
// LLMs should analyze the tool description to override this default when appropriate.
//
// Priority:
//  1. destructiveHint=true → call_tool_destructive
//  2. readOnlyHint=false (explicitly NOT read-only) → call_tool_write
//  3. readOnlyHint=true → call_tool_read
//  4. No hints / nil annotations → call_tool_read (safe default)
func DeriveCallWith(annotations *config.ToolAnnotations) string {
	if annotations != nil {
		// Destructive takes highest priority
		if annotations.DestructiveHint != nil && *annotations.DestructiveHint {
			return ToolVariantDestructive
		}
		// Explicit readOnlyHint=false means server says it's NOT read-only
		if annotations.ReadOnlyHint != nil && !*annotations.ReadOnlyHint {
			return ToolVariantWrite
		}
		// Explicit readOnlyHint=true
		if annotations.ReadOnlyHint != nil && *annotations.ReadOnlyHint {
			return ToolVariantRead
		}
	}
	// No annotations, or annotations without any hints set:
	// Default to read as the safest option. Most tools are read-only
	// (search, query, list, get, fetch, check, view, find operations).
	// LLMs should analyze tool descriptions to select write/destructive when appropriate.
	return ToolVariantRead
}

// readVerbs are tool-name prefixes/verbs that indicate a read-only operation.
// Kept in sync with the "DECISION RULES BY TOOL NAME" guidance surfaced to agents
// in retrieve_tools usage instructions (server/mcp.go).
var readVerbs = map[string]struct{}{
	"search": {}, "query": {}, "list": {}, "get": {}, "fetch": {}, "find": {},
	"check": {}, "view": {}, "read": {}, "show": {}, "describe": {}, "lookup": {},
	"retrieve": {}, "browse": {}, "explore": {}, "discover": {}, "scan": {},
	"inspect": {}, "analyze": {}, "examine": {}, "validate": {}, "verify": {},
}

// writeVerbs are tool-name verbs that indicate a write (mutating, non-destructive) operation.
var writeVerbs = map[string]struct{}{
	"create": {}, "update": {}, "modify": {}, "add": {}, "set": {}, "send": {},
	"edit": {}, "change": {}, "write": {}, "post": {}, "put": {}, "patch": {},
	"insert": {}, "upload": {}, "submit": {}, "assign": {}, "configure": {},
	"enable": {}, "register": {}, "subscribe": {}, "publish": {}, "move": {},
	"copy": {}, "rename": {}, "merge": {},
}

// destructiveVerbs are tool-name verbs that indicate a destructive operation.
var destructiveVerbs = map[string]struct{}{
	"delete": {}, "remove": {}, "drop": {}, "revoke": {}, "disable": {},
	"destroy": {}, "purge": {}, "reset": {}, "clear": {}, "unsubscribe": {},
	"cancel": {}, "terminate": {}, "close": {}, "archive": {}, "ban": {},
	"block": {}, "disconnect": {}, "kill": {}, "wipe": {}, "truncate": {},
	"force": {}, "hard": {},
}

// ClassifyOperationByName infers the operation type (read/write/destructive) from a
// tool's name using a verb heuristic. It mirrors the same verb lists advertised to
// agents in the retrieve_tools usage instructions, so a tool classified here as
// "read" is the same one recommended for call_tool_read.
//
// The leading token of the tool name (split on '_', '-', or ':') is matched against
// the verb sets. Destructive and write verbs take priority over read; an unrecognized
// verb yields "read" as the safe default (most tools are read operations), matching
// DeriveCallWith's default.
func ClassifyOperationByName(toolName string) string {
	verb := leadingVerb(toolName)
	if verb == "" {
		return OperationTypeRead
	}
	if _, ok := destructiveVerbs[verb]; ok {
		return OperationTypeDestructive
	}
	if _, ok := writeVerbs[verb]; ok {
		return OperationTypeWrite
	}
	if _, ok := readVerbs[verb]; ok {
		return OperationTypeRead
	}
	// Unknown verb — default to read (safest, matches DeriveCallWith default).
	return OperationTypeRead
}

// leadingVerb extracts the lowercase leading token of a tool name. It strips an
// optional "server:" prefix, then takes the first segment split on '_' or '-'.
func leadingVerb(toolName string) string {
	name := toolName
	if idx := strings.LastIndex(name, ":"); idx >= 0 {
		name = name[idx+1:]
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	// Split on the first '_' or '-' to isolate the leading verb.
	for i, r := range name {
		if r == '_' || r == '-' {
			return name[:i]
		}
	}
	return name
}

// Content trust constants for open-world hint scanning (Spec 035)
const (
	// ContentTrustUntrusted marks tool output as untrusted (open-world tool, data from external sources)
	ContentTrustUntrusted = "untrusted"
	// ContentTrustTrusted marks tool output as trusted (closed-world tool, data from controlled sources)
	ContentTrustTrusted = "trusted"
)

// IsOpenWorldTool checks whether a tool's annotations indicate it operates in an
// open-world context (fetching data from external/untrusted sources). Per the MCP spec,
// openWorldHint defaults to true when nil or when annotations are nil entirely.
func IsOpenWorldTool(annotations *config.ToolAnnotations) bool {
	if annotations == nil {
		return true // spec default: openWorldHint=true
	}
	if annotations.OpenWorldHint == nil {
		return true // nil defaults to true per spec
	}
	return *annotations.OpenWorldHint
}

// ContentTrustForTool returns the content trust level based on tool annotations.
// Open-world tools return "untrusted", closed-world tools return "trusted".
func ContentTrustForTool(annotations *config.ToolAnnotations) string {
	if IsOpenWorldTool(annotations) {
		return ContentTrustUntrusted
	}
	return ContentTrustTrusted
}

// isValidDataSensitivity checks if the data sensitivity is valid
func isValidDataSensitivity(sensitivity string) bool {
	for _, valid := range ValidDataSensitivities {
		if strings.EqualFold(sensitivity, valid) {
			return sensitivity == valid // Case-sensitive match required
		}
	}
	return false
}

// ToMap converts IntentDeclaration to a map for storage in metadata
func (i *IntentDeclaration) ToMap() map[string]interface{} {
	m := map[string]interface{}{
		"operation_type": i.OperationType,
	}
	if i.DataSensitivity != "" {
		m["data_sensitivity"] = i.DataSensitivity
	}
	if i.Reason != "" {
		m["reason"] = i.Reason
	}
	return m
}

// IntentFromMap creates an IntentDeclaration from a map
func IntentFromMap(m map[string]interface{}) *IntentDeclaration {
	if m == nil {
		return nil
	}

	intent := &IntentDeclaration{}

	if opType, ok := m["operation_type"].(string); ok {
		intent.OperationType = opType
	}
	if sensitivity, ok := m["data_sensitivity"].(string); ok {
		intent.DataSensitivity = sensitivity
	}
	if reason, ok := m["reason"].(string); ok {
		intent.Reason = reason
	}

	return intent
}
