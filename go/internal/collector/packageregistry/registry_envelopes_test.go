package packageregistry

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestPackageDependencyObservationBuildsReportedEnvelope(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 12, 11, 0, 0, 0, time.UTC)
	observation := PackageDependencyObservation{
		Package: PackageIdentity{
			Ecosystem: EcosystemNPM,
			Registry:  "https://registry.npmjs.org/",
			RawName:   "@Example/Web-App",
		},
		Version: "2.4.0",
		Dependency: PackageIdentity{
			Ecosystem: EcosystemNPM,
			Registry:  "registry.npmjs.org",
			RawName:   "Left.Pad",
		},
		Range:               "^1.3.0",
		DependencyType:      "peer",
		TargetFramework:     "browser",
		Marker:              "optional peer",
		Optional:            true,
		ScopeID:             "npm://registry.npmjs.org/@example/web-app",
		GenerationID:        "etag:deps",
		CollectorInstanceID: "public-npm",
		FencingToken:        9,
		ObservedAt:          observedAt,
		SourceURI:           "https://registry.npmjs.org/@example%2fweb-app",
	}

	envelope, err := NewPackageDependencyEnvelope(observation)
	if err != nil {
		t.Fatalf("NewPackageDependencyEnvelope() error = %v", err)
	}

	assertReportedPackageRegistryEnvelope(t, envelope, facts.PackageRegistryPackageDependencyFactKind, facts.PackageRegistryPackageDependencySchemaVersion)
	if !envelope.ObservedAt.Equal(observedAt) {
		t.Fatalf("ObservedAt = %s, want %s", envelope.ObservedAt, observedAt)
	}
	wantPayload := map[string]any{
		"collector_instance_id":  "public-npm",
		"package_id":             "npm://registry.npmjs.org/@example/web-app",
		"version_id":             "npm://registry.npmjs.org/@example/web-app@2.4.0",
		"version":                "2.4.0",
		"dependency_package_id":  "npm://registry.npmjs.org/left.pad",
		"dependency_range":       "^1.3.0",
		"dependency_type":        "peer",
		"target_framework":       "browser",
		"marker":                 "optional peer",
		"optional":               true,
		"excluded":               false,
		"dependency_ecosystem":   string(EcosystemNPM),
		"dependency_registry":    "registry.npmjs.org",
		"dependency_namespace":   "",
		"dependency_normalized":  "left.pad",
		"source_record_version":  "2.4.0",
		"source_record_package":  "npm://registry.npmjs.org/@example/web-app",
		"source_record_dep_kind": "peer",
	}
	for key, want := range wantPayload {
		if got := envelope.Payload[key]; got != want {
			t.Fatalf("Payload[%q] = %#v, want %#v; payload=%#v", key, got, want, envelope.Payload)
		}
	}
}

func TestPackageDependencyStableIDUsesNormalizedDependencyIdentity(t *testing.T) {
	t.Parallel()

	base := PackageDependencyObservation{
		Package: PackageIdentity{
			Ecosystem: EcosystemPyPI,
			Registry:  "https://pypi.org/simple/",
			RawName:   "Friendly_Bard",
		},
		Version: "1.0.0",
		Dependency: PackageIdentity{
			Ecosystem: EcosystemPyPI,
			Registry:  "pypi.org/simple",
			RawName:   "Requests_OAuthlib",
		},
		Range:               ">=2",
		DependencyType:      "runtime",
		ScopeID:             "pypi://pypi.org/simple/friendly-bard",
		GenerationID:        "etag:deps",
		CollectorInstanceID: "public-pypi",
		ObservedAt:          time.Date(2026, 5, 12, 11, 0, 0, 0, time.UTC),
	}
	sameDependencyDifferentDisplay := base
	sameDependencyDifferentDisplay.Dependency.RawName = "requests.oauthlib"

	first, err := NewPackageDependencyEnvelope(base)
	if err != nil {
		t.Fatalf("NewPackageDependencyEnvelope(base) error = %v", err)
	}
	second, err := NewPackageDependencyEnvelope(sameDependencyDifferentDisplay)
	if err != nil {
		t.Fatalf("NewPackageDependencyEnvelope(sameDependencyDifferentDisplay) error = %v", err)
	}

	if first.StableFactKey != second.StableFactKey {
		t.Fatalf("StableFactKey differs for normalized dependency identity: %q != %q", first.StableFactKey, second.StableFactKey)
	}
	if first.FactID != second.FactID {
		t.Fatalf("FactID differs for normalized dependency identity: %q != %q", first.FactID, second.FactID)
	}
}

