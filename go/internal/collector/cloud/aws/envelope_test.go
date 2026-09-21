// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestAWSFactBuildersUseReportedConfidence(t *testing.T) {
	t.Parallel()

	boundary := testBoundary(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC))
	ecrBoundary := boundary
	ecrBoundary.ServiceKind = ServiceECR
	route53Boundary := boundary
	route53Boundary.ServiceKind = ServiceRoute53
	builders := map[string]func() (facts.Envelope, error){
		facts.AWSResourceFactKind: func() (facts.Envelope, error) {
			return NewResourceEnvelope(ResourceObservation{
				Boundary:     boundary,
				ARN:          "arn:aws:iam::123456789012:role/eshu-runtime",
				ResourceType: ResourceTypeIAMRole,
			})
		},
		facts.AWSRelationshipFactKind: func() (facts.Envelope, error) {
			return NewRelationshipEnvelope(RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: RelationshipIAMRoleAttachedPolicy,
				SourceARN:        "arn:aws:iam::123456789012:role/eshu-runtime",
				TargetARN:        "arn:aws:iam::aws:policy/ReadOnlyAccess",
			})
		},
		facts.AWSImageReferenceFactKind: func() (facts.Envelope, error) {
			return NewImageReferenceEnvelope(ImageReferenceObservation{
				Boundary:       ecrBoundary,
				RepositoryName: "team/api",
				ImageDigest:    "sha256:image",
			})
		},
		facts.AWSDNSRecordFactKind: func() (facts.Envelope, error) {
			return NewDNSRecordEnvelope(DNSRecordObservation{
				Boundary:     route53Boundary,
				HostedZoneID: "/hostedzone/Z123",
				RecordName:   "api.example.com.",
				RecordType:   "A",
			})
		},
		facts.AWSSecurityGroupRuleFactKind: func() (facts.Envelope, error) {
			ec2Boundary := boundary
			ec2Boundary.ServiceKind = ServiceEC2
			return NewSecurityGroupRuleEnvelope(SecurityGroupRuleObservation{
				Boundary:   ec2Boundary,
				RuleID:     "sgr-123",
				GroupID:    "sg-123",
				IPProtocol: "tcp",
				CIDRIPv4:   "0.0.0.0/0",
			})
		},
		facts.AWSIAMPermissionFactKind: func() (facts.Envelope, error) {
			return NewIAMPermissionEnvelope(IAMPermissionObservation{
				Boundary:      boundary,
				PrincipalARN:  "arn:aws:iam::123456789012:role/eshu-runtime",
				PrincipalType: ResourceTypeIAMRole,
				PolicySource:  IAMPolicySourceInline,
				Effect:        "Allow",
				Actions:       []string{"iam:PassRole"},
				Resources:     []string{"*"},
			})
		},
		facts.AWSWarningFactKind: func() (facts.Envelope, error) {
			return NewWarningEnvelope(WarningObservation{
				Boundary:    boundary,
				WarningKind: WarningAssumeRoleFailed,
			})
		},
	}

	for factKind, build := range builders {
		envelope, err := build()
		if err != nil {
			t.Fatalf("%s builder returned error: %v", factKind, err)
		}
		if envelope.FactKind != factKind {
			t.Fatalf("%s builder FactKind = %q", factKind, envelope.FactKind)
		}
		if envelope.CollectorKind != CollectorKind {
			t.Fatalf("%s CollectorKind = %q, want %q", factKind, envelope.CollectorKind, CollectorKind)
		}
		if envelope.SourceConfidence != facts.SourceConfidenceReported {
			t.Fatalf("%s SourceConfidence = %q, want %q", factKind, envelope.SourceConfidence, facts.SourceConfidenceReported)
		}
		if envelope.SourceRef.SourceSystem != CollectorKind {
			t.Fatalf("%s SourceRef.SourceSystem = %q, want %q", factKind, envelope.SourceRef.SourceSystem, CollectorKind)
		}
	}
}

