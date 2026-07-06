// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildFamilyEntriesAppliesReadSurfaceOverrides(t *testing.T) {
	live := liveFamily{
		name: "semantic",
		kinds: func() []string {
			return []string{"semantic.code_hint", "semantic.documentation_observation"}
		},
		version: func(string) (string, bool) {
			return "1.0.0", true
		},
	}
	spec := familySpec{
		LifecycleOwner:         "go/internal/facts",
		SchemaVersion:          "1.0.0",
		ReducerDomain:          "semantic_entity_materialization",
		ProjectionHook:         "semantic_evidence",
		AdmissionHook:          "facts.ValidateSchemaVersion",
		ReadSurface:            "GET /api/v0/semantic/documentation-observations",
		ReadSurfaceOverrides:   map[string]string{"semantic.code_hint": "GET /api/v0/semantic/code-hints"},
		TruthProfile:           "optional_semantic",
		PolicyGate:             "semanticpolicy",
		ProviderKeyIndependent: false,
		Kinds:                  []string{"semantic.code_hint", "semantic.documentation_observation"},
	}

	entries, err := buildFamilyEntries(t.TempDir(), "semantic", live, spec)
	if err != nil {
		t.Fatalf("buildFamilyEntries() error = %v, want nil", err)
	}

	got := map[string]string{}
	for _, entry := range entries {
		got[entry.Kind] = entry.ReadSurface
	}
	if got["semantic.documentation_observation"] != "GET /api/v0/semantic/documentation-observations" {
		t.Fatalf("documentation read surface = %q", got["semantic.documentation_observation"])
	}
	if got["semantic.code_hint"] != "GET /api/v0/semantic/code-hints" {
		t.Fatalf("code hint read surface = %q", got["semantic.code_hint"])
	}
}

func TestBuildFamilyEntriesRejectsStaleReadSurfaceOverride(t *testing.T) {
	live := liveFamily{
		name: "semantic",
		kinds: func() []string {
			return []string{"semantic.documentation_observation"}
		},
		version: func(string) (string, bool) {
			return "1.0.0", true
		},
	}
	spec := familySpec{
		LifecycleOwner:         "go/internal/facts",
		SchemaVersion:          "1.0.0",
		ReducerDomain:          "semantic_entity_materialization",
		ProjectionHook:         "semantic_evidence",
		AdmissionHook:          "facts.ValidateSchemaVersion",
		ReadSurface:            "GET /api/v0/semantic/documentation-observations",
		ReadSurfaceOverrides:   map[string]string{"semantic.code_hint": "GET /api/v0/semantic/code-hints"},
		TruthProfile:           "optional_semantic",
		PolicyGate:             "semanticpolicy",
		ProviderKeyIndependent: false,
		Kinds:                  []string{"semantic.documentation_observation"},
	}

	_, err := buildFamilyEntries(t.TempDir(), "semantic", live, spec)
	if err == nil {
		t.Fatal("buildFamilyEntries() error = nil, want stale override error")
	}
	if !strings.Contains(err.Error(), "read_surface_overrides") {
		t.Fatalf("buildFamilyEntries() error = %q, want read_surface_overrides context", err)
	}
}

func TestBuildFamilyEntriesRejectsDanglingPayloadSchemaReference(t *testing.T) {
	repoRoot := t.TempDir()
	live := liveFamily{
		name: "aws",
		kinds: func() []string {
			return []string{"aws_resource"}
		},
		version: func(string) (string, bool) {
			return "1.0.0", true
		},
	}
	spec := familySpec{
		LifecycleOwner:         "go/internal/facts",
		SchemaVersion:          "1.0.0",
		ReducerDomain:          "aws_cloud_runtime_drift",
		ProjectionHook:         "cloud_asset_resolution",
		AdmissionHook:          "facts.ValidateSchemaVersion",
		ReadSurface:            "GET /api/v0/cloud/inventory",
		TruthProfile:           "provider_gated",
		ProviderKeyIndependent: false,
		PayloadSchemaOverrides: map[string]string{"aws_resource": "sdk/go/factschema/schema/does_not_exist.v1.schema.json"},
		Kinds:                  []string{"aws_resource"},
	}

	_, err := buildFamilyEntries(repoRoot, "aws", live, spec)
	if err == nil {
		t.Fatal("buildFamilyEntries() error = nil, want dangling payload_schema reference error")
	}
	if !strings.Contains(err.Error(), "payload_schema") {
		t.Fatalf("buildFamilyEntries() error = %q, want payload_schema context", err)
	}
	if !strings.Contains(err.Error(), "does_not_exist.v1.schema.json") {
		t.Fatalf("buildFamilyEntries() error = %q, want reference to the missing file", err)
	}
}

