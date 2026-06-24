// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package opensearch

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsOpenSearchMetadataOnlyFactsAndRelationships(t *testing.T) {
	domainARN := "arn:aws:es:us-east-1:123456789012:domain/orders-search"
	kmsKeyARN := "arn:aws:kms:us-east-1:123456789012:key/orders"
	masterRoleARN := "arn:aws:iam::123456789012:role/orders-search-admin"
	collectionARN := "arn:aws:aoss:us-east-1:123456789012:collection/abc123"
	collectionKMSARN := "arn:aws:kms:us-east-1:123456789012:key/serverless"

	client := fakeClient{
		domains: []Domain{{
			ARN:                     domainARN,
			ID:                      "abc123domain",
			Name:                    "orders-search",
			EngineVersion:           "OpenSearch_2.11",
			State:                   "Active",
			InstanceType:            "r6g.large.search",
			InstanceCount:           3,
			DedicatedMasterEnabled:  true,
			DedicatedMasterType:     "r6g.large.search",
			DedicatedMasterCount:    3,
			ZoneAwarenessEnabled:    true,
			EncryptionAtRestEnabled: true,
			NodeToNodeEncryptionOn:  true,
			KMSKeyID:                kmsKeyARN,
			VPCID:                   "vpc-123",
			SubnetIDs:               []string{"subnet-a", "subnet-b"},
			SecurityGroupIDs:        []string{"sg-123"},
			AvailabilityZones:       []string{"us-east-1a", "us-east-1b"},
			AdvancedSecurityEnabled: true,
			InternalUserDBEnabled:   false,
			SAMLEnabled:             true,
			IAMFederationEnabled:    false,
			MasterUserRoleARNs:      []string{masterRoleARN, masterRoleARN},
			Tags:                    map[string]string{"Environment": "prod"},
		}},
		packages: []Package{{
			ID:            "F12345",
			Name:          "orders-synonyms",
			Type:          "TXT-DICTIONARY",
			Status:        "AVAILABLE",
			Description:   "orders synonyms",
			EngineVersion: "OpenSearch_2.11",
			Owner:         "123456789012",
		}},
		associations: map[string][]PackageAssociation{
			"F12345": {{
				PackageID:         "F12345",
				DomainName:        "orders-search",
				DomainPackageStat: "ACTIVE",
				ReferencePath:     "synonyms/orders",
			}},
		},
		collections: []Collection{{
			ARN:                collectionARN,
			ID:                 "abc123",
			Name:               "orders-vectors",
			Type:               "VECTORSEARCH",
			Status:             "ACTIVE",
			Description:        "orders vector store",
			KMSKeyARN:          collectionKMSARN,
			StandbyReplicas:    "ENABLED",
			DeletionProtection: "DISABLED",
		}, {
			ARN:    "arn:aws:aoss:us-east-1:123456789012:collection/def456",
			ID:     "def456",
			Name:   "events-vectors",
			Type:   "VECTORSEARCH",
			Status: "ACTIVE",
		}},
		securityConfigs: []SecurityConfig{{
			ID:          "saml/orders/okta",
			Type:        "saml",
			Description: "okta saml",
			Version:     "MTcw",
		}},
		// Two managed VPC endpoints alongside two collections would have produced
		// a 2x2 cross-product under the old collection->endpoint emission; the
		// scanner must emit no collection->endpoint edges because neither the
		// collection record nor the endpoint record reports an association.
		vpcEndpoints: []VPCEndpoint{{
			ID:               "vpce-aoss-123",
			Name:             "orders-aoss-endpoint",
			Status:           "ACTIVE",
			VPCID:            "vpc-123",
			SubnetIDs:        []string{"subnet-a"},
			SecurityGroupIDs: []string{"sg-456"},
		}, {
			ID:     "vpce-aoss-456",
			Name:   "events-aoss-endpoint",
			Status: "ACTIVE",
			VPCID:  "vpc-123",
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	domain := resourceByType(t, envelopes, awscloud.ResourceTypeOpenSearchDomain)
	if got, want := domain.Payload["arn"], domainARN; got != want {
		t.Fatalf("domain arn = %#v, want %q", got, want)
	}
	if got, want := domain.Payload["state"], "Active"; got != want {
		t.Fatalf("domain state = %#v, want %q", got, want)
	}
	domainAttributes := attributesOf(t, domain)
	assertAttribute(t, domainAttributes, "engine_version", "OpenSearch_2.11")
	assertAttribute(t, domainAttributes, "instance_type", "r6g.large.search")
	assertAttribute(t, domainAttributes, "instance_count", int32(3))
	assertAttribute(t, domainAttributes, "encryption_at_rest_enabled", true)
	assertAttribute(t, domainAttributes, "node_to_node_encryption_on", true)
	assertAttribute(t, domainAttributes, "advanced_security_enabled", true)
	assertAttribute(t, domainAttributes, "saml_enabled", true)
	assertAttribute(t, domainAttributes, "vpc_id", "vpc-123")
	for _, forbidden := range []string{
		"master_user_password",
		"master_user_options",
		"password",
		"access_policies",
		"endpoint",
		"endpoints",
		"saml_metadata",
		"saml_options",
	} {
		if _, exists := domainAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; OpenSearch domain must stay metadata-only", forbidden)
		}
	}

	pkg := resourceByType(t, envelopes, awscloud.ResourceTypeOpenSearchPackage)
	if got, want := pkg.Payload["name"], "orders-synonyms"; got != want {
		t.Fatalf("package name = %#v, want %q", got, want)
	}
	if got, want := pkg.Payload["state"], "AVAILABLE"; got != want {
		t.Fatalf("package state = %#v, want %q", got, want)
	}
	packageAttributes := attributesOf(t, pkg)
	assertAttribute(t, packageAttributes, "package_type", "TXT-DICTIONARY")
	for _, forbidden := range []string{"package_body", "body", "plugin_properties", "available_package_configuration"} {
		if _, exists := packageAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; OpenSearch package must never carry the body", forbidden)
		}
	}

	collection := resourceByType(t, envelopes, awscloud.ResourceTypeOpenSearchServerlessCollection)
	collectionAttributes := attributesOf(t, collection)
	assertAttribute(t, collectionAttributes, "collection_type", "VECTORSEARCH")
	assertAttribute(t, collectionAttributes, "kms_key_arn", collectionKMSARN)
	for _, forbidden := range []string{"collection_endpoint", "dashboard_endpoint", "endpoint", "saved_objects"} {
		if _, exists := collectionAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; serverless collection must stay metadata-only", forbidden)
		}
	}

	securityConfig := resourceByType(t, envelopes, awscloud.ResourceTypeOpenSearchServerlessSecurityConfig)
	securityConfigAttributes := attributesOf(t, securityConfig)
	assertAttribute(t, securityConfigAttributes, "security_config_type", "saml")
	for _, forbidden := range []string{"saml_options", "metadata", "saml_metadata", "config_body"} {
		if _, exists := securityConfigAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; serverless security config must be summary-only", forbidden)
		}
	}

	vpcEndpoint := resourceByType(t, envelopes, awscloud.ResourceTypeOpenSearchServerlessVPCEndpoint)
	vpcEndpointAttributes := attributesOf(t, vpcEndpoint)
	assertAttribute(t, vpcEndpointAttributes, "vpc_id", "vpc-123")
	assertAttribute(t, vpcEndpointAttributes, "subnet_ids", []string{"subnet-a"})

	assertRelationshipTarget(t, envelopes, awscloud.RelationshipOpenSearchDomainInVPC, "vpc-123")
	assertRelationshipTargetType(t, envelopes, awscloud.RelationshipOpenSearchDomainInVPC, awscloud.ResourceTypeEC2VPC)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipOpenSearchDomainInSubnet, "subnet-a")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipOpenSearchDomainInSubnet, "subnet-b")
	assertRelationshipTargetType(t, envelopes, awscloud.RelationshipOpenSearchDomainInSubnet, awscloud.ResourceTypeEC2Subnet)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipOpenSearchDomainUsesSecurityGroup, "sg-123")
	assertRelationshipTargetType(t, envelopes, awscloud.RelationshipOpenSearchDomainUsesSecurityGroup, awscloud.ResourceTypeEC2SecurityGroup)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipOpenSearchDomainUsesKMSKey, kmsKeyARN)
	assertRelationshipTargetType(t, envelopes, awscloud.RelationshipOpenSearchDomainUsesKMSKey, awscloud.ResourceTypeKMSKey)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipOpenSearchDomainUsesIAMRole, masterRoleARN)
	assertRelationshipTargetType(t, envelopes, awscloud.RelationshipOpenSearchDomainUsesIAMRole, awscloud.ResourceTypeIAMRole)

	// Duplicate role ARNs collapse to one relationship.
	if got := countRelationshipsOfType(envelopes, awscloud.RelationshipOpenSearchDomainUsesIAMRole); got != 1 {
		t.Fatalf("iam-role relationship count = %d, want 1 (duplicates deduplicated)", got)
	}

	// Package-to-domain resolves to the domain ARN.
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipOpenSearchPackageAssociatedWithDomain, domainARN)
	assertRelationshipTargetType(t, envelopes, awscloud.RelationshipOpenSearchPackageAssociatedWithDomain, awscloud.ResourceTypeOpenSearchDomain)
	assertRelationshipSourceRecordID(t, envelopes, awscloud.RelationshipOpenSearchPackageAssociatedWithDomain, "F12345->opensearch_package_associated_with_domain:"+domainARN)

	assertRelationshipTarget(t, envelopes, awscloud.RelationshipOpenSearchCollectionUsesKMSKey, collectionKMSARN)
	assertRelationshipTargetType(t, envelopes, awscloud.RelationshipOpenSearchCollectionUsesKMSKey, awscloud.ResourceTypeKMSKey)

	// Serverless does not bind a collection to a managed VPC endpoint in the
	// collection record, and the endpoint record reports no collection, so the
	// scanner emits no collection->endpoint edge rather than a misleading
	// cross-product over every managed endpoint in the account.
	if got := countRelationshipsOfType(envelopes, "opensearch_collection_uses_vpc_endpoint"); got != 0 {
		t.Fatalf("collection->vpc-endpoint relationship count = %d, want 0 (no reliable association join key)", got)
	}

	// Every relationship must carry a non-empty target_type to graph-join.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["target_type"].(string); got == "" {
			t.Fatalf("relationship %#v has empty target_type", envelope.Payload["relationship_type"])
		}
	}
}

