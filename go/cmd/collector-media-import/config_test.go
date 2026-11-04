// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

func testGetenv(pairs map[string]string) func(string) string {
	return func(key string) string {
		return pairs[key]
	}
}

func enabledPairs(extra map[string]string) map[string]string {
	pairs := map[string]string{
		envEnabled:       "true",
		envTier:          "qa",
		envArtifactDir:   "/tmp/media-import-fixtures",
		envBinaryPath:    "/usr/bin/whisper-cli",
		envBinarySHA256:  strings.Repeat("a", 64),
		envModelPath:     "/models/ggml-base.bin",
		envModelSHA256:   strings.Repeat("b", 64),
		envEngineVersion: "v1.9.5",
	}
	for key, value := range extra {
		pairs[key] = value
	}
	return pairs
}

// TestLoadMediaImportConfigDisabledByDefault proves the binary is inert
// without an explicit opt-in: no artifact dir or engine pins are required
// when the enable flag is absent, so an undeployed-by-default binary starts
// quietly.
func TestLoadMediaImportConfigDisabledByDefault(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "false", "0", "no"} {
		cfg, err := loadMediaImportConfig(testGetenv(map[string]string{envEnabled: raw}))
		if err != nil {
			t.Fatalf("enabled=%q: loadMediaImportConfig error = %v", raw, err)
		}
		if cfg.Enabled {
			t.Fatalf("enabled=%q: Enabled = true, want false (disabled by default)", raw)
		}
	}
}

// TestLoadMediaImportConfigNilGetenv proves a nil environment reader disables
// the collector instead of panicking.
func TestLoadMediaImportConfigNilGetenv(t *testing.T) {
	t.Parallel()

	cfg, err := loadMediaImportConfig(nil)
	if err != nil {
		t.Fatalf("loadMediaImportConfig(nil) error = %v", err)
	}
	if cfg.Enabled {
		t.Fatalf("loadMediaImportConfig(nil): Enabled = true, want false")
	}
}

// TestLoadMediaImportConfigRequiresExplicitQATier proves engine injection is
// QA-tier-only (#8879, #9729 AC2): an enabled collector without
// ESHU_MEDIA_IMPORT_TIER=qa fails closed naming the variable, and any other
// tier value is rejected. No production caller beyond ops-qa is claimed.
func TestLoadMediaImportConfigRequiresExplicitQATier(t *testing.T) {
	t.Parallel()

	pairs := enabledPairs(nil)
	delete(pairs, envTier)
	if _, err := loadMediaImportConfig(testGetenv(pairs)); err == nil {
		t.Fatalf("enabled without %s: got nil error, want a named failure", envTier)
	} else if !strings.Contains(err.Error(), envTier) {
		t.Fatalf("enabled without %s: error %q does not name the variable", envTier, err)
	}
	for _, tier := range []string{"prod", "staging", "dev", "QA "} {
		pairs := enabledPairs(map[string]string{envTier: tier})
		if _, err := loadMediaImportConfig(testGetenv(pairs)); err == nil {
			t.Fatalf("tier %q: got nil error, want a failure", tier)
		}
	}
	cfg, err := loadMediaImportConfig(testGetenv(enabledPairs(map[string]string{envTier: "qa"})))
	if err != nil {
		t.Fatalf("tier qa: loadMediaImportConfig error = %v", err)
	}
	if cfg.Tier != "qa" {
		t.Fatalf("Tier = %q, want qa", cfg.Tier)
	}
}

// TestLoadMediaImportConfigEnabledRequiresArtifactDir proves an enabled
// collector without an artifact directory fails closed with a named error
// instead of draining an implicit location.
func TestLoadMediaImportConfigEnabledRequiresArtifactDir(t *testing.T) {
	t.Parallel()

	pairs := enabledPairs(nil)
	delete(pairs, envArtifactDir)
	if _, err := loadMediaImportConfig(testGetenv(pairs)); err == nil {
		t.Fatalf("enabled without %s: got nil error, want a named failure", envArtifactDir)
	} else if !strings.Contains(err.Error(), envArtifactDir) {
		t.Fatalf("enabled without %s: error %q does not name the variable", envArtifactDir, err)
	}
}

