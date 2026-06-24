// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package accuracygate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeBaseline(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write baseline error = %v", err)
	}
	return path
}

func TestLoadBaselineValid(t *testing.T) {
	t.Parallel()

	path := writeBaseline(t, `{
  "schema_version": "accuracy_golden_gate.v1",
  "thresholds": {
    "complexity": {"min_precision": 1.0, "min_recall": 1.0, "min_covered_items": 8},
    "resolvers": {"min_precision": 0.9, "min_recall": 0.9, "min_covered_items": 13},
    "correlation": {"min_precision": 1.0, "min_recall": 1.0, "min_covered_items": 3}
  }
}`)
	baseline, err := LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline error = %v", err)
	}
	if len(baseline.Thresholds) != 3 {
		t.Fatalf("thresholds = %d, want 3", len(baseline.Thresholds))
	}
}

func TestLoadBaselineRejectsMissingDimension(t *testing.T) {
	t.Parallel()

	path := writeBaseline(t, `{
  "schema_version": "accuracy_golden_gate.v1",
  "thresholds": {
    "complexity": {"min_precision": 1.0, "min_recall": 1.0, "min_covered_items": 8}
  }
}`)
	_, err := LoadBaseline(path)
	if err == nil || !strings.Contains(err.Error(), "missing required dimension") {
		t.Fatalf("expected missing-dimension error, got %v", err)
	}
}

func TestLoadBaselineRejectsUnknownDimension(t *testing.T) {
	t.Parallel()

	path := writeBaseline(t, `{
  "schema_version": "accuracy_golden_gate.v1",
  "thresholds": {
    "complexity": {"min_precision": 1.0, "min_recall": 1.0, "min_covered_items": 8},
    "resolvers": {"min_precision": 0.9, "min_recall": 0.9, "min_covered_items": 13},
    "correlation": {"min_precision": 1.0, "min_recall": 1.0, "min_covered_items": 3},
    "mystery": {"min_precision": 1.0, "min_recall": 1.0, "min_covered_items": 0}
  }
}`)
	_, err := LoadBaseline(path)
	if err == nil || !strings.Contains(err.Error(), "unknown dimension") {
		t.Fatalf("expected unknown-dimension error, got %v", err)
	}
}

func TestLoadBaselineRejectsWrongSchema(t *testing.T) {
	t.Parallel()

	path := writeBaseline(t, `{"schema_version": "wrong", "thresholds": {}}`)
	_, err := LoadBaseline(path)
	if err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("expected schema_version error, got %v", err)
	}
}

func TestLoadBaselineRejectsOutOfRangePrecision(t *testing.T) {
	t.Parallel()

	path := writeBaseline(t, `{
  "schema_version": "accuracy_golden_gate.v1",
  "thresholds": {
    "complexity": {"min_precision": 1.5, "min_recall": 1.0, "min_covered_items": 8},
    "resolvers": {"min_precision": 0.9, "min_recall": 0.9, "min_covered_items": 13},
    "correlation": {"min_precision": 1.0, "min_recall": 1.0, "min_covered_items": 3}
  }
}`)
	_, err := LoadBaseline(path)
	if err == nil || !strings.Contains(err.Error(), "min_precision") {
		t.Fatalf("expected min_precision range error, got %v", err)
	}
}
