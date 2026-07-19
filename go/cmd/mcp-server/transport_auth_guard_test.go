// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// TestRequireMCPHTTPCredentialSource proves the "no silent open mode over
// HTTP" gate (issue #5168): ESHU_MCP_TRANSPORT=http with no resolvable
// credential source refuses to start unless the explicit
// ESHU_MCP_ALLOW_UNAUTHENTICATED escape hatch is set, and stdio is never
// gated regardless of credential configuration.
func TestRequireMCPHTTPCredentialSource(t *testing.T) {
	tests := []struct {
		name                 string
		transport            string
		credentialSource     bool
		allowUnauthenticated bool
		wantErr              bool
	}{
		{
			name:             "http with credential source starts clean",
			transport:        "http",
			credentialSource: true,
			wantErr:          false,
		},
		{
			name:             "http with no credential source and no escape hatch refuses to start",
			transport:        "http",
			credentialSource: false,
			wantErr:          true,
		},
		{
			name:                 "http with no credential source but escape hatch starts",
			transport:            "http",
			credentialSource:     false,
			allowUnauthenticated: true,
			wantErr:              false,
		},
		{
			name:                 "http with credential source and escape hatch also set starts clean",
			transport:            "http",
			credentialSource:     true,
			allowUnauthenticated: true,
			wantErr:              false,
		},
		{
			name:             "stdio with no credential source is never gated",
			transport:        "stdio",
			credentialSource: false,
			wantErr:          false,
		},
		{
			name:             "stdio with credential source is never gated",
			transport:        "stdio",
			credentialSource: true,
			wantErr:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wiring := mcpAuthWiring{credentialSourceConfigured: tt.credentialSource}
			err := requireMCPHTTPCredentialSource(tt.transport, wiring, tt.allowUnauthenticated)
			if tt.wantErr && err == nil {
				t.Fatal("requireMCPHTTPCredentialSource() error = nil, want non-nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("requireMCPHTTPCredentialSource() error = %v, want nil", err)
			}
		})
	}
}

// TestRequireMCPHTTPCredentialSourceErrorIsActionable proves the refusal
// error names the concrete knobs an operator can set, not just "denied".
func TestRequireMCPHTTPCredentialSourceErrorIsActionable(t *testing.T) {
	err := requireMCPHTTPCredentialSource("http", mcpAuthWiring{credentialSourceConfigured: false}, false)
	if err == nil {
		t.Fatal("requireMCPHTTPCredentialSource() error = nil, want non-nil")
	}
	for _, want := range []string{"ESHU_API_KEY", "ESHU_SCOPED_TOKENS_FILE", "ESHU_AUTH_RESOURCE_URI", "ESHU_MCP_ALLOW_UNAUTHENTICATED"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("requireMCPHTTPCredentialSource() error = %q, want it to mention %q", err.Error(), want)
		}
	}
}
