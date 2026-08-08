// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package skillgen

import (
	"testing"
)

func TestAllHosts_ReturnsCanonicalHostsInRenderOrder(t *testing.T) {
	t.Parallel()
	hosts := AllHosts()
	// The v1 three come first, in their original order, so an existing
	// baseline diff stays readable when the matrix grows (#4059).
	want := []Host{
		HostClaudeCode,
		HostCursor,
		HostCodex,
		HostCopilot,
		HostAider,
		HostGeminiCLI,
	}
	if len(hosts) != len(want) {
		t.Fatalf("AllHosts() = %v, want %v", hosts, want)
	}
	for i, h := range hosts {
		if h != want[i] {
			t.Errorf("AllHosts()[%d] = %q, want %q", i, h, want[i])
		}
	}
}

func TestAllHosts_OutputPathsAreUniqueAndHostScoped(t *testing.T) {
	t.Parallel()
	// Two hosts sharing an output path would silently overwrite each other
	// under expected/<host>/<path> only if the host id also collided, but a
	// duplicate path still signals a copy-paste adapter.
	seen := make(map[string]Host, len(AllHosts()))
	for _, h := range AllHosts() {
		a, err := AdapterFor(h)
		if err != nil {
			t.Fatalf("AdapterFor(%q) error = %v", h, err)
		}
		p := a.OutputPath()
		if prev, dup := seen[p]; dup {
			t.Errorf("hosts %q and %q both render to %q", prev, h, p)
		}
		seen[p] = h
	}
}

func TestAdapterFor_KnownHosts(t *testing.T) {
	t.Parallel()
	for _, h := range AllHosts() {
		a, err := AdapterFor(h)
		if err != nil {
			t.Errorf("AdapterFor(%q) error = %v", h, err)
			continue
		}
		if a.Host() != h {
			t.Errorf("AdapterFor(%q).Host() = %q, want %q", h, a.Host(), h)
		}
		if a.OutputPath() == "" {
			t.Errorf("AdapterFor(%q).OutputPath() is empty", h)
		}
	}
}

func TestAdapterFor_UnknownHost(t *testing.T) {
	t.Parallel()
	_, err := AdapterFor(Host("bogus"))
	if err == nil {
		t.Fatal("AdapterFor(bogus) error = nil, want unknown host error")
	}
}

func TestHostFromString_AcceptsCanonicalAndAliases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want Host
	}{
		{in: "claude-code", want: HostClaudeCode},
		{in: "Claude-Code", want: HostClaudeCode},
		{in: "claude_code", want: HostClaudeCode},
		{in: "cursor", want: HostCursor},
		{in: "CURSOR", want: HostCursor},
		{in: "codex", want: HostCodex},
		{in: "  codex  ", want: HostCodex},
		{in: "copilot", want: HostCopilot},
		{in: "Copilot", want: HostCopilot},
		{in: "aider", want: HostAider},
		{in: "gemini-cli", want: HostGeminiCLI},
		{in: "gemini_cli", want: HostGeminiCLI},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := HostFromString(tt.in)
			if err != nil {
				t.Fatalf("HostFromString(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("HostFromString(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestHostFromString_RejectsUnknown(t *testing.T) {
	t.Parallel()
	_, err := HostFromString("windsurf")
	if err == nil {
		t.Fatal("HostFromString(windsurf) error = nil, want unknown host error")
	}
}
