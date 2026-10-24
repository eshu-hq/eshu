// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packages

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

var parityObservedAt = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func parityIdentity() PackageIdentity {
	return PackageIdentity{
		Ecosystem: EcosystemNPM,
		Registry:  "https://registry.npmjs.org",
		RawName:   "left-pad",
	}
}

func parityBoundary() (scopeID, generationID, collectorInstanceID string) {
	return "npm://parity-scope", "fixture-generation", "fixture-collector"
}

// TestExtractionParityAllKindsAdmitAtV1 pins the envelope contract for every
// package-registry fact kind the census locks: each constructor admits a
// well-formed observation with its chain-pinned kind, 1.0.0 schema, reported
// confidence, scope propagation, and a stable key. A drift in any moved
// family fails here, not just in package/warning.
func TestExtractionParityAllKindsAdmitAtV1(t *testing.T) {
	t.Parallel()

	scopeID, generationID, collectorInstanceID := parityBoundary()
	identity := parityIdentity()

	cases := []struct {
		name   string
		kind   string
		schema string
		build  func() (facts.Envelope, error)
	}{
		{
			name:   "package",
			kind:   chain.PackageRegistryPackageFactKind,
			schema: chain.PackageRegistryPackageSchemaVersion,
			build: func() (facts.Envelope, error) {
				return NewPackageEnvelope(PackageObservation{
					Identity:            identity,
					ScopeID:             scopeID,
					GenerationID:        generationID,
					CollectorInstanceID: collectorInstanceID,
					FencingToken:        7,
					ObservedAt:          parityObservedAt,
					SourceURI:           "https://registry.npmjs.org/left-pad",
				})
			},
		},
		{
			name:   "package_version",
			kind:   chain.PackageRegistryPackageVersionFactKind,
			schema: chain.PackageRegistryPackageVersionSchemaVersion,
			build: func() (facts.Envelope, error) {
				return NewPackageVersionEnvelope(PackageVersionObservation{
					Package:             identity,
					Version:             "1.3.0",
					ScopeID:             scopeID,
					GenerationID:        generationID,
					CollectorInstanceID: collectorInstanceID,
					FencingToken:        7,
					ObservedAt:          parityObservedAt,
					SourceURI:           "https://registry.npmjs.org/left-pad",
				})
			},
		},
		{
			name:   "package_dependency",
			kind:   chain.PackageRegistryPackageDependencyFactKind,
			schema: chain.PackageRegistryPackageDependencySchemaVersion,
			build: func() (facts.Envelope, error) {
				return NewPackageDependencyEnvelope(PackageDependencyObservation{
					Package:             identity,
					Version:             "1.3.0",
					Dependency:          PackageIdentity{Ecosystem: EcosystemNPM, Registry: "https://registry.npmjs.org", RawName: "concat-stream"},
					Range:               "^1.6.0",
					DependencyType:      "runtime",
					ScopeID:             scopeID,
					GenerationID:        generationID,
					CollectorInstanceID: collectorInstanceID,
					FencingToken:        7,
					ObservedAt:          parityObservedAt,
					SourceURI:           "https://registry.npmjs.org/left-pad",
				})
			},
		},
		{
			name:   "package_artifact",
			kind:   chain.PackageRegistryPackageArtifactFactKind,
			schema: chain.PackageRegistryPackageArtifactSchemaVersion,
			build: func() (facts.Envelope, error) {
				return NewPackageArtifactEnvelope(PackageArtifactObservation{
					Package:             identity,
					Version:             "1.3.0",
					ArtifactKey:         "left-pad-1.3.0.tgz",
					ArtifactType:        "tarball",
					ArtifactURL:         "https://registry.npmjs.org/left-pad/-/left-pad-1.3.0.tgz",
					ScopeID:             scopeID,
					GenerationID:        generationID,
					CollectorInstanceID: collectorInstanceID,
					FencingToken:        7,
					ObservedAt:          parityObservedAt,
					SourceURI:           "https://registry.npmjs.org/left-pad",
				})
			},
		},
		{
			name:   "source_hint",
			kind:   chain.PackageRegistrySourceHintFactKind,
			schema: chain.PackageRegistrySourceHintSchemaVersion,
			build: func() (facts.Envelope, error) {
				return NewSourceHintEnvelope(SourceHintObservation{
					Package:             identity,
					Version:             "1.3.0",
					HintKind:            "repository",
					RawURL:              "https://github.com/camaraoovel/left-pad",
					ScopeID:             scopeID,
					GenerationID:        generationID,
					CollectorInstanceID: collectorInstanceID,
					FencingToken:        7,
					ObservedAt:          parityObservedAt,
					SourceURI:           "https://registry.npmjs.org/left-pad",
				})
			},
		},
		{
			name:   "vulnerability_hint",
			kind:   chain.PackageRegistryVulnerabilityHintFactKind,
			schema: chain.PackageRegistryVulnerabilityHintSchemaVersion,
			build: func() (facts.Envelope, error) {
				return NewVulnerabilityHintEnvelope(VulnerabilityHintObservation{
					Package:             identity,
					Version:             "1.3.0",
					AdvisoryID:          "GHSA-xxxx-yyyy-zzzz",
					AdvisorySource:      "github",
					VulnerabilityID:     "CVE-2026-0001",
					ScopeID:             scopeID,
					GenerationID:        generationID,
					CollectorInstanceID: collectorInstanceID,
					FencingToken:        7,
					ObservedAt:          parityObservedAt,
					SourceURI:           "https://registry.npmjs.org/left-pad",
				})
			},
		},
		{
			name:   "registry_event",
			kind:   chain.PackageRegistryRegistryEventFactKind,
			schema: chain.PackageRegistryRegistryEventSchemaVersion,
			build: func() (facts.Envelope, error) {
				return NewRegistryEventEnvelope(RegistryEventObservation{
					Package:             identity,
					Version:             "1.3.0",
					EventKey:            "left-pad@1.3.0:publish",
					EventType:           "publish",
					Actor:               "registry",
					ScopeID:             scopeID,
					GenerationID:        generationID,
					CollectorInstanceID: collectorInstanceID,
					FencingToken:        7,
					ObservedAt:          parityObservedAt,
					SourceURI:           "https://registry.npmjs.org/left-pad",
				})
			},
		},
		{
			name:   "repository_hosting",
			kind:   chain.PackageRegistryRepositoryHostingFactKind,
			schema: chain.PackageRegistryRepositoryHostingSchemaVersion,
			build: func() (facts.Envelope, error) {
				return NewRepositoryHostingEnvelope(RepositoryHostingObservation{
					Provider:            "npm",
					Registry:            "https://registry.npmjs.org",
					Repository:          "registry",
					RepositoryType:      "package-feed",
					Ecosystem:           EcosystemNPM,
					ScopeID:             scopeID,
					GenerationID:        generationID,
					CollectorInstanceID: collectorInstanceID,
					FencingToken:        7,
					ObservedAt:          parityObservedAt,
					SourceURI:           "https://registry.npmjs.org",
				})
			},
		},
		{
			name:   "warning",
			kind:   chain.PackageRegistryWarningFactKind,
			schema: chain.PackageRegistryWarningSchemaVersion,
			build: func() (facts.Envelope, error) {
				return NewWarningEnvelope(WarningObservation{
					WarningKey:          "parity-warning",
					WarningCode:         "metadata_truncated",
					Severity:            "info",
					Message:             "parity probe",
					Ecosystem:           EcosystemNPM,
					ScopeID:             scopeID,
					GenerationID:        generationID,
					CollectorInstanceID: collectorInstanceID,
					FencingToken:        7,
					ObservedAt:          parityObservedAt,
					SourceURI:           "https://registry.npmjs.org/left-pad",
				})
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelope, err := tc.build()
			if err != nil {
				t.Fatalf("envelope build(%s) error = %v", tc.name, err)
			}
			if envelope.FactKind != tc.kind {
				t.Fatalf("FactKind = %q, want %q", envelope.FactKind, tc.kind)
			}
			if envelope.SchemaVersion != tc.schema {
				t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, tc.schema)
			}
			if envelope.SourceConfidence != facts.SourceConfidenceReported {
				t.Fatalf("SourceConfidence = %q, want reported", envelope.SourceConfidence)
			}
			if envelope.ScopeID != scopeID {
				t.Fatalf("ScopeID = %q, want %q", envelope.ScopeID, scopeID)
			}
			if envelope.GenerationID != generationID {
				t.Fatalf("GenerationID = %q, want %q", envelope.GenerationID, generationID)
			}
			if !envelope.ObservedAt.Equal(parityObservedAt) {
				t.Fatalf("ObservedAt = %v, want %v", envelope.ObservedAt, parityObservedAt)
			}
			if envelope.StableFactKey == "" || envelope.FactID == "" {
				t.Fatalf("StableFactKey and FactID must both be set for %s", tc.name)
			}
		})
	}
}

