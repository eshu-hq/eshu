// Package component owns Eshu component package manifests and local install
// metadata.
package component

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

const (
	manifestAPIVersion = "eshu.dev/v1alpha1"
	manifestKind       = "ComponentPackage"

	// ComponentTypeCollector identifies a component that emits Eshu facts from
	// an external source of truth.
	ComponentTypeCollector = "collector"
)

var (
	identifierPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*[a-z0-9]$|^[a-z0-9]$`)
	artifactDigestPattern = regexp.MustCompile(`@sha256:[A-Fa-f0-9]{64}$`)
)

// Manifest is the top-level component package document.
type Manifest struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       Spec     `yaml:"spec" json:"spec"`
}

// Metadata identifies one component package.
type Metadata struct {
	ID        string `yaml:"id" json:"id"`
	Name      string `yaml:"name" json:"name"`
	Publisher string `yaml:"publisher" json:"publisher"`
	Version   string `yaml:"version" json:"version"`
}

// Spec declares runtime compatibility and component-owned surfaces.
type Spec struct {
	CompatibleCore    string            `yaml:"compatibleCore" json:"compatibleCore"`
	ComponentType     string            `yaml:"componentType" json:"componentType"`
	CollectorKinds    []string          `yaml:"collectorKinds" json:"collectorKinds"`
	Artifacts         []Artifact        `yaml:"artifacts" json:"artifacts"`
	EmittedFacts      []FactFamily      `yaml:"emittedFacts" json:"emittedFacts"`
	ConsumerContracts ConsumerContracts `yaml:"consumerContracts" json:"consumerContracts"`
	Telemetry         Telemetry         `yaml:"telemetry" json:"telemetry"`
}

// Artifact points at one runnable component artifact.
type Artifact struct {
	Platform string `yaml:"platform" json:"platform"`
	Image    string `yaml:"image" json:"image"`
}

// FactFamily declares one emitted fact kind and its schema versions.
type FactFamily struct {
	Kind           string   `yaml:"kind" json:"kind"`
	SchemaVersions []string `yaml:"schemaVersions" json:"schemaVersions"`
}

// ConsumerContracts declares downstream runtime contracts required by the
// emitted fact kinds.
type ConsumerContracts struct {
	Reducer ReducerContract `yaml:"reducer" json:"reducer"`
}

// ReducerContract declares reducer phases expected by a component.
type ReducerContract struct {
	Phases []string `yaml:"phases" json:"phases"`
}

// Telemetry declares component-owned telemetry surfaces.
type Telemetry struct {
	MetricsPrefix string `yaml:"metricsPrefix" json:"metricsPrefix"`
}

// LoadManifest loads and validates a component package manifest.
func LoadManifest(path string) (Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read component manifest: %w", err)
	}
	var manifest Manifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode component manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate checks manifest identity, compatibility, and owned surfaces.
func (m Manifest) Validate() error {
	if strings.TrimSpace(m.APIVersion) != manifestAPIVersion {
		return fmt.Errorf("apiVersion must be %q", manifestAPIVersion)
	}
	if strings.TrimSpace(m.Kind) != manifestKind {
		return fmt.Errorf("kind must be %q", manifestKind)
	}
	if err := validateIdentifier("metadata.id", m.Metadata.ID); err != nil {
		return err
	}
	if strings.TrimSpace(m.Metadata.Name) == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if err := validateIdentifier("metadata.publisher", m.Metadata.Publisher); err != nil {
		return err
	}
	if err := validateSemverVersion("metadata.version", m.Metadata.Version); err != nil {
		return err
	}
	if strings.TrimSpace(m.Spec.CompatibleCore) == "" {
		return fmt.Errorf("spec.compatibleCore is required")
	}
	if strings.TrimSpace(m.Spec.ComponentType) != ComponentTypeCollector {
		return fmt.Errorf("spec.componentType must be %q", ComponentTypeCollector)
	}
	if len(m.Spec.CollectorKinds) == 0 {
		return fmt.Errorf("spec.collectorKinds must include at least one collector kind")
	}
	for _, kind := range m.Spec.CollectorKinds {
		if err := validateIdentifier("collector kind", kind); err != nil {
			return err
		}
	}
	if len(m.Spec.Artifacts) == 0 {
		return fmt.Errorf("spec.artifacts must include at least one artifact")
	}
	for _, artifact := range m.Spec.Artifacts {
		if err := artifact.Validate(); err != nil {
			return err
		}
	}
	for _, fact := range m.Spec.EmittedFacts {
		if err := fact.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks artifact fields.
func (a Artifact) Validate() error {
	if strings.TrimSpace(a.Platform) == "" {
		return fmt.Errorf("artifact platform is required")
	}
	if strings.TrimSpace(a.Image) == "" {
		return fmt.Errorf("artifact image is required")
	}
	if !artifactDigestPattern.MatchString(a.Image) {
		return fmt.Errorf("artifact image must be digest pinned with sha256 and 64 hex characters")
	}
	return nil
}

// Validate checks fact-family fields.
func (f FactFamily) Validate() error {
	if err := validateIdentifier("fact kind", f.Kind); err != nil {
		return err
	}
	if len(f.SchemaVersions) == 0 {
		return fmt.Errorf("fact kind %q must declare at least one schema version", f.Kind)
	}
	for _, version := range f.SchemaVersions {
		if strings.TrimSpace(version) == "" {
			return fmt.Errorf("fact kind %q has an empty schema version", f.Kind)
		}
	}
	return nil
}

func validateIdentifier(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	if !identifierPattern.MatchString(trimmed) {
		return fmt.Errorf("%s %q must use lowercase letters, numbers, dots, underscores, or hyphens", field, value)
	}
	return nil
}

func validateSemverVersion(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	if !semver.IsValid(normalizeSemver(trimmed)) {
		return fmt.Errorf("%s %q must be semantic version", field, value)
	}
	return nil
}
