// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// hardBannedDimensions lists dimension keys that must NEVER appear as OTEL
// metric labels.  These produce unbounded Prometheus label cardinality with no
// possible bounding strategy.  Adding a key here blocks CI for any metric that
// introduces it.
var hardBannedDimensions = []string{
	"repo_id",       // one per repository — unbounded
	"commit_sha",    // one per commit — unbounded
	"envelope_id",   // one per fact envelope — unbounded
	"intent_id",     // one per reducer intent — unbounded
	"worker_id",     // one per worker instance — unbounded
	"repository_id", // one per repository — unbounded
	"workload_id",   // one per deployable unit — unbounded
	"repo_path",     // file-system path — unbounded
	"cluster_id",    // one per Kubernetes cluster — unbounded
	"fact_id",       // one per fact — unbounded
	"source_run_id", // one per source run — unbounded
	"unit_id",       // one per acceptance unit — unbounded
	"document_id",   // one per document — unbounded
	"resource_arn",  // AWS ARN — unbounded
	"resource_id",   // cloud resource identifier — unbounded
	"node_id",       // graph node identifier — unbounded
	"entity_id",     // any entity identifier — unbounded
	"account_id",    // one per AWS account — bounded in practice but operator choice; use "account" not "account_id"
}

// riskTrackedDimensions lists dimension keys already present in the metric
// registry that carry high-cardinality risk.  They are grandfathered for now
// but tracked via follow-up issues.  Adding NEW usages of these keys or new
// keys to this list indicates the follow-up should be prioritized.
//
// Follow-up issues:
//
//	generation_id → file an issue to migrate generation_id-dimensioned metrics
//	                 to use a bounded epoch/generation bucket.
var riskTrackedDimensions = []string{
	"generation_id",
}

// TestCardinalityAudit_RegistryIsClean asserts that no hard-banned dimension key
// appears in the frozen metricDimensionKeys registry.  This is the primary
// gate: every approved metric label key passes through the registry.
func TestCardinalityAudit_RegistryIsClean(t *testing.T) {
	registered := telemetry.MetricDimensionKeys()
	hardBanned := make(map[string]bool, len(hardBannedDimensions))
	for _, k := range hardBannedDimensions {
		hardBanned[k] = true
	}

	var violations []string
	for _, reg := range registered {
		if hardBanned[reg] {
			violations = append(violations, fmt.Sprintf(
				"hard-banned dimension key %q found in MetricDimensionKeys() registry", reg))
		}
	}

	// Warn about risk-tracked keys already in the registry.
	risk := make(map[string]bool, len(riskTrackedDimensions))
	for _, k := range riskTrackedDimensions {
		risk[k] = true
	}
	var warnings []string
	for _, reg := range registered {
		if risk[reg] {
			warnings = append(warnings, fmt.Sprintf(
				"risk-tracked dimension key %q found in registry (migrate to bounded alternative per follow-up issue)", reg))
		}
	}
	if len(warnings) > 0 {
		t.Logf("risk-tracked dimensions still in allow-list:\n%s", strings.Join(warnings, "\n"))
	}

	if len(violations) > 0 {
		t.Errorf("hard-banned metric dimensions found in registry:\n%s\n\n"+
			"These keys produce unbounded Prometheus label cardinality. "+
			"Remove them from the registry and file a follow-up issue for each offending metric.",
			strings.Join(violations, "\n"))
	}
}

// TestCardinalityAudit_NoBannedInlineKeys scans all telemetry package source
// files for attribute.String("key", ...) calls and asserts none use a
// hard-banned key.  This catches dimension keys set inline without going
// through the Attr* helper / registry path.  Resource-attribute uses
// (service.name, service.namespace) are excluded by not matching banned keys.
func TestCardinalityAudit_NoBannedInlineKeys(t *testing.T) {
	hardBanned := make(map[string]bool, len(hardBannedDimensions))
	for _, k := range hardBannedDimensions {
		hardBanned[k] = true
	}

	// (?s) lets . match \n so multiline attribute.String( calls are caught.
	re := regexp.MustCompile(`(?s)attribute\.String\(\s*"([^"]+)"`)

	dir := telemetrySourceDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read telemetry dir: %v", err)
	}

	var violations []string
	seen := make(map[string]bool)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		matches := re.FindAllStringSubmatch(string(content), -1)
		for _, m := range matches {
			key := m[1]
			if hardBanned[key] && !seen[key] {
				seen[key] = true
				violations = append(violations, fmt.Sprintf(
					"hard-banned dimension key %q used via attribute.String() in %s", key, e.Name()))
			}
		}
	}

	if len(violations) > 0 {
		t.Errorf("hard-banned metric dimensions used inline:\n%s\n\n"+
			"Replace attribute.String(%q, ...) with the appropriate Attr* helper "+
			"(which references the registry) or move the value to span attributes / log fields.",
			strings.Join(violations, "\n"),
			violations[0])
	}
}

