// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package component owns Eshu component package manifests and local install
// metadata.
package component

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack"
)

const (
	manifestAPIVersion = "eshu.dev/v1alpha1"
	manifestKind       = "ComponentPackage"

	// ComponentTypeCollector identifies a component that emits Eshu facts from
	// an external source of truth.
	ComponentTypeCollector = "collector"
	// CollectorSDKProtocolV1Alpha1 identifies the first out-of-tree collector
	// SDK wire protocol supported by component manifests.
	CollectorSDKProtocolV1Alpha1 = "collector-sdk/v1alpha1"
	// RuntimeAdapterOCI identifies a digest-pinned OCI artifact launched by a
	// core-owned extension host.
	RuntimeAdapterOCI = "oci"
	// RuntimeAdapterProcess identifies a local process adapter used only by
	// explicit host wiring.
	RuntimeAdapterProcess = "process"
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
	PayloadSchemaRef string   `yaml:"payloadSchemaRef,omitempty" json:"payloadSchemaRef,omitempty"`
	SchemaVersions   []string `yaml:"schemaVersions" json:"schemaVersions"`
	SourceConfidence []string `yaml:"sourceConfidence" json:"sourceConfidence"`
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

// LoadManifest loads and validates a component package manifest without
// producer-grant context: core-owned fact kinds are always rejected here.
// Registry flows that consult durable grants use loadManifest with state
// grants instead.
func LoadManifest(path string) (Manifest, error) {
	return loadManifest(path, nil)
}

// LoadManifestWithGrants loads and validates a manifest, honoring the given
// core-issued producer grants for core-owned fact kinds. Home-aware flows
// (CLI install) pass the registry's durable grants so grant-first-then-
// install resolves; every other caller uses LoadManifest and stays
// fail-closed.
func LoadManifestWithGrants(path string, grants []ProducerGrant) (Manifest, error) {
	return loadManifest(path, grants)
}

// loadManifest loads and validates a manifest, honoring the given
// core-issued producer grants for core-owned fact kinds.
func loadManifest(path string, grants []ProducerGrant) (Manifest, error) {
	return loadManifestObserved(path, grants, nil, "")
}

// loadManifestObserved is loadManifest that also reports the producer-grant
// decisions for the manifest's core-owned fact kinds to observer at stage.
// Decisions are reported once, after the identity checks pass (so a manifest
// that never reached grant evaluation reports nothing) and whether or not the
// grant allows, so a denial is observed even though the load then fails
// closed. A nil observer costs nothing beyond a nil check.
func loadManifestObserved(path string, grants []ProducerGrant, observer GrantObserver, stage GrantStage) (Manifest, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is supplied by the install flow from a locally staged manifest file, not from untrusted external input
	if err != nil {
		return Manifest{}, WrapError(ErrorCodeInvalidManifest, "read component manifest", err)
	}
	var manifest Manifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, WrapError(ErrorCodeInvalidManifest, "decode component manifest", err)
	}
	if err := manifest.validateIdentity(); err != nil {
		return Manifest{}, WrapError(ErrorCodeInvalidManifest, err.Error(), err)
	}
	now := time.Now().UTC()
	manifest.ObserveManifestGrants(context.Background(), observer, stage, grants, now)
	if err := manifest.validateFactFamilies(grants, now); err != nil {
		return Manifest{}, WrapError(ErrorCodeInvalidManifest, err.Error(), err)
	}
	return manifest, nil
}

// Validate checks manifest identity, compatibility, and owned surfaces
// without producer-grant context.
func (m Manifest) Validate() error {
	return m.validate(nil)
}

// ValidateWithGrants checks manifest identity, compatibility, and owned
// surfaces, honoring core-issued producer grants for core-owned fact kinds.
// Callers that admit granted producers (registry install, extension-host
// activation) pass the durable grants they authorize; every other caller
// uses Validate and stays fail-closed.
func (m Manifest) ValidateWithGrants(grants []ProducerGrant) error {
	return m.validate(grants)
}

// validate checks manifest identity, compatibility, and owned surfaces,
// honoring core-issued producer grants for core-owned fact kinds.
func (m Manifest) validate(grants []ProducerGrant) error {
	if err := m.validateIdentity(); err != nil {
		return err
	}
	return m.validateFactFamilies(grants, time.Now().UTC())
}

