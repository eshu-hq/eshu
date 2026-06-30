// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
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

	entries, err := buildFamilyEntries("semantic", live, spec)
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

	_, err := buildFamilyEntries("semantic", live, spec)
	if err == nil {
		t.Fatal("buildFamilyEntries() error = nil, want stale override error")
	}
	if !strings.Contains(err.Error(), "read_surface_overrides") {
		t.Fatalf("buildFamilyEntries() error = %q, want read_surface_overrides context", err)
	}
}
