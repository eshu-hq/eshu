// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// writeCommitGrantsWrappedFile stages one wrapped-object grants file for
// build-commit tests: the {"grants":[...]} shape loadCommitGrants decodes
// when the bare-array shape does not parse.
func writeCommitGrantsWrappedFile(t *testing.T, grants []component.ProducerGrant) string {
	t.Helper()
	raw, err := json.Marshal(map[string][]component.ProducerGrant{"grants": grants})
	if err != nil {
		t.Fatalf("marshal wrapped grants: %v", err)
	}
	path := filepath.Join(t.TempDir(), "grants.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write wrapped grants: %v", err)
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

// TestBuildCommitListenerMuxHonorsWrappedProducerGrants proves the wrapped
// {"grants":[...]} file shape builds exactly like the bare array: a
// regression breaking the wrapped decode branch fails this test. The object
// shape never parses as a bare array, so this test can only pass through
// the wrapped decode branch of loadCommitGrants.
func TestBuildCommitListenerMuxHonorsWrappedProducerGrants(t *testing.T) {
	t.Parallel()

	cfg := commitGrantsTestConfig(
		writeCommitGrantsManifest(t, commitGrantsTestManifest()),
		writeCommitGrantsWrappedFile(t, []component.ProducerGrant{commitGrantsTestGrant()}),
	)
	mux, err := buildCommitListenerMux(cfg, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("buildCommitListenerMux(granted core kind, wrapped) error = %v, want nil", err)
	}
	if mux == nil {
		t.Fatal("buildCommitListenerMux(granted core kind, wrapped) = nil, want mux")
	}
}

// writeCommitGrantsRaw stages one grants file with exact bytes for shape
// tests the typed writer cannot express: malformed input.
func writeCommitGrantsRaw(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "grants-raw.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write grants: %v", err)
	}
	return path
}

// TestLoadCommitGrantsBareArray pins the bare-array grants shape: a file
// holding a JSON array decodes to the grants it lists.
func TestLoadCommitGrantsBareArray(t *testing.T) {
	t.Parallel()

	grants, err := loadCommitGrants(writeCommitGrantsFile(t, []component.ProducerGrant{commitGrantsTestGrant()}))
	if err != nil {
		t.Fatalf("loadCommitGrants(bare array) error = %v, want nil", err)
	}
	if len(grants) != 1 {
		t.Fatalf("loadCommitGrants(bare array) grants = %d, want 1", len(grants))
	}
	if grants[0].ProducerID != "dev.eshu.examples.scorecard" || grants[0].Kind != "aws_resource" {
		t.Fatalf("loadCommitGrants(bare array) grant = %+v, want scorecard aws_resource grant", grants[0])
	}
}

// TestLoadCommitGrantsWrappedObject pins the wrapped grants shape: a file
// holding {"grants":[...]} decodes to the grants under the key.
func TestLoadCommitGrantsWrappedObject(t *testing.T) {
	t.Parallel()

	grants, err := loadCommitGrants(writeCommitGrantsWrappedFile(t, []component.ProducerGrant{commitGrantsTestGrant()}))
	if err != nil {
		t.Fatalf("loadCommitGrants(wrapped object) error = %v, want nil", err)
	}
	if len(grants) != 1 {
		t.Fatalf("loadCommitGrants(wrapped object) grants = %d, want 1", len(grants))
	}
	if grants[0].ProducerID != "dev.eshu.examples.scorecard" || grants[0].Kind != "aws_resource" {
		t.Fatalf("loadCommitGrants(wrapped object) grant = %+v, want scorecard aws_resource grant", grants[0])
	}
}

// TestLoadCommitGrantsEmptyPath pins the unset operator spelling: an empty
// grants path pairs nothing, so namespaced-only manifests need no file.
func TestLoadCommitGrantsEmptyPath(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"", "  "} {
		grants, err := loadCommitGrants(path)
		if err != nil {
			t.Fatalf("loadCommitGrants(%q) error = %v, want nil", path, err)
		}
		if grants != nil {
			t.Fatalf("loadCommitGrants(%q) = %+v, want nil", path, grants)
		}
	}
}

