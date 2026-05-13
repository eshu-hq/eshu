package awscloud

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestNewResourceEnvelopeCarriesAWSProvenance(t *testing.T) {
	observedAt := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	envelope, err := NewResourceEnvelope(ResourceObservation{
		Boundary:     testBoundary(observedAt),
		ARN:          "arn:aws:iam::123456789012:role/eshu-runtime",
		ResourceType: ResourceTypeIAMRole,
		Name:         "eshu-runtime",
		Tags:         map[string]string{"Environment": "prod"},
		Attributes:   map[string]any{"path": "/service/"},
	})
	if err != nil {
		t.Fatalf("NewResourceEnvelope returned error: %v", err)
	}

	if envelope.FactKind != facts.AWSResourceFactKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, facts.AWSResourceFactKind)
	}
	if envelope.SchemaVersion != facts.AWSResourceSchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, facts.AWSResourceSchemaVersion)
	}
	if envelope.CollectorKind != CollectorKind {
		t.Fatalf("CollectorKind = %q, want %q", envelope.CollectorKind, CollectorKind)
	}
	if envelope.SourceConfidence != facts.SourceConfidenceReported {
		t.Fatalf("SourceConfidence = %q, want %q", envelope.SourceConfidence, facts.SourceConfidenceReported)
	}
	if envelope.FencingToken != 77 {
		t.Fatalf("FencingToken = %d, want 77", envelope.FencingToken)
	}
	assertPayloadString(t, envelope.Payload, "account_id", "123456789012")
	assertPayloadString(t, envelope.Payload, "region", "aws-global")
	assertPayloadString(t, envelope.Payload, "service_kind", ServiceIAM)
	assertPayloadString(t, envelope.Payload, "resource_type", ResourceTypeIAMRole)
	assertPayloadString(t, envelope.Payload, "arn", "arn:aws:iam::123456789012:role/eshu-runtime")
	if got := envelope.SourceRef.SourceSystem; got != CollectorKind {
		t.Fatalf("SourceRef.SourceSystem = %q, want %q", got, CollectorKind)
	}
}

func TestNewRelationshipEnvelopeRequiresSourceAndTarget(t *testing.T) {
	_, err := NewRelationshipEnvelope(RelationshipObservation{
		Boundary:         testBoundary(time.Now()),
		RelationshipType: RelationshipIAMRoleAttachedPolicy,
		SourceARN:        "arn:aws:iam::123456789012:role/eshu-runtime",
	})
	if err == nil {
		t.Fatal("NewRelationshipEnvelope returned nil error, want missing target error")
	}
}

func TestNewResourceEnvelopeRequiresPositiveFencingToken(t *testing.T) {
	boundary := testBoundary(time.Now())
	boundary.FencingToken = 0
	_, err := NewResourceEnvelope(ResourceObservation{
		Boundary:     boundary,
		ARN:          "arn:aws:iam::123456789012:role/app",
		ResourceType: ResourceTypeIAMRole,
	})
	if err == nil {
		t.Fatalf("NewResourceEnvelope() error = nil, want fencing token error")
	}
}

func TestNewWarningEnvelopeUsesGenerationScopedIdentity(t *testing.T) {
	boundary := testBoundary(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC))
	first, err := NewWarningEnvelope(WarningObservation{
		Boundary:    boundary,
		WarningKind: "assumerole_failed",
		ErrorClass:  "access_denied",
	})
	if err != nil {
		t.Fatalf("NewWarningEnvelope returned error: %v", err)
	}
	second, err := NewWarningEnvelope(WarningObservation{
		Boundary:    boundary,
		WarningKind: "assumerole_failed",
		ErrorClass:  "access_denied",
		Message:     "different redacted detail",
	})
	if err != nil {
		t.Fatalf("NewWarningEnvelope returned second error: %v", err)
	}
	if first.FactID != second.FactID {
		t.Fatalf("warning FactID changed with message: %q != %q", first.FactID, second.FactID)
	}
}

func TestNewImageReferenceEnvelopeCarriesDigestTagAndRepository(t *testing.T) {
	boundary := testBoundary(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC))
	boundary.ServiceKind = ServiceECR
	envelope, err := NewImageReferenceEnvelope(ImageReferenceObservation{
		Boundary:          boundary,
		RepositoryARN:     "arn:aws:ecr:us-east-1:123456789012:repository/team/api",
		RepositoryName:    "team/api",
		RegistryID:        "123456789012",
		ImageDigest:       "sha256:image",
		ManifestDigest:    "sha256:manifest",
		Tag:               "latest",
		PushedAt:          time.Date(2026, 5, 13, 11, 0, 0, 0, time.UTC),
		ImageSizeInBytes:  1234,
		ManifestMediaType: "application/vnd.oci.image.manifest.v1+json",
	})
	if err != nil {
		t.Fatalf("NewImageReferenceEnvelope returned error: %v", err)
	}
	if envelope.FactKind != facts.AWSImageReferenceFactKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, facts.AWSImageReferenceFactKind)
	}
	if envelope.SchemaVersion != facts.AWSImageReferenceSchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, facts.AWSImageReferenceSchemaVersion)
	}
	assertPayloadString(t, envelope.Payload, "repository_name", "team/api")
	assertPayloadString(t, envelope.Payload, "image_digest", "sha256:image")
	assertPayloadString(t, envelope.Payload, "manifest_digest", "sha256:manifest")
	assertPayloadString(t, envelope.Payload, "tag", "latest")
	assertPayloadString(t, envelope.Payload, "repository_arn", "arn:aws:ecr:us-east-1:123456789012:repository/team/api")
}

func TestNewImageReferenceEnvelopeRequiresDigest(t *testing.T) {
	boundary := testBoundary(time.Now())
	boundary.ServiceKind = ServiceECR
	_, err := NewImageReferenceEnvelope(ImageReferenceObservation{
		Boundary:       boundary,
		RepositoryName: "team/api",
	})
	if err == nil {
		t.Fatalf("NewImageReferenceEnvelope() error = nil, want missing digest error")
	}
}

func testBoundary(observedAt time.Time) Boundary {
	return Boundary{
		AccountID:           "123456789012",
		Region:              "aws-global",
		ServiceKind:         ServiceIAM,
		ScopeID:             "aws:123456789012:aws-global",
		GenerationID:        "aws:123456789012:aws-global:iam:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        77,
		ObservedAt:          observedAt,
	}
}

func assertPayloadString(t *testing.T, payload map[string]any, key string, want string) {
	t.Helper()
	got, ok := payload[key].(string)
	if !ok {
		t.Fatalf("payload[%q] = %T, want string", key, payload[key])
	}
	if got != want {
		t.Fatalf("payload[%q] = %q, want %q", key, got, want)
	}
}
