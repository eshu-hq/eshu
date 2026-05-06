package query

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

type matrixYAML struct {
	Capabilities []matrixCapability `yaml:"capabilities"`
}

type matrixCapability struct {
	Capability string                         `yaml:"capability"`
	Profiles   map[string]matrixProfileConfig `yaml:"profiles"`
}

type matrixProfileConfig struct {
	MaxTruthLevel string `yaml:"max_truth_level"`
}

func TestCapabilityMatrixMatchesYAMLContract(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	matrixPath := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", "specs", "capability-matrix.v1.yaml"))
	raw, err := os.ReadFile(matrixPath)
	if err != nil {
		t.Fatalf("read matrix yaml: %v", err)
	}

	var parsed matrixYAML
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal matrix yaml: %v", err)
	}

	if got, want := len(capabilityMatrix), len(parsed.Capabilities); got != want {
		t.Fatalf("capabilityMatrix size = %d, want %d", got, want)
	}

	for _, capability := range parsed.Capabilities {
		support, ok := capabilityMatrix[capability.Capability]
		if !ok {
			t.Fatalf("capability %q missing from Go capabilityMatrix", capability.Capability)
		}

		assertProfileTruthMatch(t, capability.Capability, "local_lightweight", support.LocalLightweightMax, capability.Profiles["local_lightweight"].MaxTruthLevel)
		assertProfileTruthMatch(t, capability.Capability, "local_authoritative", support.LocalAuthoritativeMax, capability.Profiles["local_authoritative"].MaxTruthLevel)
		assertProfileTruthMatch(t, capability.Capability, "local_full_stack", support.LocalFullStackMax, capability.Profiles["local_full_stack"].MaxTruthLevel)
		assertProfileTruthMatch(t, capability.Capability, "production", support.ProductionMax, capability.Profiles["production"].MaxTruthLevel)
	}

	for capability := range capabilityMatrix {
		found := false
		for _, entry := range parsed.Capabilities {
			if entry.Capability == capability {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("Go capabilityMatrix has extra capability %q not present in YAML", capability)
		}
	}
}

func assertProfileTruthMatch(t *testing.T, capability, profile string, got *TruthLevel, want string) {
	t.Helper()
	if want == "unsupported" {
		if got != nil {
			t.Fatalf("%s[%s] = %q, want unsupported", capability, profile, *got)
		}
		return
	}
	if got == nil {
		t.Fatalf("%s[%s] = nil, want %q", capability, profile, want)
	}
	if string(*got) != want {
		t.Fatalf("%s[%s] = %q, want %q", capability, profile, *got, want)
	}
}

func TestBuildTruthEnvelopePanicsForUnmappedCapability(t *testing.T) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("BuildTruthEnvelope did not panic for unmapped capability")
		}
	}()
	_ = BuildTruthEnvelope(ProfileProduction, "missing.capability", TruthBasisHybrid, "test")
}

func TestParseQueryProfileRejectsInvalidValue(t *testing.T) {
	if _, err := ParseQueryProfile("definitely-not-a-real-profile"); err == nil {
		t.Fatal("ParseQueryProfile() error = nil, want non-nil")
	}
}

func TestLocalAuthoritativeSupportsFullStackPlatformImpactCapabilities(t *testing.T) {
	t.Parallel()

	capabilities := []string{
		"platform_impact.deployment_chain",
		"platform_impact.context_overview",
		"platform_impact.change_surface",
		"platform_impact.resource_to_code",
		"platform_impact.environment_compare",
	}
	for _, capability := range capabilities {
		capability := capability
		t.Run(capability, func(t *testing.T) {
			t.Parallel()

			if capabilityUnsupported(ProfileLocalAuthoritative, capability) {
				t.Fatalf("%s is unsupported for %s", capability, ProfileLocalAuthoritative)
			}
			if got, want := requiredProfile(capability), ProfileLocalAuthoritative; got != want {
				t.Fatalf("requiredProfile(%q) = %q, want %q", capability, got, want)
			}
			got := BuildTruthEnvelope(ProfileLocalAuthoritative, capability, TruthBasisHybrid, "test")
			if got.Level != TruthLevelDerived {
				t.Fatalf("local_authoritative truth level for %s = %q, want %q", capability, got.Level, TruthLevelDerived)
			}
		})
	}
}