func TestScannerSkipsPackageRelationshipsWithoutDomain(t *testing.T) {
	client := fakeClient{
		packages: []Package{{ID: "F1", Name: "p1", Status: "AVAILABLE"}},
		associations: map[string][]PackageAssociation{
			"F1": {{PackageID: "F1", DomainName: ""}},
		},
	}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes); got != 0 {
		t.Fatalf("relationship count = %d, want 0 without a domain target", got)
	}
}

func TestScannerKeepsNonARNKMSIdentifierWithoutTargetARN(t *testing.T) {
	client := fakeClient{domains: []Domain{{
		ARN:      "arn:aws:es:us-east-1:123456789012:domain/orders",
		Name:     "orders",
		KMSKeyID: "alias/orders",
	}}}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	relationship := relationshipByType(t, envelopes, awscloud.RelationshipOpenSearchDomainUsesKMSKey)
	if got, want := relationship.Payload["target_resource_id"], "alias/orders"; got != want {
		t.Fatalf("target_resource_id = %#v, want %q", got, want)
	}
	if got := relationship.Payload["target_arn"]; got != "" {
		t.Fatalf("target_arn = %#v, want empty for non-ARN KMS identifier", got)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceEKS
	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerDefaultsServiceKindWhenEmpty(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = ""
	envelopes, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("envelopes = %d, want 0 for empty input", len(envelopes))
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{Client: nil}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client required")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceOpenSearch,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:opensearch:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 28, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	domains         []Domain
	packages        []Package
	associations    map[string][]PackageAssociation
	collections     []Collection
	securityConfigs []SecurityConfig
	vpcEndpoints    []VPCEndpoint
}

func (c fakeClient) ListDomains(context.Context) ([]Domain, error) { return c.domains, nil }

func (c fakeClient) ListPackages(context.Context) ([]Package, error) { return c.packages, nil }

func (c fakeClient) ListPackageAssociations(_ context.Context, packageID string) ([]PackageAssociation, error) {
	return c.associations[packageID], nil
}

func (c fakeClient) ListCollections(context.Context) ([]Collection, error) { return c.collections, nil }

func (c fakeClient) ListSecurityConfigs(context.Context) ([]SecurityConfig, error) {
	return c.securityConfigs, nil
}

func (c fakeClient) ListVPCEndpoints(context.Context) ([]VPCEndpoint, error) {
	return c.vpcEndpoints, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q", resourceType)
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q", relationshipType)
	return facts.Envelope{}
}

func assertRelationshipTarget(t *testing.T, envelopes []facts.Envelope, relationshipType, targetID string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != relationshipType {
			continue
		}
		if got, _ := envelope.Payload["target_resource_id"].(string); got == targetID {
			return
		}
		if got, _ := envelope.Payload["target_arn"].(string); got == targetID {
			return
		}
	}
	t.Fatalf("missing relationship %q target %q", relationshipType, targetID)
}

func assertRelationshipTargetType(t *testing.T, envelopes []facts.Envelope, relationshipType, targetType string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != relationshipType {
			continue
		}
		if got, _ := envelope.Payload["target_type"].(string); got == targetType {
			return
		}
	}
	t.Fatalf("relationship %q missing target_type %q", relationshipType, targetType)
}

func assertRelationshipSourceRecordID(t *testing.T, envelopes []facts.Envelope, relationshipType, want string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != relationshipType {
			continue
		}
		if envelope.SourceRef.SourceRecordID == want {
			return
		}
	}
	t.Fatalf("relationship %q SourceRecordID %q not found", relationshipType, want)
}

func countRelationships(envelopes []facts.Envelope) int {
	var count int
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			count++
		}
	}
	return count
}

func countRelationshipsOfType(envelopes []facts.Envelope, relationshipType string) int {
	var count int
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			count++
		}
	}
	return count
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func assertAttribute(t *testing.T, attributes map[string]any, key string, want any) {
	t.Helper()
	got, exists := attributes[key]
	if !exists {
		t.Fatalf("missing attribute %q in %#v", key, attributes)
	}
	if !valuesEqual(got, want) {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}

func valuesEqual(got any, want any) bool {
	switch want := want.(type) {
	case []string:
		gotStrings, ok := got.([]string)
		if !ok || len(gotStrings) != len(want) {
			return false
		}
		for i := range want {
			if gotStrings[i] != want[i] {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}