func TestBuildFamilyEntriesAcceptsExistingPayloadSchemaReference(t *testing.T) {
	repoRoot := t.TempDir()
	schemaRel := "sdk/go/factschema/schema/aws_resource.v1.schema.json"
	schemaAbs := filepath.Join(repoRoot, filepath.FromSlash(schemaRel))
	if err := writeTestFile(t, schemaAbs, "{}"); err != nil {
		t.Fatalf("writeTestFile() error = %v", err)
	}
	live := liveFamily{
		name: "aws",
		kinds: func() []string {
			return []string{"aws_resource"}
		},
		version: func(string) (string, bool) {
			return "1.0.0", true
		},
	}
	spec := familySpec{
		LifecycleOwner:         "go/internal/facts",
		SchemaVersion:          "1.0.0",
		ReducerDomain:          "aws_cloud_runtime_drift",
		ProjectionHook:         "cloud_asset_resolution",
		AdmissionHook:          "facts.ValidateSchemaVersion",
		ReadSurface:            "GET /api/v0/cloud/inventory",
		TruthProfile:           "provider_gated",
		ProviderKeyIndependent: false,
		PayloadSchemaOverrides: map[string]string{"aws_resource": schemaRel},
		Kinds:                  []string{"aws_resource"},
	}

	entries, err := buildFamilyEntries(repoRoot, "aws", live, spec)
	if err != nil {
		t.Fatalf("buildFamilyEntries() error = %v, want nil", err)
	}
	if len(entries) != 1 || entries[0].PayloadSchema != schemaRel {
		t.Fatalf("entries = %+v, want single entry with PayloadSchema %q", entries, schemaRel)
	}
}

func TestBuildFamilyEntriesAppliesDeprecationMarkers(t *testing.T) {
	repoRoot := t.TempDir()
	live := liveFamily{
		name: "aws",
		kinds: func() []string {
			return []string{"aws_resource"}
		},
		version: func(string) (string, bool) {
			return "1.0.0", true
		},
	}
	spec := familySpec{
		LifecycleOwner:         "go/internal/facts",
		SchemaVersion:          "1.0.0",
		ReducerDomain:          "aws_cloud_runtime_drift",
		ProjectionHook:         "cloud_asset_resolution",
		AdmissionHook:          "facts.ValidateSchemaVersion",
		ReadSurface:            "GET /api/v0/cloud/inventory",
		TruthProfile:           "provider_gated",
		ProviderKeyIndependent: false,
		DeprecatedInOverrides:  map[string]string{"aws_resource": "1.2.0"},
		RemovedInOverrides:     map[string]string{"aws_resource": "2.0.0"},
		Kinds:                  []string{"aws_resource"},
	}

	entries, err := buildFamilyEntries(repoRoot, "aws", live, spec)
	if err != nil {
		t.Fatalf("buildFamilyEntries() error = %v, want nil", err)
	}
	if len(entries) != 1 || entries[0].DeprecatedIn != "1.2.0" || entries[0].RemovedIn != "2.0.0" {
		t.Fatalf("entries = %+v, want DeprecatedIn=1.2.0 RemovedIn=2.0.0", entries)
	}
}