// validateIdentity checks everything that precedes producer-grant
// evaluation: manifest identity, compatibility, runtime, and artifacts.
func (m Manifest) validateIdentity() error {
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
	if err := m.Spec.Runtime.Validate(); err != nil {
		return err
	}
	if len(m.Spec.Artifacts) == 0 {
		return fmt.Errorf("spec.artifacts must include at least one artifact")
	}
	for _, artifact := range m.Spec.Artifacts {
		if err := artifact.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// validateFactFamilies checks every emitted fact family, honoring the
// producer grants live at now for core-owned kinds.
func (m Manifest) validateFactFamilies(grants []ProducerGrant, now time.Time) error {
	granted := grantedCoreKinds(m.Metadata.ID, m.Metadata.Version, m.Spec.CollectorKinds, grants, now)
	for _, fact := range m.Spec.EmittedFacts {
		if err := fact.validate(m.Metadata.ID, granted); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks the runtime SDK protocol and host adapter declaration.
func (r RuntimeContract) Validate() error {
	switch strings.TrimSpace(r.SDKProtocol) {
	case "":
		return fmt.Errorf("spec.runtime.sdkProtocol is required")
	case CollectorSDKProtocolV1Alpha1:
	default:
		return fmt.Errorf("spec.runtime.sdkProtocol %q is unsupported", r.SDKProtocol)
	}
	switch strings.TrimSpace(r.Adapter) {
	case "":
		return fmt.Errorf("spec.runtime.adapter is required")
	case RuntimeAdapterOCI, RuntimeAdapterProcess:
		return nil
	default:
		return fmt.Errorf("spec.runtime.adapter %q is unsupported", r.Adapter)
	}
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

// Validate checks fact-family fields without producer-grant context.
func (f FactFamily) Validate() error {
	return f.validate("", nil)
}

// validate checks fact-family fields, honoring granted core kinds: the map
// carries kind to covered schema versions for grants already matched to the
// owning manifest. A nil map rejects every core-owned kind. producerID names
// the claiming component so rejections identify the producer.
func (f FactFamily) validate(producerID string, granted map[string][]string) error {
	if err := validateIdentifier("fact kind", f.Kind); err != nil {
		return err
	}
	if err := validateComponentFactKind(producerID, f.Kind, granted[f.Kind], f.SchemaVersions); err != nil {
		return err
	}
	if err := validatePayloadSchemaRef(f.PayloadSchemaRef); err != nil {
		return fmt.Errorf("fact kind %q payloadSchemaRef: %w", f.Kind, err)
	}
	if len(f.SchemaVersions) == 0 {
		return fmt.Errorf("fact kind %q must declare at least one schema version", f.Kind)
	}
	for _, version := range f.SchemaVersions {
		if strings.TrimSpace(version) == "" {
			return fmt.Errorf("fact kind %q has an empty schema version", f.Kind)
		}
	}
	if len(f.SourceConfidence) == 0 {
		return fmt.Errorf("fact kind %q must declare at least one sourceConfidence value", f.Kind)
	}
	for _, confidence := range f.SourceConfidence {
		trimmed := strings.TrimSpace(confidence)
		if trimmed == "" {
			return fmt.Errorf("fact kind %q has an empty sourceConfidence value", f.Kind)
		}
		if confidence != trimmed {
			return fmt.Errorf("fact kind %q has non-canonical sourceConfidence value %q", f.Kind, confidence)
		}
		if err := facts.ValidateSourceConfidence(confidence); err != nil {
			return fmt.Errorf("fact kind %q sourceConfidence: %w", f.Kind, err)
		}
		if confidence == facts.SourceConfidenceUnknown {
			return fmt.Errorf("fact kind %q sourceConfidence must not declare unknown for new component output", f.Kind)
		}
	}
	return nil
}

func validatePayloadSchemaRef(ref string) error {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return nil
	}
	if ref != trimmed {
		return fmt.Errorf("%q must be canonical without surrounding whitespace", ref)
	}
	if err := validateIdentifier("payloadSchemaRef", trimmed); err != nil {
		return err
	}
	if _, ok := fixturepack.SchemaFor(trimmed); !ok {
		return fmt.Errorf("%q does not match a shipped factschema fixture-pack schema", trimmed)
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
