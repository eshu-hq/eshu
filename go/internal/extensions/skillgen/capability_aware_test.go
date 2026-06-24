// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package skillgen

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// testCatalogNames is the fixed collector list the package's tests use.
// Tests that don't need to read the on-disk catalog use this list via
// DefaultCapabilitiesFor so the test does not depend on the working
// directory or the file system.
//
// The list intentionally overlaps the real catalog
// (specs/surface-inventory.v1.yaml) so a test that disables "aws" or
// "kubernetes" exercises the same names the production render will see.
var testCatalogNames = []string{
	"aws",
	"azure",
	"gcp",
	"kubernetes",
	"oci_registry",
	"terraform_state",
	"package_registry",
	"pagerduty",
	"jira",
	"git",
	"documentation",
}

func TestDefaultCapabilitiesFor_AllCollectorsEnabled(t *testing.T) {
	t.Parallel()
	caps := DefaultCapabilitiesFor(testCatalogNames)
	if caps.Source != "test" {
		t.Errorf("Source = %q, want test", caps.Source)
	}
	for _, name := range testCatalogNames {
		if !caps.IsEnabled(name) {
			t.Errorf("test collector %q should be enabled", name)
		}
	}
}

func TestLoadCapabilities_MissingOverrideFileUsesCatalog(t *testing.T) {
	t.Parallel()
	// The override file does not exist. The catalog is also absent
	// (we pass a path that points at nothing). This is the
	// fail-closed contract: a missing catalog is an error.
	dir := t.TempDir()
	missingCatalog := filepath.Join(dir, "no-such-catalog.yaml")
	_, err := LoadCapabilities(filepath.Join(dir, "capabilities.local.yaml"), missingCatalog)
	if err == nil {
		t.Fatal("LoadCapabilities: error = nil, want missing-catalog error")
	}
}

func TestLoadCapabilities_EmptyCatalogPathIsError(t *testing.T) {
	t.Parallel()
	_, err := LoadCapabilities("", "")
	if err == nil {
		t.Fatal("LoadCapabilities: error = nil, want empty-catalog error")
	}
}

