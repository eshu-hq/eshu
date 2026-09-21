// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeartifact

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsDomainsRepositoriesAndRelationships(t *testing.T) {
	client := fakeClient{
		domains: []Domain{{
			Name:            "acme",
			ARN:             "arn:aws:codeartifact:us-east-1:123456789012:domain/acme",
			Owner:           "123456789012",
			EncryptionKey:   "arn:aws:kms:us-east-1:123456789012:key/1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d",
			S3BucketARN:     "arn:aws:s3:::assets-acme",
			RepositoryCount: 2,
			AssetSizeBytes:  4096,
			Status:          "Active",
			CreatedTime:     time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC),
		}},
		repositories: []Repository{{
			Name:                 "team-npm",
			ARN:                  "arn:aws:codeartifact:us-east-1:123456789012:repository/acme/team-npm",
			DomainName:           "acme",
			DomainOwner:          "123456789012",
			AdministratorAccount: "123456789012",
			Description:          "team npm proxy",
			CreatedTime:          time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC),
			ExternalConnections: []ExternalConnection{{
				Name:          "public:npmjs",
				PackageFormat: "npm",
				Status:        "Available",
			}},
			Upstreams: []string{"shared-npm", "shared-npm"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	counts := factKindCounts(envelopes)
	if counts[facts.AWSResourceFactKind] != 2 {
		t.Fatalf("aws_resource count = %d, want 2", counts[facts.AWSResourceFactKind])
	}
	// repo->domain (1) + domain->kms (1) + repo->upstream (1, deduped) +
	// repo->external-connection (1) = 4 relationships.
	if counts[facts.AWSRelationshipFactKind] != 4 {
		t.Fatalf("aws_relationship count = %d, want 4", counts[facts.AWSRelationshipFactKind])
	}

	assertResourceType(t, envelopes, awscloud.ResourceTypeCodeArtifactDomain)
	assertResourceType(t, envelopes, awscloud.ResourceTypeCodeArtifactRepository)

	repoID := "arn:aws:codeartifact:us-east-1:123456789012:repository/acme/team-npm"

	// repository -> domain: target_type aws_codeartifact_domain, target keyed by
	// the domain name the domain resource publishes as its resource_id.
	assertRelationship(t, envelopes, relationshipMatch{
		relType:    awscloud.RelationshipCodeArtifactRepositoryInDomain,
		sourceID:   repoID,
		targetID:   "acme",
		targetType: awscloud.ResourceTypeCodeArtifactDomain,
	})
	// domain -> KMS key: target_type aws_kms_key, ARN-keyed so it joins the KMS
	// key node directly.
	assertRelationship(t, envelopes, relationshipMatch{
		relType:    awscloud.RelationshipCodeArtifactDomainUsesKMSKey,
		sourceID:   "acme",
		targetID:   "arn:aws:kms:us-east-1:123456789012:key/1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d",
		targetARN:  "arn:aws:kms:us-east-1:123456789012:key/1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d",
		targetType: awscloud.ResourceTypeKMSKey,
	})
	// repository -> upstream repository: target_type aws_codeartifact_repository,
	// keyed by "<domain>/<upstream>" within the same domain.
	assertRelationship(t, envelopes, relationshipMatch{
		relType:    awscloud.RelationshipCodeArtifactRepositoryUpstreamRepository,
		sourceID:   repoID,
		targetID:   "acme/shared-npm",
		targetType: awscloud.ResourceTypeCodeArtifactRepository,
	})
	// repository -> external connection: labeled non-AWS public-registry target.
	assertRelationship(t, envelopes, relationshipMatch{
		relType:    awscloud.RelationshipCodeArtifactRepositoryExternalConnection,
		sourceID:   repoID,
		targetID:   "public:npmjs",
		targetType: externalConnectionTargetType,
	})
}

// TestScannerEmittedRelationshipsSatisfyGraphJoinGuard runs every emitted edge
// through the relguard runtime contract: non-empty target_type, a known target
// family, and ARN-vs-name join-mode consistency.
func TestScannerEmittedRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	domain := Domain{
		Name:          "acme",
		ARN:           "arn:aws:codeartifact:us-east-1:123456789012:domain/acme",
		EncryptionKey: "arn:aws:kms:us-east-1:123456789012:key/1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d",
	}
	repository := Repository{
		Name:                "team-npm",
		ARN:                 "arn:aws:codeartifact:us-east-1:123456789012:repository/acme/team-npm",
		DomainName:          "acme",
		ExternalConnections: []ExternalConnection{{Name: "public:npmjs", PackageFormat: "npm", Status: "Available"}},
		Upstreams:           []string{"shared-npm"},
	}

	var observations []awscloud.RelationshipObservation
	if rel := domainKMSKeyRelationship(boundary, domain); rel != nil {
		observations = append(observations, *rel)
	}
	if rel := repositoryInDomainRelationship(boundary, repository); rel != nil {
		observations = append(observations, *rel)
	}
	observations = append(observations, upstreamRepositoryRelationships(boundary, repository)...)
	observations = append(observations, externalConnectionRelationships(boundary, repository)...)

	if len(observations) != 4 {
		t.Fatalf("len(observations) = %d, want 4", len(observations))
	}
	relguard.AssertObservations(t, observations...)
}

// TestScannerOmitsKMSEdgeWhenDomainReportsNoKey proves the domain->KMS edge is
// emitted only when AWS reports an ARN-shaped encryption key, so the edge never
// dangles against a missing key node.
func TestScannerOmitsKMSEdgeWhenDomainReportsNoKey(t *testing.T) {
	client := fakeClient{
		domains: []Domain{{
			Name: "no-key",
			ARN:  "arn:aws:codeartifact:us-east-1:123456789012:domain/no-key",
		}},
	}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	counts := factKindCounts(envelopes)
	if counts[facts.AWSResourceFactKind] != 1 {
		t.Fatalf("aws_resource count = %d, want 1", counts[facts.AWSResourceFactKind])
	}
	if counts[facts.AWSRelationshipFactKind] != 0 {
		t.Fatalf("aws_relationship count = %d, want 0", counts[facts.AWSRelationshipFactKind])
	}
}

// TestScannerEmitsResourcesWithoutClientPackagePayloads proves the scanner-owned
// Client interface exposes no method that reads a package version or asset, so
// package contents are unreachable by construction.
func TestScannerEmitsResourcesWithoutClientPackagePayloads(t *testing.T) {
	assertClientHasNoPayloadOrMutationMethods(t)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceECR
	_, err := Scanner{Client: fakeClient{}}.Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := Scanner{}.Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client required")
	}
}

