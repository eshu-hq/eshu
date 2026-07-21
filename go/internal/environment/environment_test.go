// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package environment

import (
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"prod", "prod"},
		{"Prod", "prod"},
		{"  Prod  ", "prod"},
		{"PRODUCTION", "production"},
		{"qa", "qa"},
		{"", ""},
		{"   ", ""},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		got := Normalize(tt.raw)
		if got != tt.want {
			t.Errorf("Normalize(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestCanonical(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		// Aliases mapped to canonical
		{"production", "prod"},
		{"Production", "prod"},
		{"staging", "stage"},
		{"development", "dev"},
		// Canonical names pass through
		{"prod", "prod"},
		{"qa", "qa"},
		{"stage", "stage"},
		{"dev", "dev"},
		{"test", "test"},
		{"sandbox", "sandbox"},
		{"preview", "preview"},
		{"uat", "uat"},
		{"preprod", "preprod"},
		// Unknown values pass through normalized (never rejected, never invented)
		{"unknown", "unknown"},
		{"my-custom-env", "my-custom-env"},
		{"", ""},
		{"   ", ""},
	}
	for _, tt := range tests {
		got := Canonical(tt.raw)
		if got != tt.want {
			t.Errorf("Canonical(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestIsKnownToken(t *testing.T) {
	known := []string{
		"prod", "production", "qa", "stage", "staging", "uat",
		"preprod", "dev", "development", "test", "sandbox", "preview",
	}
	unknown := []string{
		"unknown", "my-env", "", "stagingg", "PROD", "Production",
	}
	for _, token := range known {
		if !IsKnownToken(token) {
			t.Errorf("IsKnownToken(%q) = false, want true", token)
		}
	}
	for _, token := range unknown {
		if IsKnownToken(token) {
			t.Errorf("IsKnownToken(%q) = true, want false", token)
		}
	}
	// Verify count: exactly 12
	knownCount := 0
	for _, token := range allKnownTokens() {
		knownCount++
		_ = token
	}
	if knownCount != 12 {
		t.Errorf("allKnownTokens() returned %d tokens, want 12", knownCount)
	}
}

func TestEvidenceClass(t *testing.T) {
	// Verify all expected classes are defined
	expected := []string{
		"path_overlay",
		"namespace_fallback",
		"artifact_path_token",
		"ci_observation",
		"cloud_tag",
		"operator_declared",
		"hostname_inference",
		"explicit_alias_config",
		"argocd_destination",
		"namespace_label",
	}
	for _, name := range expected {
		ec, err := ParseEvidenceClass(name)
		if err != nil {
			t.Errorf("ParseEvidenceClass(%q) returned error: %v", name, err)
		}
		if string(ec) != name {
			t.Errorf("ParseEvidenceClass(%q) = %q, want %q", name, ec, name)
		}
	}
	// Unknown class
	_, err := ParseEvidenceClass("bogus")
	if err == nil {
		t.Error("ParseEvidenceClass(bogus) should return error")
	}
	// All and Validate
	classes := AllEvidenceClasses()
	if len(classes) != len(expected) {
		t.Errorf("AllEvidenceClasses() returned %d classes, want %d", len(classes), len(expected))
	}
	seen := make(map[string]bool)
	for _, c := range classes {
		seen[string(c)] = true
	}
	for _, name := range expected {
		if !seen[name] {
			t.Errorf("AllEvidenceClasses() missing %q", name)
		}
	}
}

func TestState(t *testing.T) {
	if StateBound != "bound" {
		t.Errorf("StateBound = %q, want %q", StateBound, "bound")
	}
	if StateEnvironmentUnbound != "environment-unbound" {
		t.Errorf("StateEnvironmentUnbound = %q, want %q", StateEnvironmentUnbound, "environment-unbound")
	}
}

func TestAliases(t *testing.T) {
	entries := Aliases()
	if len(entries) != 7 {
		t.Errorf("Aliases() returned %d entries, want 7", len(entries))
	}
	canonicalNames := []string{"prod", "qa", "stage", "dev", "test", "sandbox", "preview"}
	seen := make(map[string]bool)
	for _, e := range entries {
		seen[e.Canonical] = true
	}
	for _, name := range canonicalNames {
		if !seen[name] {
			t.Errorf("Aliases() missing canonical %q", name)
		}
	}
	// Verify alias mappings are correct
	for _, e := range entries {
		for _, alias := range e.Aliases {
			got := Canonical(alias)
			if got != e.Canonical {
				t.Errorf("Canonical(%q) = %q, want %q (from AliasEntry)", alias, got, e.Canonical)
			}
		}
	}
}
