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
		envArtifactDir:   "/tmp/doc-import-fixtures",
		envBinaryPath:    "/usr/bin/tesseract",
		envBinarySHA256:  strings.Repeat("a", 64),
		envLibraries:     "/usr/lib/libtesseract.so=" + strings.Repeat("b", 64),
		envTessdataDir:   "/usr/share/tessdata",
		envModelSHA256:   strings.Repeat("c", 64),
		envEngineVersion: "5.5.0",
	}
	for key, value := range extra {
		pairs[key] = value
	}
	return pairs
}

// TestLoadImportConfigDisabledByDefault proves the binary is inert without an
// explicit opt-in: no artifact dir or engine pins are required when the enable
// flag is absent, so an undeployed-by-default binary starts quietly.
func TestLoadImportConfigDisabledByDefault(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "false", "0", "no"} {
		cfg, err := loadImportConfig(testGetenv(map[string]string{envEnabled: raw}))
		if err != nil {
			t.Fatalf("enabled=%q: loadImportConfig error = %v", raw, err)
		}
		if cfg.Enabled {
			t.Fatalf("enabled=%q: Enabled = true, want false (disabled by default)", raw)
		}
	}
}

// TestLoadImportConfigRequiresExplicitQATier proves engine injection is
// QA-tier-only (#8879, #9728 AC2): an enabled collector without
// ESHU_DOC_IMPORT_TIER=qa fails closed naming the variable, and any other
// tier value is rejected. No production caller beyond the QA tier is claimed.
func TestLoadImportConfigRequiresExplicitQATier(t *testing.T) {
	t.Parallel()

	pairs := enabledPairs(nil)
	delete(pairs, envTier)
	if _, err := loadImportConfig(testGetenv(pairs)); err == nil {
		t.Fatalf("enabled without %s: got nil error, want a named failure", envTier)
	} else if !strings.Contains(err.Error(), envTier) {
		t.Fatalf("enabled without %s: error %q does not name the variable", envTier, err)
	}
	for _, tier := range []string{"prod", "staging", "dev", "QA "} {
		pairs := enabledPairs(map[string]string{envTier: tier})
		if _, err := loadImportConfig(testGetenv(pairs)); err == nil {
			t.Fatalf("tier %q: got nil error, want a failure", tier)
		}
	}
	cfg, err := loadImportConfig(testGetenv(enabledPairs(map[string]string{envTier: "qa"})))
	if err != nil {
		t.Fatalf("tier qa: loadImportConfig error = %v", err)
	}
	if cfg.Tier != "qa" {
		t.Fatalf("Tier = %q, want qa", cfg.Tier)
	}
}

// TestLoadImportConfigNilGetenv proves a nil environment reader disables the
// collector instead of panicking.
func TestLoadImportConfigNilGetenv(t *testing.T) {
	t.Parallel()

	cfg, err := loadImportConfig(nil)
	if err != nil {
		t.Fatalf("loadImportConfig(nil) error = %v", err)
	}
	if cfg.Enabled {
		t.Fatalf("loadImportConfig(nil): Enabled = true, want false")
	}
}

// TestLoadImportConfigEnabledRequiresArtifactDir proves the second off: an
// enabled collector without an artifact directory fails closed with a named
// error instead of draining an implicit location.
func TestLoadImportConfigEnabledRequiresArtifactDir(t *testing.T) {
	t.Parallel()

	pairs := enabledPairs(nil)
	delete(pairs, envArtifactDir)
	if _, err := loadImportConfig(testGetenv(pairs)); err == nil {
		t.Fatalf("enabled without %s: got nil error, want a named failure", envArtifactDir)
	} else if !strings.Contains(err.Error(), envArtifactDir) {
		t.Fatalf("enabled without %s: error %q does not name the variable", envArtifactDir, err)
	}
}

// TestLoadImportConfigRejectsRelativeArtifactDir proves the artifact directory
// must be absolute so a relative working-directory surprise cannot redirect
// the drain.
func TestLoadImportConfigRejectsRelativeArtifactDir(t *testing.T) {
	t.Parallel()

	pairs := enabledPairs(map[string]string{envArtifactDir: "relative/path"})
	if _, err := loadImportConfig(testGetenv(pairs)); err == nil {
		t.Fatalf("relative artifact dir: got nil error, want a failure")
	}
}

// TestLoadImportConfigEnabledRequiresEnginePins proves the third off: every
// pinned tesseract field is required, and a missing piece names the variable.
func TestLoadImportConfigEnabledRequiresEnginePins(t *testing.T) {
	t.Parallel()

	required := []string{
		envBinaryPath,
		envBinarySHA256,
		envLibraries,
		envTessdataDir,
		envModelSHA256,
		envEngineVersion,
	}
	for _, key := range required {
		pairs := enabledPairs(nil)
		delete(pairs, key)
		if _, err := loadImportConfig(testGetenv(pairs)); err == nil {
			t.Fatalf("enabled without %s: got nil error, want a named failure", key)
		} else if !strings.Contains(err.Error(), key) {
			t.Fatalf("enabled without %s: error %q does not name the variable", key, err)
		}
	}
}

