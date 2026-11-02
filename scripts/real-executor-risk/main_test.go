package main

import (
	"testing"
)

func TestToolNamesStayInsideTomlTableValues(t *testing.T) {
	for _, name := range []string{"node_repl", "server.with.dots", "plugin@registry", "quoted\"name"} {
		value, err := disabledToolSetOverride("mcp_servers", []string{name, "another-server"})
		if err != nil || len(value) < 13 || value[:13] != "mcp_servers={" {
			t.Fatal("tool identity entered the CLI dotted path", value, err)
		}
	}
	for _, name := range []string{"", "bad\nname", "bad\x01name"} {
		if _, err := disabledToolSetOverride("plugins", []string{name}); err == nil {
			t.Fatal("accepted an invalid tool name")
		}
	}
}