func TestBuildFamilyEntriesRejectsNonSemverLifecycleMarkers(t *testing.T) {
	repoRoot := t.TempDir()
	live := liveFamily{
		name:    "aws",
		kinds:   func() []string { return []string{"aws_resource"} },
		version: func(string) (string, bool) { return "1.0.0", true },
	}
	base := familySpec{
		LifecycleOwner:         "go/internal/facts",
		SchemaVersion:          "1.0.0",
		ReducerDomain:          "aws_cloud_runtime_drift",
		ProjectionHook:         "cloud_asset_resolution",
		AdmissionHook:          "facts.ValidateSchemaVersion",
		ReadSurface:            "GET /api/v0/cloud/inventory",
		TruthProfile:           "provider_gated",
		ProviderKeyIndependent: false,
		Kinds:                  []string{"aws_resource"},
	}

	cases := []struct {
		name    string
		mutate  func(*familySpec)
		wantCtx string
	}{
		{
			name:    "non-semver deprecated_in",
			mutate:  func(s *familySpec) { s.DeprecatedInOverrides = map[string]string{"aws_resource": "next"} },
			wantCtx: "deprecated_in",
		},
		{
			name: "non-semver removed_in",
			mutate: func(s *familySpec) {
				s.DeprecatedInOverrides = map[string]string{"aws_resource": "1.2.0"}
				s.RemovedInOverrides = map[string]string{"aws_resource": "2"}
			},
			wantCtx: "removed_in",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := base
			tc.mutate(&spec)
			_, err := buildFamilyEntries(repoRoot, "aws", live, spec)
			if err == nil {
				t.Fatalf("buildFamilyEntries() error = nil, want non-semver %s error", tc.wantCtx)
			}
			if !strings.Contains(err.Error(), tc.wantCtx) || !strings.Contains(err.Error(), "semver") {
				t.Fatalf("buildFamilyEntries() error = %q, want %s + semver context", err, tc.wantCtx)
			}
		})
	}
}

// TestBuildFamilyEntriesRejectsPayloadSchemaEscapingSchemaDir proves the
// containment guard is not bypassable with `..` traversal: a ref whose cleaned
// form resolves to a real repo file OUTSIDE sdk/go/factschema/schema/ must fail
// closed even though os.Stat on the resolved path would otherwise succeed.
func TestBuildFamilyEntriesRejectsPayloadSchemaEscapingSchemaDir(t *testing.T) {
	live := liveFamily{
		name:    "aws",
		kinds:   func() []string { return []string{"aws_resource"} },
		version: func(string) (string, bool) { return "1.0.0", true },
	}
	baseSpec := func(ref string) familySpec {
		return familySpec{
			LifecycleOwner:         "go/internal/facts",
			SchemaVersion:          "1.0.0",
			ReducerDomain:          "aws_cloud_runtime_drift",
			ProjectionHook:         "cloud_asset_resolution",
			AdmissionHook:          "facts.ValidateSchemaVersion",
			ReadSurface:            "GET /api/v0/cloud/inventory",
			TruthProfile:           "provider_gated",
			ProviderKeyIndependent: false,
			PayloadSchemaOverrides: map[string]string{"aws_resource": ref},
			Kinds:                  []string{"aws_resource"},
		}
	}

	cases := []struct {
		name string
		// ref is the payload_schema value under test.
		ref string
		// realTargetRel, when non-empty, is a repo-relative file the test
		// creates so os.Stat on the resolved path WOULD succeed — proving the
		// rejection comes from containment, not a dangling stat.
		realTargetRel string
	}{
		{
			// Escapes to <repoRoot>/OUTSIDE.md, a real file (the four ../ climb
			// schema -> factschema -> go -> sdk -> repoRoot), so os.Stat on the
			// resolved path succeeds and only the containment check can reject it.
			name:          "escapes to real repo file via traversal",
			ref:           "sdk/go/factschema/schema/../../../../OUTSIDE.md",
			realTargetRel: "OUTSIDE.md",
		},
		{
			name: "trailing-slash non-clean ref",
			ref:  "sdk/go/factschema/schema/aws_resource.v1.schema.json/",
		},
		{
			name: "dot-segment non-clean ref",
			ref:  "sdk/go/factschema/schema/./aws_resource.v1.schema.json",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			if tc.realTargetRel != "" {
				target := filepath.Join(repoRoot, filepath.FromSlash(tc.realTargetRel))
				if err := writeTestFile(t, target, "x"); err != nil {
					t.Fatalf("writeTestFile() error = %v", err)
				}
			}
			_, err := buildFamilyEntries(repoRoot, "aws", live, baseSpec(tc.ref))
			if err == nil {
				t.Fatalf("buildFamilyEntries() error = nil, want %q rejected", tc.ref)
			}
			if !strings.Contains(err.Error(), "payload_schema") {
				t.Fatalf("buildFamilyEntries() error = %q, want payload_schema context", err)
			}
		})
	}
}

