package main

import (
	"bytes"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
)

func TestPrintMCPServerVersionFlagReturnsBeforeRuntimeStartup(t *testing.T) {
	original := buildinfo.Version
	buildinfo.Version = "v1.2.3-mcp"
	t.Cleanup(func() { buildinfo.Version = original })

	var stdout bytes.Buffer
	handled, err := printMCPServerVersionFlag([]string{"--version"}, &stdout)
	if err != nil {
		t.Fatalf("printMCPServerVersionFlag() error = %v, want nil", err)
	}
	if !handled {
		t.Fatal("printMCPServerVersionFlag() handled = false, want true")
	}
	if got, want := stdout.String(), "eshu-mcp-server v1.2.3-mcp\n"; got != want {
		t.Fatalf("printMCPServerVersionFlag() output = %q, want %q", got, want)
	}
}