// TestLoadCommitGrantsMalformed pins the malformed-file path: bytes that
// are neither a bare array nor the wrapped object fail with the grants
// variable named, so the operator knows which file to fix.
func TestLoadCommitGrantsMalformed(t *testing.T) {
	t.Parallel()

	_, err := loadCommitGrants(writeCommitGrantsRaw(t, "{not json"))
	if err == nil {
		t.Fatal("loadCommitGrants(malformed) error = nil, want decode failure")
	}
	if !strings.Contains(err.Error(), commitEnvGrantsPath) {
		t.Fatalf("loadCommitGrants(malformed) error = %q, want %q named", err, commitEnvGrantsPath)
	}
}

// TestLoadCommitGrantsUnknownKeyFailsClosed pins the unknown-key path: an
// object without the "grants" key (e.g. {"grantz":[]}) must fail instead
// of decoding to zero grants silently.
func TestLoadCommitGrantsUnknownKeyFailsClosed(t *testing.T) {
	t.Parallel()

	_, err := loadCommitGrants(writeCommitGrantsRaw(t, `{"grantz":[]}`))
	if err == nil {
		t.Fatal(`loadCommitGrants({"grantz":[]}) error = nil, want decode failure`)
	}
	if !strings.Contains(err.Error(), commitEnvGrantsPath) {
		t.Fatalf("loadCommitGrants(unknown key) error = %q, want %q named", err, commitEnvGrantsPath)
	}
}

// TestLoadCommitGrantsMissingFile pins the missing-file path: a grants
// path that names no file fails with the grants variable named instead
// of silently pairing nothing.
func TestLoadCommitGrantsMissingFile(t *testing.T) {
	t.Parallel()

	_, err := loadCommitGrants(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err == nil {
		t.Fatal("loadCommitGrants(missing file) error = nil, want read failure")
	}
	if !strings.Contains(err.Error(), commitEnvGrantsPath) {
		t.Fatalf("loadCommitGrants(missing file) error = %q, want %q named", err, commitEnvGrantsPath)
	}
}

// TestBuildCommitListenerMuxMalformedGrants pins the build-path failure
// when the grants file cannot decode: the build fails with the grants
// variable named, before the manifest load runs.
func TestBuildCommitListenerMuxMalformedGrants(t *testing.T) {
	t.Parallel()

	cfg := commitGrantsTestConfig(
		writeCommitGrantsManifest(t, commitGrantsTestManifest()),
		writeCommitGrantsRaw(t, "{not json"),
	)
	mux, err := buildCommitListenerMux(cfg, nil, nil, slog.Default())
	if err == nil || mux != nil {
		t.Fatalf("buildCommitListenerMux(malformed grants) = (%v, %v), want fail-closed error", mux, err)
	}
	if !strings.Contains(err.Error(), commitEnvGrantsPath) {
		t.Fatalf("buildCommitListenerMux(malformed grants) error = %q, want %q named", err, commitEnvGrantsPath)
	}
}

// TestBuildCommitListenerMuxMissingManifest pins the build-path failure
// when the manifest file is absent: the build fails with the manifest
// variable named.
func TestBuildCommitListenerMuxMissingManifest(t *testing.T) {
	t.Parallel()

	cfg := commitGrantsTestConfig(
		filepath.Join(t.TempDir(), "missing-manifest.yaml"),
		writeCommitGrantsFile(t, []component.ProducerGrant{commitGrantsTestGrant()}),
	)
	mux, err := buildCommitListenerMux(cfg, nil, nil, slog.Default())
	if err == nil || mux != nil {
		t.Fatalf("buildCommitListenerMux(missing manifest) = (%v, %v), want fail-closed error", mux, err)
	}
	if !strings.Contains(err.Error(), commitEnvManifestPath) {
		t.Fatalf("buildCommitListenerMux(missing manifest) error = %q, want %q named", err, commitEnvManifestPath)
	}
}
