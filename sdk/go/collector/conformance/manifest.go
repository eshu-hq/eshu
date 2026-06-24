// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package conformance

import (
	"fmt"
	"regexp"
	"strings"

	collector "github.com/eshu-hq/eshu/sdk/go/collector"
)

const (
	manifestAPIVersion = "eshu.dev/v1alpha1"
	manifestKind       = "ComponentPackage"

	// ComponentTypeCollector identifies a component that emits Eshu facts from
	// an external source of truth. It is the only component type the collector
	// conformance harness evaluates.
	ComponentTypeCollector = "collector"

	// RuntimeAdapterOCI identifies a digest-pinned OCI artifact launched by a
	// core-owned extension host.
	RuntimeAdapterOCI = "oci"
	// RuntimeAdapterProcess identifies a local process adapter used only by
	// explicit host wiring.
	RuntimeAdapterProcess = "process"
)

var (
	identifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*[a-z0-9]$|^[a-z0-9]$`)
	// semverPattern requires a full MAJOR.MINOR.PATCH core and accepts the
	// optional pre-release and build-metadata suffixes that golang.org/x/mod's
	// semver.IsValid accepts, so this dependency-free check does not reject a
	// version string the in-tree component validator admits.
	semverPattern         = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
	artifactDigestPattern = regexp.MustCompile(`@sha256:[A-Fa-f0-9]{64}$`)
	// coreRangeComparatorPattern matches one space-separated compatibleCore
	// comparator: an optional operator plus a 1-3 segment version with optional
	// pre-release/build, mirroring the shapes the in-tree component range parser
	// accepts, so this dependency-free check does not diverge from it.
	coreRangeComparatorPattern = regexp.MustCompile(`^(>=|<=|>|<|=)?[0-9]+(\.[0-9]+){0,2}([-+][0-9A-Za-z.-]+)?$`)
)

// Manifest is the portable subset of an Eshu component package document that
// the conformance harness needs to derive the host-declared SDK contract and
// to check proof metadata. It carries both yaml and json struct tags so an
// out-of-tree collector package can decode its own manifest.yaml into this
// type without importing Eshu internal packages.
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
	Runtime           RuntimeContract   `yaml:"runtime" json:"runtime"`
	Artifacts         []Artifact        `yaml:"artifacts" json:"artifacts"`
	EmittedFacts      []FactFamily      `yaml:"emittedFacts" json:"emittedFacts"`
	ConsumerContracts ConsumerContracts `yaml:"consumerContracts" json:"consumerContracts"`
	Telemetry         Telemetry         `yaml:"telemetry" json:"telemetry"`
}

// RuntimeContract declares the public SDK protocol and host adapter a
// component expects before it can become claim-capable.
type RuntimeContract struct {
	SDKProtocol string `yaml:"sdkProtocol" json:"sdkProtocol"`
	Adapter     string `yaml:"adapter" json:"adapter"`
}

// Artifact points at one runnable component artifact.
type Artifact struct {
	Platform string `yaml:"platform" json:"platform"`
	Image    string `yaml:"image" json:"image"`
}

// FactFamily declares one emitted fact kind, schema versions, and source
// confidence values.
type FactFamily struct {
	Kind             string   `yaml:"kind" json:"kind"`
	SchemaVersions   []string `yaml:"schemaVersions" json:"schemaVersions"`
	SourceConfidence []string `yaml:"sourceConfidence" json:"sourceConfidence"`
	TombstoneAllowed bool     `yaml:"tombstoneAllowed,omitempty" json:"tombstoneAllowed,omitempty"`
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

// Contract derives the host-supported SDK collector contract from the
// manifest's emitted-fact declarations. Out-of-tree packages feed this to
// collector.NewValidator to check fixtures against their own manifest.
func (m Manifest) Contract() collector.Contract {
	protocol := strings.TrimSpace(m.Spec.Runtime.SDKProtocol)
	if protocol == "" {
		protocol = collector.ProtocolVersionV1Alpha1
	}
	facts := make([]collector.FactDeclaration, 0, len(m.Spec.EmittedFacts))
	for _, declared := range m.Spec.EmittedFacts {
		confidences := make([]collector.SourceConfidence, 0, len(declared.SourceConfidence))
		for _, confidence := range declared.SourceConfidence {
			confidences = append(confidences, collector.SourceConfidence(confidence))
		}
		facts = append(facts, collector.FactDeclaration{
			Kind:             declared.Kind,
			SchemaVersions:   append([]string(nil), declared.SchemaVersions...),
			SourceConfidence: confidences,
			TombstoneAllowed: declared.TombstoneAllowed,
		})
	}
	return collector.Contract{ProtocolVersion: protocol, Facts: facts}
}

// validateProofMetadata enforces the proof-metadata contract a package must
// satisfy before fixture conformance is meaningful: stable identity, a pinned
// core range, a digest-pinned artifact, and versioned, confidence-labeled fact
// kinds. It mirrors the core manifest validation so an out-of-tree harness run
// rejects the same shapes the in-tree host rejects.
func (m Manifest) validateProofMetadata() error {
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
	if !semverPattern.MatchString(strings.TrimSpace(m.Metadata.Version)) {
		return fmt.Errorf("metadata.version %q must be semantic version", m.Metadata.Version)
	}
	if err := validateCompatibleCoreRange(m.Spec.CompatibleCore); err != nil {
		return err
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
	if err := m.Spec.Runtime.validate(); err != nil {
		return err
	}
	if len(m.Spec.Artifacts) == 0 {
		return fmt.Errorf("spec.artifacts must include at least one artifact")
	}
	for _, artifact := range m.Spec.Artifacts {
		if err := artifact.validate(); err != nil {
			return err
		}
	}
	if len(m.Spec.EmittedFacts) == 0 {
		return fmt.Errorf("spec.emittedFacts must declare at least one fact kind")
	}
	for _, fact := range m.Spec.EmittedFacts {
		if err := fact.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (r RuntimeContract) validate() error {
	switch strings.TrimSpace(r.SDKProtocol) {
	case "":
		return fmt.Errorf("spec.runtime.sdkProtocol is required")
	case collector.ProtocolVersionV1Alpha1:
	default:
		return fmt.Errorf("spec.runtime.sdkProtocol %q is unsupported", r.SDKProtocol)
	}
	switch strings.TrimSpace(r.Adapter) {
	case RuntimeAdapterOCI, RuntimeAdapterProcess:
		return nil
	case "":
		return fmt.Errorf("spec.runtime.adapter is required")
	default:
		return fmt.Errorf("spec.runtime.adapter %q is unsupported", r.Adapter)
	}
}

func (a Artifact) validate() error {
	if strings.TrimSpace(a.Platform) == "" {
		return fmt.Errorf("artifact platform is required")
	}
	if strings.TrimSpace(a.Image) == "" {
		return fmt.Errorf("artifact image is required")
	}
	if !artifactDigestPattern.MatchString(a.Image) {
		return fmt.Errorf("artifact image %q must be digest pinned with sha256 and 64 hex characters", a.Image)
	}
	return nil
}

func (f FactFamily) validate() error {
	if err := validateIdentifier("fact kind", f.Kind); err != nil {
		return err
	}
	if !strings.Contains(strings.TrimSpace(f.Kind), ".") {
		return fmt.Errorf("fact kind %q must be namespaced with a collision-resistant prefix", f.Kind)
	}
	if len(f.SchemaVersions) == 0 {
		return fmt.Errorf("fact kind %q must declare at least one schema version", f.Kind)
	}
	for _, version := range f.SchemaVersions {
		if !semverPattern.MatchString(strings.TrimSpace(version)) {
			return fmt.Errorf("fact kind %q schema version %q must be semantic version", f.Kind, version)
		}
	}
	if len(f.SourceConfidence) == 0 {
		return fmt.Errorf("fact kind %q must declare at least one sourceConfidence value", f.Kind)
	}
	for _, confidence := range f.SourceConfidence {
		if err := validateSourceConfidence(confidence); err != nil {
			return fmt.Errorf("fact kind %q sourceConfidence: %w", f.Kind, err)
		}
	}
	return nil
}

// validateCompatibleCoreRange rejects a missing or malformed compatibleCore
// range so the harness does not green-light a package the in-tree component
// verifier would reject at install/verify. It validates comparator syntax only,
// not whether the current core satisfies the range.
func validateCompatibleCoreRange(rangeExpression string) error {
	fields := strings.Fields(rangeExpression)
	if len(fields) == 0 {
		return fmt.Errorf("spec.compatibleCore is required")
	}
	for _, field := range fields {
		if !coreRangeComparatorPattern.MatchString(field) {
			return fmt.Errorf("spec.compatibleCore comparator %q is invalid", field)
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

func validateSourceConfidence(confidence string) error {
	if confidence != strings.TrimSpace(confidence) {
		return fmt.Errorf("value %q must be canonical without surrounding whitespace", confidence)
	}
	switch collector.SourceConfidence(strings.TrimSpace(confidence)) {
	case collector.SourceConfidenceObserved,
		collector.SourceConfidenceReported,
		collector.SourceConfidenceInferred,
		collector.SourceConfidenceDerived:
		return nil
	case collector.SourceConfidenceUnknown:
		return fmt.Errorf("must not declare %q for new component output", collector.SourceConfidenceUnknown)
	case "":
		return fmt.Errorf("must not be empty")
	default:
		return fmt.Errorf("%q is unsupported", confidence)
	}
}