// TestBuildFamilyEntriesAdmissionExemptFamilySkipsLiveVersion proves an
// admission-exempt family generates entries with no schema version, the exempt
// flag set, and its payload_schema recorded, without needing a live
// (XFactKinds, XSchemaVersion) helper — the live argument is zero.
func TestBuildFamilyEntriesAdmissionExemptFamilySkipsLiveVersion(t *testing.T) {
	repoRoot := t.TempDir()
	schemaRel := "sdk/go/factschema/schema/file.v1.schema.json"
	if err := writeTestFile(t, filepath.Join(repoRoot, filepath.FromSlash(schemaRel)), "{}"); err != nil {
		t.Fatalf("writeTestFile() error = %v", err)
	}
	spec := familySpec{
		LifecycleOwner:         "go/internal/facts",
		AdmissionExempt:        true,
		ReducerDomain:          "code_graph_projection",
		ProjectionHook:         "canonical_code_graph",
		AdmissionHook:          "none",
		ReadSurface:            "GET /api/v0/repositories",
		TruthProfile:           "deterministic",
		ProviderKeyIndependent: true,
		PayloadSchemaOverrides: map[string]string{"file": schemaRel},
		Kinds:                  []string{"file"},
	}

	// live is intentionally zero: an exempt family has no live helper.
	entries, err := buildFamilyEntries(repoRoot, "code", liveFamily{}, spec)
	if err != nil {
		t.Fatalf("buildFamilyEntries() error = %v, want nil", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want one entry", entries)
	}
	got := entries[0]
	if !got.AdmissionExempt {
		t.Fatalf("entry AdmissionExempt = false, want true")
	}
	if got.SchemaVersion != "" {
		t.Fatalf("entry SchemaVersion = %q, want blank", got.SchemaVersion)
	}
	if got.PayloadSchema != schemaRel {
		t.Fatalf("entry PayloadSchema = %q, want %q", got.PayloadSchema, schemaRel)
	}
}

// TestBuildFamilyEntriesAdmissionExemptFamilyRejectsSchemaVersion proves an
// exempt family that declares a schema_version fails closed, so an exempt kind
// can never be silently enrolled in version admission through the spec.
func TestBuildFamilyEntriesAdmissionExemptFamilyRejectsSchemaVersion(t *testing.T) {
	spec := familySpec{
		LifecycleOwner:         "go/internal/facts",
		AdmissionExempt:        true,
		SchemaVersion:          "1.0.0",
		ReducerDomain:          "code_graph_projection",
		ProjectionHook:         "canonical_code_graph",
		AdmissionHook:          "none",
		ReadSurface:            "GET /api/v0/repositories",
		TruthProfile:           "deterministic",
		ProviderKeyIndependent: true,
		Kinds:                  []string{"file"},
	}

	_, err := buildFamilyEntries(t.TempDir(), "code", liveFamily{}, spec)
	if err == nil {
		t.Fatal("buildFamilyEntries() error = nil, want schema_version rejection for exempt family")
	}
	if !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("buildFamilyEntries() error = %q, want schema_version context", err)
	}
}

// TestBuildFamilyEntriesAdmissionExemptFamilyRejectsSchemaVersionOverrides
// proves an exempt family that declares a per-kind schema_version_overrides
// fails closed. Without this, buildFamilyEntries skips the version branch for
// exempt families and would silently emit blank schema versions, so the YAML
// could appear to declare per-kind version admission for an exempt kind while
// runtime classification stayed unknown_kind. See issue #4752.
func TestBuildFamilyEntriesAdmissionExemptFamilyRejectsSchemaVersionOverrides(t *testing.T) {
	spec := familySpec{
		LifecycleOwner:         "go/internal/facts",
		AdmissionExempt:        true,
		SchemaVersionOverride:  map[string]string{"file": "1.0.0"},
		ReducerDomain:          "code_graph_projection",
		ProjectionHook:         "canonical_code_graph",
		AdmissionHook:          "none",
		ReadSurface:            "GET /api/v0/repositories",
		TruthProfile:           "deterministic",
		ProviderKeyIndependent: true,
		Kinds:                  []string{"file"},
	}

	_, err := buildFamilyEntries(t.TempDir(), "code", liveFamily{}, spec)
	if err == nil {
		t.Fatal("buildFamilyEntries() error = nil, want schema_version_overrides rejection for exempt family")
	}
	if !strings.Contains(err.Error(), "schema_version_overrides") {
		t.Fatalf("buildFamilyEntries() error = %q, want schema_version_overrides context", err)
	}
}

func writeTestFile(t *testing.T, path, contents string) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(contents), 0o644) //nolint:gosec // test fixture file.
}
