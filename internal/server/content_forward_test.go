package server

import (
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/truncate"
)

// TestForwardContentResult_PreservesImageContent verifies that an ImageContent
// block from upstream is forwarded unchanged to the downstream client.
// Regression test for issue #368.
func TestForwardContentResult_PreservesImageContent(t *testing.T) {
	upstream := &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent("Here is your image:"),
			mcp.NewImageContent("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwC", "image/png"),
		},
	}
	truncator := truncate.NewTruncator(0) // disabled

	forwarded, text, truncated := forwardContentResult(upstream, truncator, nil, "test:tool", nil)

	require.NotNil(t, forwarded)
	require.Equal(t, 2, len(forwarded.Content), "both content blocks must be forwarded")
	assert.False(t, truncated)

	// First block: text preserved
	tc, ok := forwarded.Content[0].(mcp.TextContent)
	require.True(t, ok, "block 0 should remain TextContent")
	assert.Equal(t, "Here is your image:", tc.Text)

	// Second block: image preserved as native type
	ic, ok := forwarded.Content[1].(mcp.ImageContent)
	require.True(t, ok, "block 1 should remain ImageContent (not serialized to text)")
	assert.Equal(t, "image/png", ic.MIMEType)
	assert.Equal(t, "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwC", ic.Data)

	// Text representation used for logging should reference both blocks
	assert.Contains(t, text, "Here is your image:")
	assert.Contains(t, text, "[image:image/png")
}

// TestForwardContentResult_TruncatesOnlyText verifies that truncation applies
// to TextContent but leaves ImageContent and AudioContent untouched regardless
// of their size.
func TestForwardContentResult_TruncatesOnlyText(t *testing.T) {
	// Build a very large base64 payload to show it survives truncation
	bigData := strings.Repeat("A", 10000)
	bigText := strings.Repeat("x", 2000)

	upstream := &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(bigText),
			mcp.NewImageContent(bigData, "image/png"),
			mcp.NewAudioContent(bigData, "audio/wav"),
		},
	}
	// Truncator with a 500-char limit
	truncator := truncate.NewTruncator(500)

	forwarded, _, truncated := forwardContentResult(upstream, truncator, nil, "test:tool", nil)
	require.NotNil(t, forwarded)
	require.Equal(t, 3, len(forwarded.Content))
	assert.True(t, truncated, "text block should be marked as truncated")

	// Text was truncated
	tc, ok := forwarded.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Less(t, len(tc.Text), len(bigText), "text block should be shorter after truncation")

	// Image unchanged
	ic, ok := forwarded.Content[1].(mcp.ImageContent)
	require.True(t, ok)
	assert.Equal(t, bigData, ic.Data, "image data must be forwarded byte-for-byte")

	// Audio unchanged
	ac, ok := forwarded.Content[2].(mcp.AudioContent)
	require.True(t, ok)
	assert.Equal(t, bigData, ac.Data, "audio data must be forwarded byte-for-byte")
}

// TestForwardContentResult_TextOnlyNoTruncation exercises the common case of a
// small text-only response. Verifies the result is forwarded unchanged.
func TestForwardContentResult_TextOnlyNoTruncation(t *testing.T) {
	upstream := &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent("small result"),
		},
	}
	truncator := truncate.NewTruncator(0)

	forwarded, text, truncated := forwardContentResult(upstream, truncator, nil, "test:tool", nil)
	require.NotNil(t, forwarded)
	require.Equal(t, 1, len(forwarded.Content))
	assert.False(t, truncated)
	assert.Equal(t, "small result", text)

	tc, ok := forwarded.Content[0].(mcp.TextContent)
	require.True(t, ok)
	assert.Equal(t, "small result", tc.Text)
}

type fakeCacheStorer struct {
	key, toolName, content, recordPath string
	totalRecords                       int
	calls                              int
}

func (f *fakeCacheStorer) Store(key, toolName string, _ map[string]interface{}, content, recordPath string, totalRecords int) error {
	f.calls++
	f.key = key
	f.toolName = toolName
	f.content = content
	f.recordPath = recordPath
	f.totalRecords = totalRecords
	return nil
}

// TestForwardContentResult_StoresTruncatedInCache locks in the fix for the bug
// where Truncate advertised a cache_key but no one wrote to the cache, so the
// follow-up read_cache call always returned "cache key not found".
func TestForwardContentResult_StoresTruncatedInCache(t *testing.T) {
	// Build a JSON array big enough to be record-split by the truncator.
	var sb strings.Builder
	sb.WriteString(`{"items":[`)
	for i := 0; i < 20; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`{"v":"` + strings.Repeat("x", 200) + `"}`)
	}
	sb.WriteString(`]}`)
	original := sb.String()

	upstream := &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent(original)},
	}
	cache := &fakeCacheStorer{}
	truncator := truncate.NewTruncator(500)

	forwarded, _, truncated := forwardContentResult(upstream, truncator, cache, "remna:config_profile_get", nil)
	require.NotNil(t, forwarded)
	require.True(t, truncated)
	require.Equal(t, 1, cache.calls, "cache.Store must be called exactly once for a truncated response")
	assert.NotEmpty(t, cache.key, "cache key must be propagated to Store")
	assert.Equal(t, original, cache.content, "Store must receive the original pre-truncation content")
	assert.Equal(t, "remna:config_profile_get", cache.toolName)
	assert.Greater(t, cache.totalRecords, 0)

	// The advertised key in the truncated text must match what was stored, so
	// a downstream read_cache(key=...) call can find it.
	tc := forwarded.Content[0].(mcp.TextContent)
	assert.Contains(t, tc.Text, cache.key, "advertised cache_key must match what Store received")
}

// TestForwardContentResult_NilCacheNoPanic verifies that callers without a
// cache (legacy paths, tests) still work — Store is simply skipped.
func TestForwardContentResult_NilCacheNoPanic(t *testing.T) {
	bigText := strings.Repeat(`{"a":1},`, 200)
	upstream := &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("[" + bigText + "{}]")}}
	forwarded, _, truncated := forwardContentResult(upstream, truncate.NewTruncator(200), nil, "t", nil)
	require.NotNil(t, forwarded)
	assert.True(t, truncated)
}

// TestForwardContentResult_Fallback verifies that if result is not a
// *mcp.CallToolResult (e.g., nil or some other interface value), the function
// falls back to legacy JSON-wrapping behavior without panicking.
func TestForwardContentResult_Fallback(t *testing.T) {
	// Case 1: nil — should not panic, returns a JSON "null" text wrapper
	forwarded, _, _ := forwardContentResult(nil, truncate.NewTruncator(0), nil, "t", nil)
	require.NotNil(t, forwarded)
	require.Equal(t, 1, len(forwarded.Content))

	// Case 2: a plain map — legacy JSON marshal path
	forwarded, text, _ := forwardContentResult(map[string]string{"key": "value"}, truncate.NewTruncator(0), nil, "t", nil)
	require.NotNil(t, forwarded)
	require.Equal(t, 1, len(forwarded.Content))
	assert.Contains(t, text, "key")
	assert.Contains(t, text, "value")
}
