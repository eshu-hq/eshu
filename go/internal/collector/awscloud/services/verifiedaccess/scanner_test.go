// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package verifiedaccess

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testInstanceID      = "vai-0123456789abcdef0"
	testGroupID         = "vagr-0123456789abcdef0"
	testEndpointID      = "vae-0123456789abcdef0"
	testTrustProviderID = "vatp-0123456789abcdef0"
	testGroupARN        = "arn:aws:ec2:us-east-1:123456789012:verified-access-group/vagr-0123456789abcdef0"
	testCertARN         = "arn:aws:acm:us-east-1:123456789012:certificate/abcd1234-12ab-34cd-56ef-1234567890ab"
	testSubnetID        = "subnet-0123456789abcdef0"
	testSecurityGroupID = "sg-0123456789abcdef0"
)

func wantInstanceARN() string {
	return "arn:aws:ec2:us-east-1:123456789012:verified-access-instance/" + testInstanceID
}

func wantEndpointARN() string {
	return "arn:aws:ec2:us-east-1:123456789012:verified-access-endpoint/" + testEndpointID
}

func wantTrustProviderARN() string {
	return "arn:aws:ec2:us-east-1:123456789012:verified-access-trust-provider/" + testTrustProviderID
}

func fullSnapshot() Snapshot {
	return Snapshot{
		Instances: []Instance{{
			ID:                        testInstanceID,
			Description:               "prod access",
			FIPSEnabled:               true,
			CustomerManagedKeyEnabled: true,
			TrustProviderIDs:          []string{testTrustProviderID},
			CreationTime:              time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
			Tags:                      map[string]string{"Environment": "prod"},
		}},
		TrustProviders: []TrustProvider{{
			ID:                    testTrustProviderID,
			TrustProviderType:     "user",
			UserTrustProviderType: "iam-identity-center",
			PolicyReferenceName:   "idc",
			CreationTime:          time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		}},
		Groups: []Group{{
			ARN:          testGroupARN,
			ID:           testGroupID,
			InstanceID:   testInstanceID,
			Owner:        "123456789012",
			CreationTime: time.Date(2026, 5, 1, 12, 5, 0, 0, time.UTC),
			Tags:         map[string]string{"Team": "platform"},
		}},
		Endpoints: []Endpoint{{
			ID:                   testEndpointID,
			GroupID:              testGroupID,
			InstanceID:           testInstanceID,
			EndpointType:         "load-balancer",
			AttachmentType:       "vpc",
			ApplicationDomain:    "app.example.com",
			EndpointDomain:       "vae-xyz.edge.vai.us-east-1.amazonaws.com",
			Status:               "active",
			DomainCertificateARN: testCertARN,
			SubnetIDs:            []string{testSubnetID},
			SecurityGroupIDs:     []string{testSecurityGroupID},
			LoadBalancerARN:      "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/web/abc",
			CreationTime:         time.Date(2026, 5, 1, 12, 10, 0, 0, time.UTC),
			Tags:                 map[string]string{"App": "web"},
		}},
	}
}

func TestScannerEmitsVerifiedAccessMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: fullSnapshot()}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Instance node, keyed by the synthesized partition-aware ARN.
	instance := resourceByType(t, envelopes, awscloud.ResourceTypeVerifiedAccessInstance)
	if got, want := instance.Payload["resource_id"], wantInstanceARN(); got != want {
		t.Fatalf("instance resource_id = %#v, want %q", got, want)
	}
	if got, want := instance.Payload["arn"], wantInstanceARN(); got != want {
		t.Fatalf("instance arn = %#v, want %q", got, want)
	}
	instAttrs := attributesOf(t, instance)
	assertAttribute(t, instAttrs, "instance_id", testInstanceID)
	assertAttribute(t, instAttrs, "fips_enabled", true)
	assertAttribute(t, instAttrs, "customer_managed_key_enabled", true)
	assertAttribute(t, instAttrs, "trust_provider_ids", []string{testTrustProviderID})

	// Trust provider node, keyed by the synthesized partition-aware ARN.
	trustProvider := resourceByType(t, envelopes, awscloud.ResourceTypeVerifiedAccessTrustProvider)
	if got, want := trustProvider.Payload["resource_id"], wantTrustProviderARN(); got != want {
		t.Fatalf("trust provider resource_id = %#v, want %q", got, want)
	}
	tpAttrs := attributesOf(t, trustProvider)
	assertAttribute(t, tpAttrs, "user_trust_provider_type", "iam-identity-center")
	assertAttribute(t, tpAttrs, "uses_iam_identity_center", true)

	// Group node, keyed by the API-reported ARN.
	group := resourceByType(t, envelopes, awscloud.ResourceTypeVerifiedAccessGroup)
	if got, want := group.Payload["resource_id"], testGroupARN; got != want {
		t.Fatalf("group resource_id = %#v, want %q", got, want)
	}

	// Endpoint node.
	endpoint := resourceByType(t, envelopes, awscloud.ResourceTypeVerifiedAccessEndpoint)
	if got, want := endpoint.Payload["resource_id"], wantEndpointARN(); got != want {
		t.Fatalf("endpoint resource_id = %#v, want %q", got, want)
	}
	if got, want := endpoint.Payload["state"], "active"; got != want {
		t.Fatalf("endpoint state = %#v, want %q", got, want)
	}
	epAttrs := attributesOf(t, endpoint)
	assertAttribute(t, epAttrs, "endpoint_type", "load-balancer")
	assertAttribute(t, epAttrs, "subnet_ids", []string{testSubnetID})
	assertAttribute(t, epAttrs, "security_group_ids", []string{testSecurityGroupID})

	// group -> instance edge, keyed by the instance ARN the instance node publishes.
	groupInInstance := relationshipByType(t, envelopes, awscloud.RelationshipVerifiedAccessGroupInInstance)
	assertEdgeTarget(t, groupInInstance, awscloud.ResourceTypeVerifiedAccessInstance, wantInstanceARN())
	if got, want := groupInInstance.Payload["source_resource_id"], testGroupARN; got != want {
		t.Fatalf("group->instance source_resource_id = %#v, want %q", got, want)
	}

	// endpoint -> group edge, keyed by the group ARN the group node publishes.
	endpointInGroup := relationshipByType(t, envelopes, awscloud.RelationshipVerifiedAccessEndpointInGroup)
	assertEdgeTarget(t, endpointInGroup, awscloud.ResourceTypeVerifiedAccessGroup, testGroupARN)

	// instance -> trust provider edge.
	instanceTrust := relationshipByType(t, envelopes, awscloud.RelationshipVerifiedAccessInstanceUsesTrustProvider)
	assertEdgeTarget(t, instanceTrust, awscloud.ResourceTypeVerifiedAccessTrustProvider, wantTrustProviderARN())
	if got, want := instanceTrust.Payload["source_resource_id"], wantInstanceARN(); got != want {
		t.Fatalf("instance->trust source_resource_id = %#v, want %q", got, want)
	}

	// endpoint -> subnet edge, keyed by the bare subnet id the EC2 scanner publishes.
	endpointSubnet := relationshipByType(t, envelopes, awscloud.RelationshipVerifiedAccessEndpointUsesSubnet)
	assertEdgeTarget(t, endpointSubnet, awscloud.ResourceTypeEC2Subnet, testSubnetID)
	if got := endpointSubnet.Payload["target_arn"]; got != "" {
		t.Fatalf("endpoint->subnet target_arn = %#v, want empty for bare id", got)
	}

	// endpoint -> security group edge, keyed by the bare sg id.
	endpointSG := relationshipByType(t, envelopes, awscloud.RelationshipVerifiedAccessEndpointUsesSecurityGroup)
	assertEdgeTarget(t, endpointSG, awscloud.ResourceTypeEC2SecurityGroup, testSecurityGroupID)

	// endpoint -> ACM certificate edge, keyed by the certificate ARN.
	endpointCert := relationshipByType(t, envelopes, awscloud.RelationshipVerifiedAccessEndpointUsesACMCertificate)
	assertEdgeTarget(t, endpointCert, awscloud.ResourceTypeACMCertificate, testCertARN)
	if got, want := endpointCert.Payload["target_arn"], testCertARN; got != want {
		t.Fatalf("endpoint->cert target_arn = %#v, want %q", got, want)
	}

	// No secret leakage anywhere in the resource payloads.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"client_secret", "clientsecret", "client_id", "clientid",
			"token_endpoint", "user_info_endpoint", "policy_document", "policy",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Verified Access scanner must stay metadata-only and never persist secrets", forbidden)
			}
		}
	}
}

