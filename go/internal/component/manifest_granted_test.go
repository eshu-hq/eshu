// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const grantedLoadCoreOwnedError = "is core-owned by Eshu and cannot be claimed by optional components"

func grantedLoadManifestPath(t *testing.T, factKind string) string {
	t.Helper()
	return writeManifest(t, componentManifestForFactKind(
		"dev.example.collector.pick", "0.1.0", factKind, []string{"1.0.0"},
	))
}

func corruptRegistry(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, registryFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt registry: %v", err)
	}
	return home
}

// TestLoadGrantedManifestAdmitsGrantedCoreKind proves the read-only loader
// resolves a core-owned declaration through the registry's live grants and
// returns the grants it used so verify can apply the same set.
func TestLoadGrantedManifestAdmitsGrantedCoreKind(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	registry := NewRegistry(home)
	if err := registry.RecordGrant(baseGrantForDecisions()); err != nil {
		t.Fatalf("RecordGrant() error = %v, want nil", err)
	}

	manifest, grants, err := registry.LoadGrantedManifest(grantedLoadManifestPath(t, "aws_resource"))
	if err != nil {
		t.Fatalf("LoadGrantedManifest() error = %v, want nil", err)
	}
	if got, want := manifest.Metadata.ID, "dev.example.collector.pick"; got != want {
		t.Fatalf("manifest id = %q, want %q", got, want)
	}
	if len(grants) != 1 || grants[0].Kind != "aws_resource" {
		t.Fatalf("grants = %+v, want the one recorded aws_resource grant", grants)
	}
}

// TestLoadGrantedManifestFailsClosedWithoutLiveGrant proves every way a grant
// can fail to cover the declaration leaves the existing core-owned error
// intact, and that a missing home behaves like an empty registry.
func TestLoadGrantedManifestFailsClosedWithoutLiveGrant(t *testing.T) {
	t.Parallel()

	mutate := func(edit func(*ProducerGrant)) *ProducerGrant {
		grant := baseGrantForDecisions()
		edit(&grant)
		return &grant
	}
	tests := []struct {
		name  string
		grant *ProducerGrant
		home  string
		blank bool
	}{
		{name: "no grants recorded"},
		{name: "blank home", blank: true},
		{name: "expired", grant: mutate(func(g *ProducerGrant) { g.ExpiresAt = time.Now().Add(-time.Hour) })},
		{name: "revoked", grant: mutate(func(g *ProducerGrant) { g.Revoked = true })},
		{name: "other scope", grant: mutate(func(g *ProducerGrant) { g.Scope = "gcp" })},
		{name: "other kind", grant: mutate(func(g *ProducerGrant) { g.Kind = "aws_relationship" })},
		{name: "other version", grant: mutate(func(g *ProducerGrant) { g.Version = "0.2.0" })},
		{name: "other schema version", grant: mutate(func(g *ProducerGrant) { g.SchemaVersions = []string{"2.0.0"} })},
		{name: "other producer", grant: mutate(func(g *ProducerGrant) { g.ProducerID = "dev.example.collector.other" })},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			if tt.blank {
				home = ""
			}
			registry := NewRegistry(home)
			if tt.grant != nil {
				if err := registry.RecordGrant(*tt.grant); err != nil {
					t.Fatalf("RecordGrant() error = %v, want nil", err)
				}
			}
			_, grants, err := registry.LoadGrantedManifest(grantedLoadManifestPath(t, "aws_resource"))
			if err == nil {
				t.Fatal("LoadGrantedManifest() error = nil, want the fail-closed manifest error")
			}
			if got := ErrorCodeOf(err); got != ErrorCodeInvalidManifest {
				t.Fatalf("code = %q, want %q", got, ErrorCodeInvalidManifest)
			}
			if !strings.Contains(err.Error(), grantedLoadCoreOwnedError) {
				t.Fatalf("error = %q, want the core-owned error", err)
			}
			if len(grants) != 0 {
				t.Fatalf("grants = %+v, want none on failure", grants)
			}
		})
	}
}

// TestLoadGrantedManifestSkipsRegistryForNamespacedKinds proves a manifest
// that needs no grant never reads the registry: an unreadable one cannot
// change its verdict, so existing namespaced flows behave as before.
func TestLoadGrantedManifestSkipsRegistryForNamespacedKinds(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(corruptRegistry(t))
	manifest, grants, err := registry.LoadGrantedManifest(writeManifest(t, validManifestYAML()))
	if err != nil {
		t.Fatalf("LoadGrantedManifest() error = %v, want nil", err)
	}
	if manifest.Metadata.ID == "" {
		t.Fatal("manifest id is empty, want the loaded manifest")
	}
	if grants != nil {
		t.Fatalf("grants = %+v, want nil: the registry was not consulted", grants)
	}
}

// TestLoadGrantedManifestReportsUnreadableRegistry proves a core-owned
// declaration with grants that cannot be read fails with the registry error,
// not as if no grants existed.
func TestLoadGrantedManifestReportsUnreadableRegistry(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(corruptRegistry(t))
	_, _, err := registry.LoadGrantedManifest(grantedLoadManifestPath(t, "aws_resource"))
	if err == nil {
		t.Fatal("LoadGrantedManifest() error = nil, want the unreadable-registry error")
	}
	if got := ErrorCodeOf(err); got != ErrorCodeCorruptedRegistryState {
		t.Fatalf("code = %q, want %q", got, ErrorCodeCorruptedRegistryState)
	}
}

// TestLoadGrantedManifestKeepsIdentityErrors proves identity failures still
// surface before any grant evaluation.
func TestLoadGrantedManifestKeepsIdentityErrors(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	_, _, err := registry.LoadGrantedManifest(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("LoadGrantedManifest() error = nil, want a read error")
	}
	if got := ErrorCodeOf(err); got != ErrorCodeInvalidManifest {
		t.Fatalf("code = %q, want %q", got, ErrorCodeInvalidManifest)
	}
}