// TestLoadImportConfigNormalizesDigestPins proves the documented contract:
// a sha256:-prefixed or uppercase pin accepted by config validation is
// handed downstream in normalized 64-lowercase-hex form, the only form the
// engine verifiers accept. Config-accepted-then-engine-rejected is a broken
// operator contract, so normalization happens at load, not at the engine.
func TestLoadImportConfigNormalizesDigestPins(t *testing.T) {
	t.Parallel()

	wantBin := strings.Repeat("a", 64)
	wantModel := strings.Repeat("c", 64)
	wantLib := strings.Repeat("b", 64)
	pairs := enabledPairs(map[string]string{
		envBinarySHA256: "sha256:" + strings.ToUpper(wantBin),
		envLibraries:    "/usr/lib/libtesseract.so=sha256:" + strings.ToUpper(wantLib),
		envModelSHA256:  strings.ToUpper(wantModel),
	})
	cfg, err := loadImportConfig(testGetenv(pairs))
	if err != nil {
		t.Fatalf("loadImportConfig with prefixed/uppercase pins: %v", err)
	}
	if cfg.Tesseract.BinarySHA256 != wantBin {
		t.Fatalf("BinarySHA256 = %q, want normalized %q", cfg.Tesseract.BinarySHA256, wantBin)
	}
	if cfg.Tesseract.ModelSHA256 != wantModel {
		t.Fatalf("ModelSHA256 = %q, want normalized %q", cfg.Tesseract.ModelSHA256, wantModel)
	}
	if len(cfg.Tesseract.Libraries) != 1 || cfg.Tesseract.Libraries[0].SHA256 != wantLib {
		t.Fatalf("Libraries pins = %+v, want normalized %q", cfg.Tesseract.Libraries, wantLib)
	}
}

// TestLoadImportConfigRejectsMalformedDigest proves a non-hex digest is
// rejected at startup instead of reaching the engine verifier.
func TestLoadImportConfigRejectsMalformedDigest(t *testing.T) {
	t.Parallel()

	pairs := enabledPairs(map[string]string{envBinarySHA256: "not-hex"})
	if _, err := loadImportConfig(testGetenv(pairs)); err == nil {
		t.Fatalf("malformed digest: got nil error, want a failure")
	}
}

// TestLoadImportConfigRejectsRelativeBinaryPath proves the tesseract binary
// and tessdata directory must be absolute: no shell or PATH lookup exists
// downstream.
func TestLoadImportConfigRejectsRelativeBinaryPath(t *testing.T) {
	t.Parallel()

	pairs := enabledPairs(map[string]string{envBinaryPath: "tesseract"})
	if _, err := loadImportConfig(testGetenv(pairs)); err == nil {
		t.Fatalf("relative binary path: got nil error, want a failure")
	}
	pairs = enabledPairs(map[string]string{envTessdataDir: "tessdata"})
	if _, err := loadImportConfig(testGetenv(pairs)); err == nil {
		t.Fatalf("relative tessdata dir: got nil error, want a failure")
	}
}

// TestLoadImportConfigParsesLibraryPins proves the semicolon-separated
// path=digest pin list parses into pinned files, and a malformed entry fails
// with the variable named.
func TestLoadImportConfigParsesLibraryPins(t *testing.T) {
	t.Parallel()

	digest := strings.Repeat("d", 64)
	pairs := enabledPairs(map[string]string{
		envLibraries: "/usr/lib/liba.so=" + digest + ";/usr/lib/libb.so=" + digest,
	})
	cfg, err := loadImportConfig(testGetenv(pairs))
	if err != nil {
		t.Fatalf("loadImportConfig error = %v", err)
	}
	if len(cfg.Tesseract.Libraries) != 2 {
		t.Fatalf("Libraries = %d entries, want 2", len(cfg.Tesseract.Libraries))
	}

	pairs = enabledPairs(map[string]string{envLibraries: "no-equals-sign"})
	if _, err := loadImportConfig(testGetenv(pairs)); err == nil {
		t.Fatalf("malformed library pin: got nil error, want a failure")
	} else if !strings.Contains(err.Error(), envLibraries) {
		t.Fatalf("malformed library pin: error %q does not name the variable", err)
	}
}

// TestLoadImportConfigDefaults proves the operator-tunable defaults: scope
// prefix, source system, and artifact cap.
func TestLoadImportConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := loadImportConfig(testGetenv(enabledPairs(nil)))
	if err != nil {
		t.Fatalf("loadImportConfig error = %v", err)
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

// TestLoadImportConfigRejectsNonPositiveArtifactCap proves the per-run
// artifact bound cannot be disabled through the environment.
func TestLoadImportConfigRejectsNonPositiveArtifactCap(t *testing.T) {
	t.Parallel()

	pairs := enabledPairs(map[string]string{envMaxArtifacts: "0"})
	if _, err := loadImportConfig(testGetenv(pairs)); err == nil {
		t.Fatalf("zero artifact cap: got nil error, want a failure")
	}
}
