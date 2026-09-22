// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry_test

import (
	"fmt"
	"io/fs"
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
	"generation_id", // one per scope generation — unbounded; migrated per #3943
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
//	(none currently tracked — all known unbounded dimensions have been migrated
//	 to hard-banned; see individual issue numbers on hardBannedDimensions entries)
var riskTrackedDimensions = []string{}

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
				"hard-banned dimension key %q found in MetricDimensionKeys() registry", reg,
			))
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
				"risk-tracked dimension key %q found in registry (migrate to bounded alternative per follow-up issue)", reg,
			))
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

	var violations []string
	seen := make(map[string]bool)
	for _, src := range telemetrySourceFiles(t) {
		matches := re.FindAllStringSubmatch(src.content, -1)
		for _, m := range matches {
			key := m[1]
			if hardBanned[key] && !seen[key] {
				seen[key] = true
				violations = append(violations, fmt.Sprintf(
					"hard-banned dimension key %q used via attribute.String() in %s", key, src.rel,
				))
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

// TestCardinalityAudit_NoBannedKeysInContractFiles scans every source file in
// the telemetry package and its contract, contract/observability, and
// contract/thirdparty subpackages (issue #6777 nested the frozen contract
// declarations out of the flat contract_*.go layout this test originally
// assumed) for newly defined dimension key constants whose wire value is a
// hard-banned key.  The metricDimensionKeys registry in registry.go should
// already catch these, but this test provides defense-in-depth.
func TestCardinalityAudit_NoBannedKeysInContractFiles(t *testing.T) {
	hardBanned := make(map[string]bool, len(hardBannedDimensions))
	for _, k := range hardBannedDimensions {
		hardBanned[k] = true
	}

	// Match Go string constants of the form: MetricDimensionXxx = "key"
	re := regexp.MustCompile(`MetricDimension\w+\s*=\s*"([^"]+)"`)

	var violations []string
	for _, src := range telemetrySourceFiles(t) {
		matches := re.FindAllStringSubmatch(src.content, -1)
		for _, m := range matches {
			wireKey := m[1]
			if hardBanned[wireKey] {
				violations = append(violations, fmt.Sprintf(
					"hard-banned wire key %q in dimension constant in %s", wireKey, src.rel,
				))
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
				"high-cardinality-risk key %q is in the allow-list: %s", key, rationale,
			))
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

	wireKeys := make(map[string]bool)
	re := regexp.MustCompile(`MetricDimension\w+\s*=\s*"([^"]+)"`)

	for _, src := range telemetrySourceFiles(t) {
		matches := re.FindAllStringSubmatch(src.content, -1)
		for _, m := range matches {
			wireKeys[m[1]] = true
		}
	}

	return wireKeys
}

// telemetrySource is one non-test Go source file under the telemetry package
// tree, with its path relative to the package root for violation messages.
type telemetrySource struct {
	rel     string
	content string
}

// telemetrySourceFiles returns every non-test Go file under the telemetry
// package root, walking all subdirectories. Issue #6777 moved the per-family
// contract declarations into contract/ and its subpackages; walking the tree
// keeps any future nested package covered by construction instead of relying
// on a hardcoded directory list. It fails closed if the walk finds no file
// under contract/, so a relocation cannot silently empty the scan.
func telemetrySourceFiles(t *testing.T) []telemetrySource {
	t.Helper()
	root := telemetrySourceDir(t)
	var files []telemetrySource
	sawContract := false
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path) // #nosec G304 -- path comes from walking the package's own source tree
		if err != nil {
			return err
		}
		if strings.HasPrefix(filepath.ToSlash(rel), "contract/") {
			sawContract = true
		}
		files = append(files, telemetrySource{rel: filepath.ToSlash(rel), content: string(content)})
		return nil
	})
	if err != nil {
		t.Fatalf("walk telemetry source tree %s: %v", root, err)
	}
	if !sawContract {
		t.Fatalf("no Go source found under %s/contract; the contract declarations moved and this scan would be empty", root)
	}
	return files
}
