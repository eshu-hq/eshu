package packageregistry

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestPackageObservationBuildsReportedPackageEnvelope(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 12, 10, 30, 0, 0, time.UTC)
	observation := PackageObservation{
		Identity: PackageIdentity{
			Ecosystem: EcosystemPyPI,
			Registry:  "https://pypi.org/simple/",
			RawName:   "Friendly_Bard",
		},
		ScopeID:             "pypi://pypi.org/simple/friendly-bard",
		GenerationID:        "etag:abc123",
		CollectorInstanceID: "public-pypi",
		FencingToken:        42,
		ObservedAt:          observedAt,
		Visibility:          VisibilityPublic,
		SourceURI:           "https://pypi.org/simple/friendly-bard/",
	}

	envelope, err := NewPackageEnvelope(observation)
	if err != nil {
		t.Fatalf("NewPackageEnvelope() error = %v", err)
	}

	if envelope.FactKind != facts.PackageRegistryPackageFactKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, facts.PackageRegistryPackageFactKind)
	}
	if envelope.SchemaVersion != facts.PackageRegistryPackageSchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, facts.PackageRegistryPackageSchemaVersion)
	}
	if envelope.CollectorKind != CollectorKind {
		t.Fatalf("CollectorKind = %q, want %q", envelope.CollectorKind, CollectorKind)
	}
	if envelope.SourceConfidence != facts.SourceConfidenceReported {
		t.Fatalf("SourceConfidence = %q, want %q", envelope.SourceConfidence, facts.SourceConfidenceReported)
	}
	if envelope.FencingToken != 42 {
		t.Fatalf("FencingToken = %d, want 42", envelope.FencingToken)
	}
	if !envelope.ObservedAt.Equal(observedAt) {
		t.Fatalf("ObservedAt = %s, want %s", envelope.ObservedAt, observedAt)
	}
	if envelope.SourceRef.SourceSystem != CollectorKind {
		t.Fatalf("SourceRef.SourceSystem = %q, want %q", envelope.SourceRef.SourceSystem, CollectorKind)
	}
	if envelope.SourceRef.SourceURI != observation.SourceURI {
		t.Fatalf("SourceRef.SourceURI = %q, want %q", envelope.SourceRef.SourceURI, observation.SourceURI)
	}
	if envelope.StableFactKey == "" || envelope.FactID == "" {
		t.Fatalf("stable identifiers must not be blank: %#v", envelope)
	}

	wantPayload := map[string]any{
		"collector_instance_id": "public-pypi",
		"ecosystem":             string(EcosystemPyPI),
		"registry":              "pypi.org/simple",
		"raw_name":              "Friendly_Bard",
		"normalized_name":       "friendly-bard",
		"namespace":             "",
		"classifier":            "",
		"package_id":            "pypi://pypi.org/simple/friendly-bard",
		"visibility":            string(VisibilityPublic),
	}
	for key, want := range wantPayload {
		if got := envelope.Payload[key]; got != want {
			t.Fatalf("Payload[%q] = %#v, want %#v; payload=%#v", key, got, want, envelope.Payload)
		}
	}
}

func TestPackageObservationStableIDUsesNormalizedIdentity(t *testing.T) {
	t.Parallel()

	base := PackageObservation{
		Identity: PackageIdentity{
			Ecosystem: EcosystemPyPI,
			Registry:  "https://pypi.org/simple/",
			RawName:   "Friendly_Bard",
		},
		ScopeID:             "pypi://pypi.org/simple/friendly-bard",
		GenerationID:        "etag:abc123",
		CollectorInstanceID: "public-pypi",
		ObservedAt:          time.Date(2026, 5, 12, 10, 30, 0, 0, time.UTC),
	}
	sameIdentityDifferentRaw := base
	sameIdentityDifferentRaw.Identity.RawName = "friendly.bard"

	first, err := NewPackageEnvelope(base)
	if err != nil {
		t.Fatalf("NewPackageEnvelope(base) error = %v", err)
	}
	second, err := NewPackageEnvelope(sameIdentityDifferentRaw)
	if err != nil {
		t.Fatalf("NewPackageEnvelope(sameIdentityDifferentRaw) error = %v", err)
	}

	if first.StableFactKey != second.StableFactKey {
		t.Fatalf("StableFactKey differs for normalized identity: %q != %q", first.StableFactKey, second.StableFactKey)
	}
	if first.FactID != second.FactID {
		t.Fatalf("FactID differs for normalized identity: %q != %q", first.FactID, second.FactID)
	}
}
