package testutil

import (
	"encoding/json"
	"testing"
)

// TestBuildTestConfig_PathsSurviveJSON is the regression guard for the Windows
// binary-E2E breakage: the config used to be assembled with fmt.Sprintf into a
// JSON string literal, so a data_dir like `C:\Users\...` produced invalid JSON
// escapes, the binary exited with a config error, and the tests died on a 60s
// readiness timeout with no hint of the cause.
func TestBuildTestConfig_PathsSurviveJSON(t *testing.T) {
	tests := []struct {
		name    string
		dataDir string
	}{
		{name: "unix", dataDir: "/tmp/mcpproxy-test-123/data"},
		{name: "windows", dataDir: `C:\Users\Dee\AppData\Local\Temp\mcpproxy-test-123`},
		{name: "windows unc", dataDir: `\\server\share\mcpproxy`},
		{name: "quotes and tabs", dataDir: "/tmp/we\"ird\tpath"},
		{name: "non-ascii", dataDir: `C:\Users\Дмитрий\Temp\прокси`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := buildTestConfig(18080, tc.dataDir)
			if err != nil {
				t.Fatalf("buildTestConfig returned an error: %v", err)
			}

			var parsed map[string]interface{}
			if err := json.Unmarshal(raw, &parsed); err != nil {
				t.Fatalf("config is not valid JSON: %v\n%s", err, raw)
			}

			got, ok := parsed["data_dir"].(string)
			if !ok {
				t.Fatalf("data_dir missing or not a string: %#v", parsed["data_dir"])
			}
			if got != tc.dataDir {
				t.Fatalf("data_dir round-trip mismatch:\n want %q\n  got %q", tc.dataDir, got)
			}
		})
	}
}

// TestBuildTestConfig_KeepsRequiredFields pins the fields the binary tests rely
// on, so a future rewrite of the builder cannot silently drop them.
func TestBuildTestConfig_KeepsRequiredFields(t *testing.T) {
	raw, err := buildTestConfig(19090, t.TempDir())
	if err != nil {
		t.Fatalf("buildTestConfig returned an error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("config is not valid JSON: %v", err)
	}

	if listen, _ := parsed["listen"].(string); listen != ":19090" {
		t.Fatalf("listen = %q, want %q", listen, ":19090")
	}
	if key, _ := parsed["api_key"].(string); key != TestAPIKey {
		t.Fatalf("api_key = %q, want %q", key, TestAPIKey)
	}
	if q, _ := parsed["quarantine_enabled"].(bool); q {
		t.Fatal("quarantine_enabled must stay false, otherwise the memory server is blocked")
	}

	servers, ok := parsed["mcpServers"].([]interface{})
	if !ok || len(servers) != 1 {
		t.Fatalf("mcpServers = %#v, want exactly one entry", parsed["mcpServers"])
	}
	server, _ := servers[0].(map[string]interface{})
	if name, _ := server["name"].(string); name != "memory" {
		t.Fatalf("server name = %q, want %q", name, "memory")
	}
	if enabled, _ := server["enabled"].(bool); !enabled {
		t.Fatal("memory server must be enabled")
	}
}