func TestAWSCloudEmittersUseFactschemaEncode(t *testing.T) {
	t.Parallel()

	files := []string{
		"envelope.go",
		"dns_envelope.go",
		"ec2_posture_envelope.go",
		"iam_permission_envelope.go",
		"posture_envelope.go",
		"resource_policy_permission_envelope.go",
		"s3_external_principal_grant_envelope.go",
		"s3_posture_envelope.go",
		"security_group_rule_envelope.go",
	}
	for _, name := range files {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			content, err := os.ReadFile(filepath.Clean(name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			source := string(content)
			if strings.Contains(source, "payload := map[string]any{") {
				t.Fatalf("%s still builds a final payload map inline; use factschema direct-map Encode instead", name)
			}
			if !strings.Contains(source, "factschema.Encode") {
				t.Fatalf("%s does not call a factschema Encode function", name)
			}
		})
	}
}

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
		WarningKind: WarningAssumeRoleFailed,
		ErrorClass:  "access_denied",
	})
	if err != nil {
		t.Fatalf("NewWarningEnvelope returned error: %v", err)
	}
	second, err := NewWarningEnvelope(WarningObservation{
		Boundary:    boundary,
		WarningKind: WarningAssumeRoleFailed,
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

// TestNewImageReferenceEnvelopeDistinguishesCrossAccountRegistries is the
// codex #5451 P2 regression: two image reference observations sharing the
// SAME boundary account_id/region, repository_name, image_digest, and tag but
// naming DIFFERENT registry accounts (a cross-account ECR pull — the case the
// ECS scanner's parseECRImage specifically parses RegistryID from the image
// host to support) must produce DISTINCT FactIDs and StableFactKeys, so
// ingestion never collapses one of them onto the other and silently drops the
// image reference for the account whose fact loses the collision.
func TestNewImageReferenceEnvelopeDistinguishesCrossAccountRegistries(t *testing.T) {
	boundary := testBoundary(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC))
	boundary.ServiceKind = ServiceECS

	first, err := NewImageReferenceEnvelope(ImageReferenceObservation{
		Boundary:       boundary,
		RepositoryName: "supply-chain-demo",
		RegistryID:     "111111111111",
		ImageDigest:    "sha256:image",
		Tag:            "latest",
	})
	if err != nil {
		t.Fatalf("NewImageReferenceEnvelope() first error = %v, want nil", err)
	}
	second, err := NewImageReferenceEnvelope(ImageReferenceObservation{
		Boundary:       boundary,
		RepositoryName: "supply-chain-demo",
		RegistryID:     "222222222222",
		ImageDigest:    "sha256:image",
		Tag:            "latest",
	})
	if err != nil {
		t.Fatalf("NewImageReferenceEnvelope() second error = %v, want nil", err)
	}

	if first.FactID == second.FactID {
		t.Fatalf("FactID collided across registry accounts: first = %q, second = %q, want distinct", first.FactID, second.FactID)
	}
	if first.StableFactKey == second.StableFactKey {
		t.Fatalf("StableFactKey collided across registry accounts: first = %q, second = %q, want distinct",
			first.StableFactKey, second.StableFactKey)
	}
	assertPayloadString(t, first.Payload, "registry_id", "111111111111")
	assertPayloadString(t, second.Payload, "registry_id", "222222222222")
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

func TestNewDNSRecordEnvelopePreservesAliasAndZoneEvidence(t *testing.T) {
	boundary := testBoundary(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC))
	boundary.ServiceKind = ServiceRoute53
	envelope, err := NewDNSRecordEnvelope(DNSRecordObservation{
		Boundary:          boundary,
		HostedZoneID:      "/hostedzone/Z123",
		HostedZoneName:    "example.com.",
		HostedZonePrivate: true,
		RecordName:        "api.example.com.",
		RecordType:        "A",
		AliasTarget: &DNSAliasTarget{
			DNSName:              "dualstack.api-123.us-east-1.elb.amazonaws.com.",
			HostedZoneID:         "Z35SXDOTRQ7X7K",
			EvaluateTargetHealth: true,
		},
		RoutingPolicy: DNSRoutingPolicy{
			HealthCheckID: "hc-123",
		},
	})
	if err != nil {
		t.Fatalf("NewDNSRecordEnvelope returned error: %v", err)
	}
	if envelope.FactKind != facts.AWSDNSRecordFactKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, facts.AWSDNSRecordFactKind)
	}
	if envelope.SchemaVersion != facts.AWSDNSRecordSchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, facts.AWSDNSRecordSchemaVersion)
	}
	assertPayloadString(t, envelope.Payload, "hosted_zone_id", "/hostedzone/Z123")
	assertPayloadString(t, envelope.Payload, "record_name", "api.example.com.")
	assertPayloadString(t, envelope.Payload, "normalized_record_name", "api.example.com")
	assertPayloadString(t, envelope.Payload, "record_type", "A")
	aliasTarget, ok := envelope.Payload["alias_target"].(map[string]any)
	if !ok {
		t.Fatalf("alias_target = %#v, want map", envelope.Payload["alias_target"])
	}
	if got, _ := aliasTarget["dns_name"].(string); got != "dualstack.api-123.us-east-1.elb.amazonaws.com." {
		t.Fatalf("alias_target.dns_name = %q", got)
	}
	if got, _ := aliasTarget["normalized_dns_name"].(string); got != "dualstack.api-123.us-east-1.elb.amazonaws.com" {
		t.Fatalf("alias_target.normalized_dns_name = %q", got)
	}
	if got, _ := envelope.Payload["hosted_zone_private"].(bool); !got {
		t.Fatalf("hosted_zone_private = %v, want true", got)
	}
	routingPolicy, ok := envelope.Payload["routing_policy"].(map[string]any)
	if !ok {
		t.Fatalf("routing_policy = %#v, want map", envelope.Payload["routing_policy"])
	}
	if got, _ := routingPolicy["health_check_id"].(string); got != "hc-123" {
		t.Fatalf("routing_policy.health_check_id = %q", got)
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