func TestScannerSynthesizesGovCloudInstanceARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	client := fakeClient{snapshot: Snapshot{Instances: []Instance{{ID: testInstanceID}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	instance := resourceByType(t, envelopes, awscloud.ResourceTypeVerifiedAccessInstance)
	wantARN := "arn:aws-us-gov:ec2:us-gov-west-1:123456789012:verified-access-instance/" + testInstanceID
	if got := instance.Payload["resource_id"]; got != wantARN {
		t.Fatalf("GovCloud instance resource_id = %#v, want %q", got, wantARN)
	}
	if got := instance.Payload["arn"]; got != wantARN {
		t.Fatalf("GovCloud instance arn = %#v, want %q", got, wantARN)
	}
}

func TestScannerSynthesizesChinaEndpointARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "cn-north-1"
	client := fakeClient{snapshot: Snapshot{Endpoints: []Endpoint{{ID: testEndpointID, GroupID: testGroupID}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	endpoint := resourceByType(t, envelopes, awscloud.ResourceTypeVerifiedAccessEndpoint)
	wantARN := "arn:aws-cn:ec2:cn-north-1:123456789012:verified-access-endpoint/" + testEndpointID
	if got := endpoint.Payload["arn"]; got != wantARN {
		t.Fatalf("China endpoint arn = %#v, want %q", got, wantARN)
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Instances: []Instance{{ID: testInstanceID}},
		Endpoints: []Endpoint{{ID: testEndpointID}}, // no group, no subnets, no sgs, no cert.
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship emitted: %#v", envelope.Payload)
		}
	}
}

func TestScannerOmitsACMEdgeForNonARNCertificate(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Endpoints: []Endpoint{{
		ID:                   testEndpointID,
		DomainCertificateARN: "not-an-arn",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if envelope.Payload["relationship_type"] == awscloud.RelationshipVerifiedAccessEndpointUsesACMCertificate {
			t.Fatalf("ACM edge emitted for non-ARN certificate value")
		}
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	instance := Instance{ID: testInstanceID, TrustProviderIDs: []string{testTrustProviderID}}
	group := Group{ARN: testGroupARN, ID: testGroupID, InstanceID: testInstanceID}
	endpoint := Endpoint{
		ID:                   testEndpointID,
		GroupID:              testGroupID,
		DomainCertificateARN: testCertARN,
		SubnetIDs:            []string{testSubnetID},
		SecurityGroupIDs:     []string{testSecurityGroupID},
	}
	var observations []awscloud.RelationshipObservation
	observations = append(observations, instanceTrustProviderRelationships(boundary, instance)...)
	if rel := groupInInstanceRelationship(boundary, group); rel != nil {
		observations = append(observations, *rel)
	}
	if rel := endpointInGroupRelationship(boundary, endpoint); rel != nil {
		observations = append(observations, *rel)
	}
	observations = append(observations, endpointSubnetRelationships(boundary, endpoint)...)
	observations = append(observations, endpointSecurityGroupRelationships(boundary, endpoint)...)
	if rel := endpointACMCertificateRelationship(boundary, endpoint); rel != nil {
		observations = append(observations, *rel)
	}
	if len(observations) == 0 {
		t.Fatalf("expected relationships for fully populated fixture")
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Instances: []Instance{{ID: testInstanceID}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Verified Access DescribeVerifiedAccessEndpoints throttled after SDK retries; endpoint metadata omitted for this scan",
			SourceRecordID: "verifiedaccess_endpoints_throttled",
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	warning := warningByKind(t, envelopes, awscloud.WarningThrottleSustained)
	if got := warning.Payload["error_class"]; got != "throttled" {
		t.Fatalf("warning error_class = %#v, want throttled", got)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceVerifiedAccess,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:verifiedaccess:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	snapshot Snapshot
}

func (c fakeClient) Snapshot(context.Context) (Snapshot, error) {
	return c.snapshot, nil
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
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
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
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	return facts.Envelope{}
}

func warningByKind(t *testing.T, envelopes []facts.Envelope, warningKind string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSWarningFactKind {
			continue
		}
		if got, _ := envelope.Payload["warning_kind"].(string); got == warningKind {
			return envelope
		}
	}
	t.Fatalf("missing warning_kind %q in %#v", warningKind, envelopes)
	return facts.Envelope{}
}

func assertEdgeTarget(t *testing.T, envelope facts.Envelope, targetType, targetResourceID string) {
	t.Helper()
	if got := envelope.Payload["target_type"]; got != targetType {
		t.Fatalf("target_type = %#v, want %q", got, targetType)
	}
	if got := envelope.Payload["target_resource_id"]; got != targetResourceID {
		t.Fatalf("target_resource_id = %#v, want %q", got, targetResourceID)
	}
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
		gotSlice, ok := got.([]string)
		if !ok || len(gotSlice) != len(want) {
			return false
		}
		for i := range want {
			if gotSlice[i] != want[i] {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}