// assertClientHasNoPayloadOrMutationMethods reflects over the scanner-owned
// Client interface and fails if any method reads a package payload or mutates a
// CodeArtifact resource. The scanner can only call what Client exposes, so an
// absent method is a contract the package cannot break at runtime.
func assertClientHasNoPayloadOrMutationMethods(t *testing.T) {
	t.Helper()
	forbiddenSubstrings := []string{
		"package", "asset", "version", "readme", "dependencies",
		"publish", "put", "copy", "create", "update", "delete",
		"dispose", "associate", "disassociate", "tag", "untag",
	}
	iface := reflect.TypeOf((*Client)(nil)).Elem()
	if iface.NumMethod() == 0 {
		t.Fatalf("Client interface has no methods; expected the CodeArtifact read surface")
	}
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		lower := strings.ToLower(name)
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(lower, banned) {
				t.Fatalf("Client exposes method %q matching forbidden token %q; the CodeArtifact scanner is metadata-only and never reads package payloads or mutates resources", name, banned)
			}
		}
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Describe") {
			t.Fatalf("Client method %q is neither a List nor Describe read", name)
		}
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceCodeArtifact,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:codeartifact:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	domains      []Domain
	repositories []Repository
}

func (c fakeClient) ListDomains(context.Context) ([]Domain, error) {
	return c.domains, nil
}

func (c fakeClient) ListRepositories(context.Context) ([]Repository, error) {
	return c.repositories, nil
}

func factKindCounts(envelopes []facts.Envelope) map[string]int {
	counts := make(map[string]int)
	for _, envelope := range envelopes {
		counts[envelope.FactKind]++
	}
	return counts
}

func assertResourceType(t *testing.T, envelopes []facts.Envelope, resourceType string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return
		}
	}
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
}

type relationshipMatch struct {
	relType    string
	sourceID   string
	targetID   string
	targetARN  string
	targetType string
}

func assertRelationship(t *testing.T, envelopes []facts.Envelope, want relationshipMatch) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != want.relType {
			continue
		}
		if got, _ := envelope.Payload["source_resource_id"].(string); got != want.sourceID {
			continue
		}
		if got, _ := envelope.Payload["target_resource_id"].(string); got != want.targetID {
			continue
		}
		if got, _ := envelope.Payload["target_type"].(string); got != want.targetType {
			t.Fatalf("relationship %q target_type = %q, want %q", want.relType, got, want.targetType)
		}
		if got, _ := envelope.Payload["target_arn"].(string); got != want.targetARN {
			t.Fatalf("relationship %q target_arn = %q, want %q", want.relType, got, want.targetARN)
		}
		return
	}
	t.Fatalf("missing relationship type=%q source=%q target=%q in %#v", want.relType, want.sourceID, want.targetID, envelopes)
}