// TestExtractionParityKindRequiredFieldsRejectBlank pins the fail-closed
// half of the contract: each constructor rejects its blank required field
// instead of emitting an identity-free fact.
func TestExtractionParityKindRequiredFieldsRejectBlank(t *testing.T) {
	t.Parallel()

	scopeID, generationID, collectorInstanceID := parityBoundary()
	identity := parityIdentity()

	boundary := func() (PackageIdentity, string, string, string, string) {
		return identity, scopeID, generationID, collectorInstanceID, "1.3.0"
	}

	cases := []struct {
		name    string
		wantErr string
		build   func() error
	}{
		{
			name:    "package_version_blank_version",
			wantErr: "package version must not be blank",
			build: func() error {
				pkg, scope, generation, collector, _ := boundary()
				_, err := NewPackageVersionEnvelope(PackageVersionObservation{
					Package: pkg, ScopeID: scope, GenerationID: generation,
					CollectorInstanceID: collector,
				})
				return err
			},
		},
		{
			name:    "package_dependency_blank_version",
			wantErr: "package dependency version must not be blank",
			build: func() error {
				pkg, scope, generation, collector, _ := boundary()
				_, err := NewPackageDependencyEnvelope(PackageDependencyObservation{
					Package: pkg, Dependency: identity,
					ScopeID: scope, GenerationID: generation,
					CollectorInstanceID: collector,
				})
				return err
			},
		},
		{
			name:    "package_artifact_blank_key",
			wantErr: "package artifact key must not be blank",
			build: func() error {
				pkg, scope, generation, collector, version := boundary()
				_, err := NewPackageArtifactEnvelope(PackageArtifactObservation{
					Package: pkg, Version: version,
					ScopeID: scope, GenerationID: generation,
					CollectorInstanceID: collector,
				})
				return err
			},
		},
		{
			name:    "source_hint_blank_kind",
			wantErr: "source hint kind must not be blank",
			build: func() error {
				pkg, scope, generation, collector, version := boundary()
				_, err := NewSourceHintEnvelope(SourceHintObservation{
					Package: pkg, Version: version,
					RawURL:  "https://github.com/camaraoovel/left-pad",
					ScopeID: scope, GenerationID: generation,
					CollectorInstanceID: collector,
				})
				return err
			},
		},
		{
			name:    "vulnerability_hint_blank_advisory",
			wantErr: "vulnerability hint advisory_id must not be blank",
			build: func() error {
				pkg, scope, generation, collector, version := boundary()
				_, err := NewVulnerabilityHintEnvelope(VulnerabilityHintObservation{
					Package: pkg, Version: version,
					ScopeID: scope, GenerationID: generation,
					CollectorInstanceID: collector,
				})
				return err
			},
		},
		{
			name:    "registry_event_blank_type",
			wantErr: "registry event type must not be blank",
			build: func() error {
				pkg, scope, generation, collector, version := boundary()
				_, err := NewRegistryEventEnvelope(RegistryEventObservation{
					Package: pkg, Version: version,
					EventKey: "left-pad@1.3.0:publish",
					ScopeID:  scope, GenerationID: generation,
					CollectorInstanceID: collector,
				})
				return err
			},
		},
		{
			name:    "repository_hosting_blank_provider",
			wantErr: "repository hosting provider must not be blank",
			build: func() error {
				_, scope, generation, collector, _ := boundary()
				_, err := NewRepositoryHostingEnvelope(RepositoryHostingObservation{
					Registry:   "https://registry.npmjs.org",
					Repository: "registry",
					ScopeID:    scope, GenerationID: generation,
					CollectorInstanceID: collector,
				})
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := tc.build(); err == nil {
				t.Fatalf("blank required field accepted for %s, want rejection", tc.name)
			} else if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("wrong rejection for %s: got %q, want substring %q", tc.name, err, tc.wantErr)
			}
		})
	}
}
