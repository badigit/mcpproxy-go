package managed

import (
	"errors"
	"testing"
)

// Регрессия tvp-wm0z / infra-eaj: MCP-сессия умирает под ЖИВЫМ транспортом.
// Когда upstream-приложение (напр. Nuxt task_reporter) перезапускается за
// постоянным rathole-туннелем, TCP-слой остаётся поднятым — единственный признак
// протухшей сессии это прикладная ошибка "Server not initialized". Она обязана
// считаться connection error, иначе health-check не пометит state=Error и
// reconnect (со свежим MCP initialize) не запустится — upstream виснет в цикле
// "Failed to list tools ... Server not initialized" и не восстанавливается сам.
func TestIsConnectionError_StaleSessionNotInitialized(t *testing.T) {
	mc := &Client{}
	cases := []string{
		"CallTool failed for 'list_config': Bad Request: Server not initialized",
		"Bad Request: Server not initialized",
		"client not initialized",
	}
	for _, s := range cases {
		if !mc.isConnectionError(errors.New(s)) {
			t.Errorf("expected connection error for %q (stale MCP session under live tunnel)", s)
		}
	}
}

// Не должно ловить обычные прикладные ошибки, не связанные с мёртвой сессией.
func TestIsConnectionError_IgnoresPlainToolErrors(t *testing.T) {
	mc := &Client{}
	cases := []string{
		"invalid argument: id is required",
		"tool not found",
		"permission denied",
	}
	for _, s := range cases {
		if mc.isConnectionError(errors.New(s)) {
			t.Errorf("did not expect connection error for %q", s)
		}
	}
}
