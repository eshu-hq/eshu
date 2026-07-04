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

func writeTestFile(t *testing.T, path, contents string) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(contents), 0o644) //nolint:gosec // test fixture file.
}
