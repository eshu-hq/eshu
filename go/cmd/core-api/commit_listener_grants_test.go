// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// commitGrantsTestManifest returns the scorecard manifest with its emitted
// fact switched to the core-owned aws_resource kind, so loading it without
// a covering producer grant must fail closed.
func commitGrantsTestManifest() component.Manifest {
	manifest := commitListenerManifest()
	manifest.Spec.EmittedFacts[0].Kind = "aws_resource"
	return manifest
}

// commitGrantsTestGrant returns a live producer grant covering the core
// kind the test manifest declares for the repo collector kind.
func commitGrantsTestGrant() component.ProducerGrant {
	return component.ProducerGrant{
		ProducerID:     "dev.eshu.examples.scorecard",
		Version:        "0.1.0",
		Kind:           "aws_resource",
		SchemaVersions: []string{"1.0.0"},
		Scope:          "repo",
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
	}
}

// writeCommitGrantsManifest stages one manifest file for build-commit tests.
func writeCommitGrantsManifest(t *testing.T, manifest component.Manifest) string {
	t.Helper()
	raw, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

// writeCommitGrantsFile stages one bare-array grants file for build-commit
// tests. An empty grants slice writes no file and returns "", the operator
// spelling for "no grants configured".
func writeCommitGrantsFile(t *testing.T, grants []component.ProducerGrant) string {
	t.Helper()
	if len(grants) == 0 {
		return ""
	}
	raw, err := json.Marshal(grants)
	if err != nil {
		t.Fatalf("marshal grants: %v", err)
	}
	path := filepath.Join(t.TempDir(), "grants.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write grants: %v", err)
	}
	return path
}

// commitGrantsTestConfig wires a validated commit-listener bundle pointing
// at staged files. The database stays nil: mux construction never touches
// storage, only the manifest/grants admission check.
func commitGrantsTestConfig(manifestPath, grantsPath string) *commitListenerConfig {
	return &commitListenerConfig{
		listenAddr:    "127.0.0.1:8089",
		manifestPath:  manifestPath,
		grantsPath:    grantsPath,
		claimPlaneURL: "http://127.0.0.1:1",
		sourceSystem:  "git",
		scopeKind:     scope.KindRepository,
		instanceID:    "scorecard-instance",
	}
}

// TestBuildCommitListenerMuxHonorsProducerGrants is the F2 RED test: a
// core-owned-kind manifest with a valid covering grant must build. The
// loader reads the manifest with nil grants, so even a valid grant file
// cannot admit the core kind and this fails.
func TestBuildCommitListenerMuxHonorsProducerGrants(t *testing.T) {
	t.Parallel()

	cfg := commitGrantsTestConfig(
		writeCommitGrantsManifest(t, commitGrantsTestManifest()),
		writeCommitGrantsFile(t, []component.ProducerGrant{commitGrantsTestGrant()}),
	)
	mux, err := buildCommitListenerMux(cfg, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("buildCommitListenerMux(granted core kind) error = %v, want nil", err)
	}
	if mux == nil {
		t.Fatal("buildCommitListenerMux(granted core kind) = nil, want mux")
	}
}

// TestBuildCommitListenerMuxFailsClosedWithoutGrants pins the fail-closed
// half of F2: the same core-owned-kind manifest with no grants must not
// build, before and after the grant-honoring fix.
func TestBuildCommitListenerMuxFailsClosedWithoutGrants(t *testing.T) {
	t.Parallel()

	cfg := commitGrantsTestConfig(
		writeCommitGrantsManifest(t, commitGrantsTestManifest()),
		writeCommitGrantsFile(t, nil),
	)
	if mux, err := buildCommitListenerMux(cfg, nil, nil, slog.Default()); err == nil || mux != nil {
		t.Fatalf("buildCommitListenerMux(ungranted core kind) = (%v, %v), want fail-closed error", mux, err)
	}
}
