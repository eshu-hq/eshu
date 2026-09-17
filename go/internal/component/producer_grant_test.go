// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"strings"
	"testing"
	"time"
)

// TestRegistryInstallAcceptsGrantedCoreFactKindClaim is the #6709 TDD red
// test: a recorded producer grant authorizes a first-party producer to emit
// an approved core-owned fact kind. The grant scope binds to a collector
// kind the manifest declares, so emission stays inside the source the core
// authorized.
func TestRegistryInstallAcceptsGrantedCoreFactKindClaim(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	grant := ProducerGrant{
		ProducerID:     "dev.example.collector.granted",
		Version:        "0.1.0",
		Kind:           "aws_resource",
		SchemaVersions: []string{"1.0.0"},
		Scope:          "aws",
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
	}
	if err := registry.RecordGrant(grant); err != nil {
		t.Fatalf("RecordGrant() error = %v, want nil", err)
	}

	manifestYAML := componentManifestForFactKind(
		"dev.example.collector.granted", "0.1.0", "aws_resource", []string{"1.0.0"},
	)
	if _, err := registry.Install(
		writeManifest(t, manifestYAML),
		verificationFor("dev.example.collector.granted", "0.1.0"),
	); err != nil {
		t.Fatalf("Install(granted core kind) error = %v, want nil", err)
	}
}

// TestRegistryInstallRejectsMismatchedGrants proves every grant field is
// load-bearing: a revoked, expired, or non-matching grant fails closed with
// the same actionable core-owned error as no grant at all.
func TestRegistryInstallRejectsMismatchedGrants(t *testing.T) {
	t.Parallel()

	base := ProducerGrant{
		ProducerID:     "dev.example.collector.pick",
		Version:        "0.1.0",
		Kind:           "aws_resource",
		SchemaVersions: []string{"1.0.0"},
		Scope:          "aws",
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
	}
	cases := map[string]func(*ProducerGrant){
		"revoked":         func(g *ProducerGrant) { g.Revoked = true },
		"expired":         func(g *ProducerGrant) { g.ExpiresAt = time.Now().Add(-time.Hour).UTC() },
		"wrong producer":  func(g *ProducerGrant) { g.ProducerID = "dev.example.collector.other" },
		"wrong version":   func(g *ProducerGrant) { g.Version = "0.2.0" },
		"wrong kind":      func(g *ProducerGrant) { g.Kind = "vpc" },
		"wrong scope":     func(g *ProducerGrant) { g.Scope = "gcp" },
		"narrower schema": func(g *ProducerGrant) { g.SchemaVersions = []string{"2.0.0"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			registry := NewRegistry(t.TempDir())
			grant := base
			mutate(&grant)
			if err := registry.RecordGrant(grant); err != nil {
				t.Fatalf("RecordGrant() error = %v, want nil", err)
			}
			manifestYAML := componentManifestForFactKind(
				"dev.example.collector.pick", "0.1.0", "aws_resource", []string{"1.0.0"},
			)
			_, err := registry.Install(
				writeManifest(t, manifestYAML),
				verificationFor("dev.example.collector.pick", "0.1.0"),
			)
			if err == nil {
				t.Fatalf("Install(%s grant) error = nil, want core-owned rejection", name)
			}
			if !strings.Contains(err.Error(), "aws_resource") || !strings.Contains(err.Error(), "core-owned") {
				t.Fatalf("Install() error = %v, want actionable core-owned fact-kind error", err)
			}
		})
	}
}

// TestRevokeGrantDeniesSubsequentInstall proves revocation is observable
// through the registry: after RevokeGrant the same declaration fails
// closed, which is the rollback primitive for withdrawing a delegation.
func TestRevokeGrantDeniesSubsequentInstall(t *testing.T) {
	t.Parallel()

	const (
		componentID = "dev.example.collector.revoked"
		version     = "0.1.0"
		kind        = "aws_resource"
		grantScope  = "aws"
	)
	registry := NewRegistry(t.TempDir())
	grant := ProducerGrant{
		ProducerID:     componentID,
		Version:        version,
		Kind:           kind,
		SchemaVersions: []string{"1.0.0"},
		Scope:          grantScope,
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
	}
	if err := registry.RecordGrant(grant); err != nil {
		t.Fatalf("RecordGrant() error = %v, want nil", err)
	}
	if err := registry.RevokeGrant(componentID, version, kind, grantScope); err != nil {
		t.Fatalf("RevokeGrant() error = %v, want nil", err)
	}
	manifestYAML := componentManifestForFactKind(componentID, version, kind, []string{"1.0.0"})
	_, err := registry.Install(writeManifest(t, manifestYAML), verificationFor(componentID, version))
	if err == nil {
		t.Fatal("Install(revoked grant) error = nil, want core-owned rejection")
	}
	if !strings.Contains(err.Error(), kind) || !strings.Contains(err.Error(), "core-owned") {
		t.Fatalf("Install() error = %v, want actionable core-owned fact-kind error", err)
	}
}

// TestRevokeGrantMissingReportsNotFound proves revocation cannot silently
// miss: naming an absent grant reports grant_not_found so rollback
// automation cannot mistake a typo for a completed revocation.
func TestRevokeGrantMissingReportsNotFound(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	err := registry.RevokeGrant("dev.example.collector.ghost", "0.1.0", "aws_resource", "aws")
	if err == nil {
		t.Fatal("RevokeGrant(absent) error = nil, want grant_not_found")
	}
	if got := ErrorCodeOf(err); got != ErrorCodeGrantNotFound {
		t.Fatalf("ErrorCodeOf() = %q, want %q", got, ErrorCodeGrantNotFound)
	}
}

// TestRegistryInstallStillRejectsUngrantedCoreFactKindClaim locks the
// fail-closed default: without a recorded grant the core-owned rejection
// stands, even for an otherwise valid manifest.
func TestRegistryInstallStillRejectsUngrantedCoreFactKindClaim(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	manifestYAML := componentManifestForFactKind(
		"dev.example.collector.ungranted", "0.1.0", "aws_resource", []string{"1.0.0"},
	)
	_, err := registry.Install(
		writeManifest(t, manifestYAML),
		verificationFor("dev.example.collector.ungranted", "0.1.0"),
	)
	if err == nil {
		t.Fatal("Install(ungranted core kind) error = nil, want core-owned rejection")
	}
	if !strings.Contains(err.Error(), "aws_resource") || !strings.Contains(err.Error(), "core-owned") {
		t.Fatalf("Install() error = %v, want actionable core-owned fact-kind error", err)
	}
	if !strings.Contains(err.Error(), "dev.example.collector.ungranted") {
		t.Fatalf("Install() error = %v, want rejection identifying the producer", err)
	}
}