func TestPackageArtifactObservationBuildsReportedEnvelope(t *testing.T) {
	t.Parallel()

	observation := PackageArtifactObservation{
		Package: PackageIdentity{
			Ecosystem: EcosystemMaven,
			Registry:  "https://repo.maven.apache.org/maven2/",
			Namespace: "org.example",
			RawName:   "core-api",
		},
		Version:             "1.2.3",
		ArtifactKey:         "org/example/core-api/1.2.3/core-api-1.2.3.jar",
		ArtifactType:        "jar",
		ArtifactURL:         "https://token:secret@repo.example/maven/core-api-1.2.3.jar?access_token=secret&checksum=ok",
		ArtifactPath:        "org/example/core-api/1.2.3/core-api-1.2.3.jar",
		SizeBytes:           4096,
		Hashes:              map[string]string{"sha256": "abcdef"},
		Classifier:          "sources",
		PlatformTags:        []string{"jvm"},
		ScopeID:             "maven://repo.maven.apache.org/maven2/org.example:core-api",
		GenerationID:        "sha256:artifact",
		CollectorInstanceID: "central",
		FencingToken:        10,
		ObservedAt:          time.Date(2026, 5, 12, 11, 5, 0, 0, time.UTC),
		SourceURI:           "https://repo.maven.apache.org/maven2/org/example/core-api/1.2.3/",
	}

	envelope, err := NewPackageArtifactEnvelope(observation)
	if err != nil {
		t.Fatalf("NewPackageArtifactEnvelope() error = %v", err)
	}

	assertReportedPackageRegistryEnvelope(t, envelope, facts.PackageRegistryPackageArtifactFactKind, facts.PackageRegistryPackageArtifactSchemaVersion)
	if got := envelope.Payload["version_id"]; got != "maven://repo.maven.apache.org/maven2/org.example:core-api@1.2.3" {
		t.Fatalf("version_id = %#v", got)
	}
	if got := envelope.Payload["artifact_url"]; got != "https://repo.example/maven/core-api-1.2.3.jar?checksum=ok" {
		t.Fatalf("artifact_url = %#v", got)
	}
	if got := envelope.Payload["artifact_key"]; got != observation.ArtifactKey {
		t.Fatalf("artifact_key = %#v", got)
	}
	if got := envelope.Payload["size_bytes"]; got != int64(4096) {
		t.Fatalf("size_bytes = %#v", got)
	}
}

func TestSourceHintObservationBuildsReportedEnvelope(t *testing.T) {
	t.Parallel()

	observation := SourceHintObservation{
		Package: PackageIdentity{
			Ecosystem: EcosystemPyPI,
			Registry:  "https://pypi.org/simple/",
			RawName:   "Friendly_Bard",
		},
		Version:             "0.9.0",
		HintKind:            "repository",
		RawURL:              "https://user:secret@github.com/example/friendly-bard?token=secret",
		NormalizedURL:       "https://github.com/example/friendly-bard",
		ConfidenceReason:    "core-metadata-project-url",
		ScopeID:             "pypi://pypi.org/simple/friendly-bard",
		GenerationID:        "etag:metadata",
		CollectorInstanceID: "public-pypi",
		FencingToken:        11,
		ObservedAt:          time.Date(2026, 5, 12, 11, 10, 0, 0, time.UTC),
		SourceURI:           "https://pypi.org/pypi/friendly-bard/json",
	}

	envelope, err := NewSourceHintEnvelope(observation)
	if err != nil {
		t.Fatalf("NewSourceHintEnvelope() error = %v", err)
	}

	assertReportedPackageRegistryEnvelope(t, envelope, facts.PackageRegistrySourceHintFactKind, facts.PackageRegistrySourceHintSchemaVersion)
	if got := envelope.Payload["raw_url"]; got != "https://github.com/example/friendly-bard" {
		t.Fatalf("raw_url = %#v", got)
	}
	if got := envelope.Payload["normalized_url"]; got != observation.NormalizedURL {
		t.Fatalf("normalized_url = %#v", got)
	}
	if got := envelope.Payload["confidence_reason"]; got != "core-metadata-project-url" {
		t.Fatalf("confidence_reason = %#v", got)
	}
}

