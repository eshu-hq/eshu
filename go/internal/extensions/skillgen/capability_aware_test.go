package skillgen

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestDefaultCapabilities_AllCollectorsEnabled(t *testing.T) {
	t.Parallel()
	caps := DefaultCapabilities()
	if caps.Source != "default" {
		t.Errorf("Source = %q, want default", caps.Source)
	}
	for _, name := range defaultCollectors {
		if !caps.IsEnabled(name) {
			t.Errorf("default collector %q should be enabled", name)
		}
	}
}

func TestLoadCapabilities_MissingFileReturnsDefault(t *testing.T) {
	t.Parallel()
	caps, err := LoadCapabilities(filepath.Join(t.TempDir(), "capabilities.local.yaml"))
	if err != nil {
		t.Fatalf("LoadCapabilities: %v", err)
	}
	if caps.Source != "default" {
		t.Errorf("Source = %q, want default", caps.Source)
	}
	if !caps.IsEnabled("aws") {
		t.Errorf("default aws should be enabled")
	}
}

func TestLoadCapabilities_DisablesAWSFromFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "capabilities.local.yaml")
	yaml := "collectors:\n  aws: false\n  azure: true\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	caps, err := LoadCapabilities(path)
	if err != nil {
		t.Fatalf("LoadCapabilities: %v", err)
	}
	if caps.Source != path {
		t.Errorf("Source = %q, want %q", caps.Source, path)
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
	path := filepath.Join(dir, "capabilities.local.yaml")
	if err := os.WriteFile(path, []byte("collectors: [unbalanced"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadCapabilities(path)
	if err == nil {
		t.Fatal("LoadCapabilities: error = nil, want parse error")
	}
}

func TestLoadCapabilities_EmptyFileIsDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "capabilities.local.yaml")
	if err := os.WriteFile(path, []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	caps, err := LoadCapabilities(path)
	if err != nil {
		t.Fatalf("LoadCapabilities: %v", err)
	}
	if caps.Source != "default" {
		t.Errorf("Source = %q, want default for blank file", caps.Source)
	}
}

func TestEnabledCollectors_OnlyEnabledNames(t *testing.T) {
	t.Parallel()
	caps := DefaultCapabilities()
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
	caps := DefaultCapabilities()
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
	caps := DefaultCapabilities()
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
		DefaultCapabilities(),
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

func allDisabledConfig() Capabilities {
	caps := DefaultCapabilities()
	for name := range caps.Collectors {
		caps.Collectors[name] = false
	}
	caps.Source = "all-disabled"
	return caps
}

func sparseConfig() Capabilities {
	caps := DefaultCapabilities()
	caps.Collectors["aws"] = false
	caps.Collectors["terraform"] = false
	caps.Collectors["kustomize"] = false
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
