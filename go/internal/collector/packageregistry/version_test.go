package packageregistry

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestPackageVersionObservationBuildsReportedVersionEnvelope(t *testing.T) {
	t.Parallel()

	publishedAt := time.Date(2026, 5, 11, 9, 15, 0, 0, time.UTC)
	observation := PackageVersionObservation{
		Package: PackageIdentity{
			Ecosystem: EcosystemMaven,
			Registry:  "https://repo.maven.apache.org/maven2/",
			Namespace: "org.apache.maven",
			RawName:   "maven-core",
		},
		Version:             "3.9.9",
		ScopeID:             "maven://repo.maven.apache.org/maven2/org.apache.maven:maven-core",
		GenerationID:        "sha256:metadata",
		CollectorInstanceID: "central",
		FencingToken:        7,
		ObservedAt:          publishedAt,
		PublishedAt:         publishedAt,
		Deprecated:          true,
		ArtifactURLs: []string{
			"https://repo.maven.apache.org/maven2/org/apache/maven/maven-core/3.9.9/maven-core-3.9.9.jar",
		},
		Checksums: map[string]string{
			"sha1": "0123456789abcdef",
		},
		SourceURI: "https://repo.maven.apache.org/maven2/org/apache/maven/maven-core/maven-metadata.xml",
	}

	envelope, err := NewPackageVersionEnvelope(observation)
	if err != nil {
		t.Fatalf("NewPackageVersionEnvelope() error = %v", err)
	}

	if envelope.FactKind != facts.PackageRegistryPackageVersionFactKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, facts.PackageRegistryPackageVersionFactKind)
	}
	if envelope.SchemaVersion != facts.PackageRegistryPackageVersionSchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, facts.PackageRegistryPackageVersionSchemaVersion)
	}
	if envelope.SourceConfidence != facts.SourceConfidenceReported {
		t.Fatalf("SourceConfidence = %q, want %q", envelope.SourceConfidence, facts.SourceConfidenceReported)
	}
	if envelope.SourceRef.SourceRecordID != "maven://repo.maven.apache.org/maven2/org.apache.maven:maven-core@3.9.9" {
		t.Fatalf("SourceRecordID = %q", envelope.SourceRef.SourceRecordID)
	}

	wantPayload := map[string]any{
		"collector_instance_id": "central",
		"ecosystem":             string(EcosystemMaven),
		"registry":              "repo.maven.apache.org/maven2",
		"package_id":            "maven://repo.maven.apache.org/maven2/org.apache.maven:maven-core",
		"version_id":            "maven://repo.maven.apache.org/maven2/org.apache.maven:maven-core@3.9.9",
		"version":               "3.9.9",
		"is_yanked":             false,
		"is_unlisted":           false,
		"is_deprecated":         true,
		"is_retracted":          false,
	}
	for key, want := range wantPayload {
		if got := envelope.Payload[key]; got != want {
			t.Fatalf("Payload[%q] = %#v, want %#v; payload=%#v", key, got, want, envelope.Payload)
		}
	}
	if got := envelope.Payload["artifact_urls"].([]string); len(got) != 1 || got[0] != observation.ArtifactURLs[0] {
		t.Fatalf("artifact_urls = %#v, want %#v", got, observation.ArtifactURLs)
	}
	if got := envelope.Payload["checksums"].(map[string]string); got["sha1"] != "0123456789abcdef" {
		t.Fatalf("checksums = %#v", got)
	}
}

func TestPackageVersionObservationRequiresVersion(t *testing.T) {
	t.Parallel()

	_, err := NewPackageVersionEnvelope(PackageVersionObservation{
		Package: PackageIdentity{
			Ecosystem: EcosystemNPM,
			Registry:  "registry.npmjs.org",
			RawName:   "react",
		},
		ScopeID:             "npm://registry.npmjs.org/react",
		GenerationID:        "etag:abc123",
		CollectorInstanceID: "public-npm",
	})
	if err == nil {
		t.Fatal("NewPackageVersionEnvelope() error = nil, want missing version error")
	}
}

func TestPackageVersionFactIDUsesGenerationBoundary(t *testing.T) {
	t.Parallel()

	base := PackageVersionObservation{
		Package: PackageIdentity{
			Ecosystem: EcosystemNPM,
			Registry:  "registry.npmjs.org",
			RawName:   "react",
		},
		Version:             "19.0.0",
		ScopeID:             "npm://registry.npmjs.org/react",
		GenerationID:        "etag:abc123",
		CollectorInstanceID: "public-npm",
	}
	next := base
	next.GenerationID = "etag:def456"

	first, err := NewPackageVersionEnvelope(base)
	if err != nil {
		t.Fatalf("NewPackageVersionEnvelope(base) error = %v", err)
	}
	second, err := NewPackageVersionEnvelope(next)
	if err != nil {
		t.Fatalf("NewPackageVersionEnvelope(next) error = %v", err)
	}
	if first.StableFactKey != second.StableFactKey {
		t.Fatalf("StableFactKey changed across generations: %q != %q", first.StableFactKey, second.StableFactKey)
	}
	if first.FactID == second.FactID {
		t.Fatalf("FactID did not include generation boundary: %q", first.FactID)
	}
}
