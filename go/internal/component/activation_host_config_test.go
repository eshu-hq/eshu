// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadActivationHostClaimMetadataReadsSafeClaimIdentity(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "activation.yaml")
	raw := `host:
  sourceSystem: " openssf-scorecard "
  scope:
    id: " github.com/example/widgets "
    kind: " repository "
process:
  command: ./scorecard-collector
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write activation config: %v", err)
	}

	host, ok, err := LoadActivationHostClaimMetadata(path)
	if err != nil {
		t.Fatalf("LoadActivationHostClaimMetadata() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("LoadActivationHostClaimMetadata() ok = false, want true")
	}
	if got, want := host.SourceSystem, "openssf-scorecard"; got != want {
		t.Fatalf("SourceSystem = %q, want %q", got, want)
	}
	if got, want := host.Scope.ID, "github.com/example/widgets"; got != want {
		t.Fatalf("Scope.ID = %q, want %q", got, want)
	}
	if got, want := host.Scope.Kind, "repository"; got != want {
		t.Fatalf("Scope.Kind = %q, want %q", got, want)
	}
}

func TestLoadActivationHostClaimMetadataRejectsPartialHost(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "activation.yaml")
	raw := `host:
  sourceSystem: openssf-scorecard
  scope:
    id: github.com/example/widgets
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write activation config: %v", err)
	}

	_, _, err := LoadActivationHostClaimMetadata(path)
	if err == nil {
		t.Fatal("LoadActivationHostClaimMetadata() error = nil, want partial host rejection")
	}
	if !strings.Contains(err.Error(), "host.scope.kind") {
		t.Fatalf("LoadActivationHostClaimMetadata() error = %q, want host.scope.kind rejection", err)
	}
}

func TestLoadActivationHostClaimMetadataOmitsPathFromReadError(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "private", "activation.yaml")
	_, _, err := LoadActivationHostClaimMetadata(path)
	if err == nil {
		t.Fatal("LoadActivationHostClaimMetadata() error = nil, want read failure")
	}
	if strings.Contains(err.Error(), path) || strings.Contains(err.Error(), "private") {
		t.Fatalf("LoadActivationHostClaimMetadata() error = %q, did not want raw path", err)
	}
}
