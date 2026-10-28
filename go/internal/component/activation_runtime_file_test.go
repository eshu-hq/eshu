// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRuntimeFile(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "activation.yaml")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write activation config: %v", err)
	}
	return path
}

// TestLoadActivationRuntimeFileReadsHostAndConfigBlock proves one read yields
// both the host claim and the config block, re-encoded as the JSON object the
// worker hands the component (a YAML mapping decoded into map[string]any).
func TestLoadActivationRuntimeFileReadsHostAndConfigBlock(t *testing.T) {
	t.Parallel()

	path := writeRuntimeFile(t, `host:
  sourceSystem: vulnerability_intelligence
  scope:
    id: vuln-intel://cisa/kev
    kind: vulnerability_intelligence
process:
  command: ./collector
config:
  derive_from_owned_packages:
    enabled: true
    sources: [osv]
    target_limit: 1000
  source_cache:
    directory: /var/cache/vi
`)
	file, err := LoadActivationRuntimeFile(path)
	if err != nil {
		t.Fatalf("LoadActivationRuntimeFile() error = %v, want nil", err)
	}
	if !file.HasHost || file.Host.Scope.ID != "vuln-intel://cisa/kev" {
		t.Fatalf("host = %+v (has %v), want the KEV host scope", file.Host, file.HasHost)
	}
	block, err := file.ConfigJSON()
	if err != nil {
		t.Fatalf("ConfigJSON() error = %v, want nil", err)
	}
	want := `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"target_limit":1000},"source_cache":{"directory":"/var/cache/vi"}}`
	if got := string(block); got != want {
		t.Fatalf("ConfigJSON() = %s, want %s", got, want)
	}
	if strings.Contains(string(block), "command") {
		t.Fatalf("ConfigJSON() = %s, want only the config block", block)
	}
}

// TestActivationRuntimeFileConfigBlockShapes covers absent, null, and
// non-mapping config blocks. A non-mapping block is an error only when the
// caller asks for the block; the host read is unchanged, so a kind that never
// reads the block behaves exactly as before.
func TestActivationRuntimeFileConfigBlockShapes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, raw string
		wantErr   bool
	}{
		{"absent", "host:\n  sourceSystem: s\n  scope:\n    id: i\n    kind: k\n", false},
		{"null", "config: ~\n", false},
		{"scalar", "config: not-a-mapping\n", true},
		{"list", "config:\n  - a\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeRuntimeFile(t, tc.raw)
			file, err := LoadActivationRuntimeFile(path)
			if err != nil {
				t.Fatalf("LoadActivationRuntimeFile() error = %v, want nil", err)
			}
			if _, _, err := LoadActivationHostClaimMetadata(path); err != nil {
				t.Fatalf("LoadActivationHostClaimMetadata() error = %v, want nil", err)
			}
			block, err := file.ConfigJSON()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ConfigJSON() = %s, want an error", block)
				}
				if strings.Contains(err.Error(), path) {
					t.Fatalf("ConfigJSON() error = %v, must not expose the config path", err)
				}
				return
			}
			if err != nil || block != nil {
				t.Fatalf("ConfigJSON() = %s, %v; want nil, nil", block, err)
			}
		})
	}
}