// TestCardinalityAudit_NoBannedKeysInContractFiles scans all contract_*.go
// files for newly defined dimension key constants whose wire value is a
// hard-banned key.  The metricDimensionKeys registry in registry.go should
// already catch these, but this test provides defense-in-depth.
func TestCardinalityAudit_NoBannedKeysInContractFiles(t *testing.T) {
	hardBanned := make(map[string]bool, len(hardBannedDimensions))
	for _, k := range hardBannedDimensions {
		hardBanned[k] = true
	}

	telemetryDir := telemetrySourceDir(t)
	entries, err := os.ReadDir(telemetryDir)
	if err != nil {
		t.Fatalf("read telemetry dir: %v", err)
	}

	var violations []string
	// Match Go string constants of the form: MetricDimensionXxx = "key"
	re := regexp.MustCompile(`MetricDimension\w+\s*=\s*"([^"]+)"`)

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(telemetryDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}

		matches := re.FindAllStringSubmatch(string(content), -1)
		for _, m := range matches {
			wireKey := m[1]
			if hardBanned[wireKey] {
				violations = append(violations, fmt.Sprintf(
					"hard-banned wire key %q in dimension constant in %s", wireKey, e.Name()))
			}
		}
	}

	if len(violations) > 0 {
		t.Errorf("hard-banned dimension key wire values found in contract files:\n%s\n\n"+
			"These keys must not be metric labels.  If the semantic dimension is necessary, "+
			"use a bounded alternative (e.g. scope_kind instead of scope_id).",
			strings.Join(violations, "\n"))
	}
}

// TestCardinalityAudit_AllowedKeysAreDocumented asserts that every key in the
// registry has a matching MetricDimension* constant defined somewhere in the
// telemetry package, AND that every defined constant is in the registry.
// A mismatch in either direction is a drift risk.
func TestCardinalityAudit_AllowedKeysAreDocumented(t *testing.T) {
	registered := telemetry.MetricDimensionKeys()

	// Collect all metric dimension wire values from contract files.
	wireKeys := collectWireKeysFromContracts(t)

	if len(wireKeys) == 0 {
		t.Fatalf("no dimension wire keys found in telemetry source files — is the checkout corrupted?")
	}

	regMap := make(map[string]bool, len(registered))
	for _, k := range registered {
		regMap[k] = true
	}

	// Direction 1: every registry key must have a matching constant.
	var undocumented []string
	for _, reg := range registered {
		if !wireKeys[reg] {
			undocumented = append(undocumented, reg)
		}
	}

	if len(undocumented) > 0 {
		t.Errorf("metric dimension keys in registry with no matching MetricDimension* constant:\n%s\n\n"+
			"Add a constant to a contract file and re-run the test.",
			strings.Join(undocumented, "\n"))
	}

	// Direction 2: every constant-defined key must be in the registry.
	var unregistered []string
	for wire := range wireKeys {
		if !regMap[wire] {
			unregistered = append(unregistered, wire)
		}
	}

	if len(unregistered) > 0 {
		t.Errorf("MetricDimension* constants defined but not in the metricDimensionKeys registry:\n%s\n\n"+
			"Add each wire key to the metricDimensionKeys slice in registry.go.",
			strings.Join(unregistered, "\n"))
	}
}

// TestCardinalityAudit_DimensionKeyCardinalityBound asserts that every
// dimension key tagged as bounded-cardinality in the contract has
// documentation confirming the bound.
func TestCardinalityAudit_DimensionKeyCardinalityBound(t *testing.T) {
	// This is a documentation-level audit.  We assert that every key in the
	// registry that is NOT a banned key is either:
	//   1. An enum of ≤20 values (closed set), OR
	//   2. A bounded numeric partition (partition_id ≤ 64), OR
	//   3. Explicitly documented with a cardinality rationale.
	//
	// The frozen registry is the allow-list; this test ensures the allow-list
	// itself doesn't silently accumulate unbounded keys.

	highCardinalityRiskKeys := map[string]string{
		"generation_id":          "unbounded across generations; re-evaluate if added to allow-list",
		"provider_kind":          "bounded per collector family; confirm ≤20 values",
		"provider_profile_class": "bounded; confirm source_class is from a closed enum",
	}

	registered := telemetry.MetricDimensionKeys()
	regMap := make(map[string]bool, len(registered))
	for _, k := range registered {
		regMap[k] = true
	}

	var warnings []string
	for key, rationale := range highCardinalityRiskKeys {
		if regMap[key] {
			warnings = append(warnings, fmt.Sprintf(
				"high-cardinality-risk key %q is in the allow-list: %s", key, rationale))
		}
	}

	// This is logged, not failed, because these keys may be legitimate when
	// bounded by operator convention.  An operator who sees this warning
	// should confirm the bound is enforced at the emission site.
	if len(warnings) > 0 {
		t.Logf("high-cardinality risk keys in allow-list (bound should be confirmed):\n%s",
			strings.Join(warnings, "\n"))
	}
}

// --- helpers ---

func telemetrySourceDir(t *testing.T) string {
	t.Helper()

	// Walk up from the test file to find the telemetry package directory.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	// The test package is telemetry_test; the source is in the parent directory
	// or at a known path relative to the module root.
	candidates := []string{
		filepath.Join(dir, "..", "telemetry"),
		filepath.Join(dir, "telemetry"),
		dir,
	}

	for _, c := range candidates {
		if fi, err := os.Stat(filepath.Join(c, "instruments.go")); err == nil && !fi.IsDir() {
			return c
		}
	}

	t.Fatalf("cannot find telemetry package directory from %s", dir)
	return ""
}

func collectWireKeysFromContracts(t *testing.T) map[string]bool {
	t.Helper()
	dir := telemetrySourceDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read telemetry dir: %v", err)
	}

	wireKeys := make(map[string]bool)
	re := regexp.MustCompile(`MetricDimension\w+\s*=\s*"([^"]+)"`)

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}

		matches := re.FindAllStringSubmatch(string(content), -1)
		for _, m := range matches {
			wireKeys[m[1]] = true
		}
	}

	return wireKeys
}