func TestLoadCapabilities_DisablesAWSFromFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	overridePath := filepath.Join(dir, "capabilities.local.yaml")
	catalogPath := filepath.Join(dir, "catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(testCatalogYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := "collectors:\n  aws: false\n  azure: true\n"
	if err := os.WriteFile(overridePath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	caps, err := LoadCapabilities(overridePath, catalogPath)
	if err != nil {
		t.Fatalf("LoadCapabilities: %v", err)
	}
	if caps.Source != overridePath {
		t.Errorf("Source = %q, want %q", caps.Source, overridePath)
	}
	if caps.IsEnabled("aws") {
		t.Errorf("aws should be disabled after override")
	}
	if !caps.IsEnabled("azure") {
		t.Errorf("azure should remain enabled")
	}
	if !caps.IsEnabled("kubernetes") {
		t.Errorf("kubernetes is not in the file and should default to enabled")
	}
}

func TestLoadCapabilities_RejectsMalformedYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	overridePath := filepath.Join(dir, "capabilities.local.yaml")
	catalogPath := filepath.Join(dir, "catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(testCatalogYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overridePath, []byte("collectors: [unbalanced"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadCapabilities(overridePath, catalogPath)
	if err == nil {
		t.Fatal("LoadCapabilities: error = nil, want parse error")
	}
}

func TestLoadCapabilities_EmptyOverrideFileIsCatalog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	overridePath := filepath.Join(dir, "capabilities.local.yaml")
	catalogPath := filepath.Join(dir, "catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(testCatalogYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overridePath, []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	caps, err := LoadCapabilities(overridePath, catalogPath)
	if err != nil {
		t.Fatalf("LoadCapabilities: %v", err)
	}
	if caps.Source != overridePath {
		t.Errorf("Source = %q, want %q", caps.Source, overridePath)
	}
	if !caps.IsEnabled("aws") {
		t.Errorf("blank override file: aws should default to enabled from catalog")
	}
}

func TestEnabledCollectors_OnlyEnabledNames(t *testing.T) {
	t.Parallel()
	caps := DefaultCapabilitiesFor(testCatalogNames)
	caps.Collectors["aws"] = false
	caps.Collectors["azure"] = false
	got := caps.EnabledCollectors()
	for _, name := range got {
		if name == "aws" || name == "azure" {
			t.Errorf("EnabledCollectors() should not include %q", name)
		}
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("EnabledCollectors() is not sorted: %v", got)
	}
}

func TestDisabledCollectors_OnlyDisabledNames(t *testing.T) {
	t.Parallel()
	caps := DefaultCapabilitiesFor(testCatalogNames)
	caps.Collectors["aws"] = false
	got := caps.DisabledCollectors()
	want := []string{"aws"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DisabledCollectors() = %v, want %v", got, want)
	}
}

func TestRenderAll_AWSDisabledDoesNotListAWSAsEnabled(t *testing.T) {
	t.Parallel()
	fragments := loadCanonicalFragments(t)
	caps := DefaultCapabilitiesFor(testCatalogNames)
	caps.Collectors["aws"] = false
	caps.Source = "test-aws-disabled"
	results, err := RenderAll(fragments, caps)
	if err != nil {
		t.Fatalf("RenderAll: %v", err)
	}
	for _, r := range results {
		// The per-collector-matrix section's "Active Collectors on This
		// Deployment" list must NOT mark AWS as enabled when the
		// capability override disables it, and the disabled list must
		// enumerate AWS.
		if contains(r.Bytes, "aws (enabled)") {
			t.Errorf("host %s: aws should not be marked (enabled) when disabled; got:\n%s", r.Host, r.Bytes)
		}
		if !contains(r.Bytes, "The following collectors are disabled on this deployment") {
			t.Errorf("host %s: expected disabled list in per-collector-matrix section; got:\n%s", r.Host, r.Bytes)
		}
		if !contains(r.Bytes, "- aws\n") {
			t.Errorf("host %s: aws should appear in the disabled list; got:\n%s", r.Host, r.Bytes)
		}
	}
}

func TestRenderAll_PropertyTest_NeverPanics(t *testing.T) {
	t.Parallel()
	fragments := loadCanonicalFragments(t)
	// Generate a deterministic set of capability configurations that
	// exercise the merge path: subsets enabled, subsets disabled, names
	// outside the default set, an empty config.
	configs := []Capabilities{
		DefaultCapabilitiesFor(testCatalogNames),
		allDisabledConfig(),
		sparseConfig(),
		emptyConfig(),
		{Source: "unsourced"},
	}
	for i, caps := range configs {
		t.Run(capabilityName(i, caps), func(t *testing.T) {
			t.Parallel()
			if _, err := RenderAll(fragments, caps); err != nil {
				t.Fatalf("RenderAll(config %d): %v", i, err)
			}
		})
	}
}

func TestLoadDefaultCatalog_FailsClosedOnMissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := loadCatalogFrom(filepath.Join(dir, "no-such-file.yaml"))
	if err == nil {
		t.Fatal("loadCatalogFrom: error = nil, want missing-file error")
	}
}

func TestLoadDefaultCatalog_ParsesImplementedCollectors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(testCatalogYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	names, err := loadCatalogFrom(catalogPath)
	if err != nil {
		t.Fatalf("loadCatalogFrom: %v", err)
	}
	want := []string{"aws", "azure", "documentation", "git", "jira", "kubernetes", "oci_registry", "package_registry", "pagerduty", "prometheus_mimir", "tempo"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
}

func allDisabledConfig() Capabilities {
	caps := DefaultCapabilitiesFor(testCatalogNames)
	for name := range caps.Collectors {
		caps.Collectors[name] = false
	}
	caps.Source = "all-disabled"
	return caps
}

func sparseConfig() Capabilities {
	caps := DefaultCapabilitiesFor(testCatalogNames)
	caps.Collectors["aws"] = false
	caps.Collectors["oci_registry"] = false
	caps.Collectors["pagerduty"] = false
	caps.Source = "sparse"
	return caps
}

func emptyConfig() Capabilities {
	return Capabilities{Source: "empty", Collectors: map[string]bool{}}
}

func capabilityName(i int, c Capabilities) string {
	if c.Source == "" {
		return "config-" + string(rune('0'+i))
	}
	return c.Source
}

func contains(b []byte, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	if len(needle) > len(b) {
		return false
	}
	for i := 0; i+len(needle) <= len(b); i++ {
		if string(b[i:i+len(needle)]) == needle {
			return true
		}
	}
	return false
}

// testCatalogYAML is a fixture for tests that exercise the catalog
// parser. It mirrors the shape of specs/surface-inventory.v1.yaml so a
// test that loads this file behaves the same as production code reading
// the real catalog.
const testCatalogYAML = `version: v1
surfaces:
  - category: collector
    name: git
    readiness: implemented
  - category: collector
    name: documentation
    readiness: implemented
  - category: collector
    name: oci_registry
    readiness: implemented
  - category: collector
    name: aws
    readiness: implemented
  - category: collector
    name: azure
    readiness: implemented
  - category: collector
    name: gcp
    readiness: partial
  - category: collector
    name: kubernetes
    readiness: implemented
  - category: collector
    name: pagerduty
    readiness: implemented
  - category: collector
    name: jira
    readiness: implemented
  - category: collector
    name: package_registry
    readiness: implemented
  - category: collector
    name: grafana
    readiness: not_implemented
  - category: collector
    name: loki
    readiness: not_implemented
  - category: collector
    name: prometheus_mimir
    readiness: implemented
  - category: collector
    name: tempo
    readiness: implemented
  - category: capability
    name: code_search
    readiness: implemented
`