// TestLoadMediaImportConfigRejectsRelativeArtifactDir proves the artifact
// directory must be absolute so a relative working-directory surprise cannot
// redirect the drain.
func TestLoadMediaImportConfigRejectsRelativeArtifactDir(t *testing.T) {
	t.Parallel()

	pairs := enabledPairs(map[string]string{envArtifactDir: "relative/path"})
	if _, err := loadMediaImportConfig(testGetenv(pairs)); err == nil {
		t.Fatalf("relative artifact dir: got nil error, want a failure")
	}
}

// TestLoadMediaImportConfigEnabledRequiresEnginePins proves every pinned
// whisper.cpp field is required, and a missing piece names the variable:
// there is no default engine and no silent fallback.
func TestLoadMediaImportConfigEnabledRequiresEnginePins(t *testing.T) {
	t.Parallel()

	required := []string{
		envBinaryPath,
		envBinarySHA256,
		envModelPath,
		envModelSHA256,
		envEngineVersion,
	}
	for _, key := range required {
		pairs := enabledPairs(nil)
		delete(pairs, key)
		if _, err := loadMediaImportConfig(testGetenv(pairs)); err == nil {
			t.Fatalf("enabled without %s: got nil error, want a named failure", key)
		} else if !strings.Contains(err.Error(), key) {
			t.Fatalf("enabled without %s: error %q does not name the variable", key, err)
		}
	}
}

// TestLoadMediaImportConfigRejectsMalformedDigest proves a non-hex digest is
// rejected at startup instead of reaching the engine verifier.
func TestLoadMediaImportConfigRejectsMalformedDigest(t *testing.T) {
	t.Parallel()

	pairs := enabledPairs(map[string]string{envBinarySHA256: "not-hex"})
	if _, err := loadMediaImportConfig(testGetenv(pairs)); err == nil {
		t.Fatalf("malformed digest: got nil error, want a failure")
	}
}

// TestLoadMediaImportConfigRejectsRelativePaths proves the whisper.cpp binary
// and model must be absolute: no shell or PATH lookup exists downstream.
func TestLoadMediaImportConfigRejectsRelativePaths(t *testing.T) {
	t.Parallel()

	pairs := enabledPairs(map[string]string{envBinaryPath: "whisper-cli"})
	if _, err := loadMediaImportConfig(testGetenv(pairs)); err == nil {
		t.Fatalf("relative binary path: got nil error, want a failure")
	}
	pairs = enabledPairs(map[string]string{envModelPath: "ggml-base.bin"})
	if _, err := loadMediaImportConfig(testGetenv(pairs)); err == nil {
		t.Fatalf("relative model path: got nil error, want a failure")
	}
}

// TestLoadMediaImportConfigDefaults proves the operator-tunable defaults:
// scope prefix, source system, and artifact cap.
func TestLoadMediaImportConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := loadMediaImportConfig(testGetenv(enabledPairs(nil)))
	if err != nil {
		t.Fatalf("loadMediaImportConfig error = %v", err)
	}
	if !cfg.Enabled {
		t.Fatalf("Enabled = false, want true")
	}
	if cfg.ScopePrefix == "" {
		t.Fatalf("ScopePrefix is blank, want a default")
	}
	if cfg.SourceSystem == "" {
		t.Fatalf("SourceSystem is blank, want a default")
	}
	if cfg.MaxArtifacts <= 0 {
		t.Fatalf("MaxArtifacts = %d, want a positive default", cfg.MaxArtifacts)
	}
}

// TestLoadMediaImportConfigRejectsNonPositiveArtifactCap proves the per-run
// artifact bound cannot be disabled through the environment.
func TestLoadMediaImportConfigRejectsNonPositiveArtifactCap(t *testing.T) {
	t.Parallel()

	pairs := enabledPairs(map[string]string{envMaxArtifacts: "0"})
	if _, err := loadMediaImportConfig(testGetenv(pairs)); err == nil {
		t.Fatalf("zero artifact cap: got nil error, want a failure")
	}
}