func TestRepositoryHostingObservationBuildsReportedEnvelope(t *testing.T) {
	t.Parallel()

	observation := RepositoryHostingObservation{
		Provider:            "artifactory",
		Registry:            "https://jfrog.example/artifactory/",
		Repository:          "libs-release-local",
		RepositoryType:      "local",
		Ecosystem:           EcosystemMaven,
		UpstreamID:          "central-cache",
		UpstreamURL:         "https://user:secret@repo.maven.apache.org/maven2/?api_key=secret",
		ScopeID:             "artifactory://jfrog.example/artifactory/libs-release-local",
		GenerationID:        "topology:1",
		CollectorInstanceID: "jfrog-prod",
		FencingToken:        12,
		ObservedAt:          time.Date(2026, 5, 12, 11, 15, 0, 0, time.UTC),
		SourceURI:           "https://jfrog.example/artifactory/api/repositories/libs-release-local",
	}

	envelope, err := NewRepositoryHostingEnvelope(observation)
	if err != nil {
		t.Fatalf("NewRepositoryHostingEnvelope() error = %v", err)
	}

	assertReportedPackageRegistryEnvelope(t, envelope, facts.PackageRegistryRepositoryHostingFactKind, facts.PackageRegistryRepositoryHostingSchemaVersion)
	if got := envelope.Payload["registry"]; got != "jfrog.example/artifactory" {
		t.Fatalf("registry = %#v", got)
	}
	if got := envelope.Payload["repository"]; got != "libs-release-local" {
		t.Fatalf("repository = %#v", got)
	}
	if got := envelope.Payload["upstream_url"]; got != "https://repo.maven.apache.org/maven2/" {
		t.Fatalf("upstream_url = %#v", got)
	}
}

func TestWarningObservationBuildsReportedEnvelope(t *testing.T) {
	t.Parallel()

	observation := WarningObservation{
		WarningKey:          "metadata-page-4",
		WarningCode:         "partial_generation",
		Severity:            "warning",
		Message:             "skipped https://user:secret@registry.example/pkg?token=secret after rate limit",
		ScopeID:             "npm://registry.example/pkg",
		GenerationID:        "page:4",
		CollectorInstanceID: "private-npm",
		FencingToken:        13,
		ObservedAt:          time.Date(2026, 5, 12, 11, 20, 0, 0, time.UTC),
		SourceURI:           "https://registry.example/pkg",
	}

	envelope, err := NewWarningEnvelope(observation)
	if err != nil {
		t.Fatalf("NewWarningEnvelope() error = %v", err)
	}

	assertReportedPackageRegistryEnvelope(t, envelope, facts.PackageRegistryWarningFactKind, facts.PackageRegistryWarningSchemaVersion)
	if got := envelope.Payload["message"]; got != "skipped https://registry.example/pkg after rate limit" {
		t.Fatalf("message = %#v", got)
	}
	if got := envelope.SourceRef.SourceRecordID; got != "metadata-page-4" {
		t.Fatalf("SourceRecordID = %q", got)
	}
}

func assertReportedPackageRegistryEnvelope(t *testing.T, envelope facts.Envelope, factKind, schemaVersion string) {
	t.Helper()

	if envelope.FactKind != factKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, factKind)
	}
	if envelope.SchemaVersion != schemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, schemaVersion)
	}
	if envelope.CollectorKind != CollectorKind {
		t.Fatalf("CollectorKind = %q, want %q", envelope.CollectorKind, CollectorKind)
	}
	if envelope.SourceConfidence != facts.SourceConfidenceReported {
		t.Fatalf("SourceConfidence = %q, want %q", envelope.SourceConfidence, facts.SourceConfidenceReported)
	}
	if envelope.StableFactKey == "" || envelope.FactID == "" {
		t.Fatalf("stable identifiers must not be blank: %#v", envelope)
	}
	if envelope.SourceRef.SourceSystem != CollectorKind {
		t.Fatalf("SourceRef.SourceSystem = %q, want %q", envelope.SourceRef.SourceSystem, CollectorKind)
	}
	anchors, ok := envelope.Payload["correlation_anchors"].([]string)
	if !ok || len(anchors) == 0 {
		t.Fatalf("correlation_anchors = %#v, want non-empty []string", envelope.Payload["correlation_anchors"])
	}
}
