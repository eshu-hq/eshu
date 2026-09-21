// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docdbelastic

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testClusterARN = "arn:aws:docdb-elastic:us-east-1:123456789012:cluster/abcd1234"
	testKMSARN     = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
	testSecretARN  = "arn:aws:secretsmanager:us-east-1:123456789012:secret:docdb/admin-AbCdEf"
)

func TestScannerEmitsClusterMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Clusters: []Cluster{{
		ARN:                        testClusterARN,
		Name:                       "analytics",
		Status:                     "ACTIVE",
		AuthType:                   "SECRET_ARN",
		AdminSecretARN:             testSecretARN,
		KMSKeyID:                   testKMSARN,
		ShardCapacity:              4,
		ShardCount:                 2,
		ShardInstanceCount:         3,
		BackupRetentionPeriod:      7,
		PreferredBackupWindow:      "02:00-03:00",
		PreferredMaintenanceWindow: "sun:05:00-sun:06:00",
		SubnetIDs:                  []string{"subnet-0a1b2c3d", "subnet-4e5f6a7b"},
		SecurityGroupIDs:           []string{"sg-0123456789abcdef0"},
		CreateTime:                 time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		Tags:                       map[string]string{"Environment": "prod"},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	cluster := resourceByType(t, envelopes, awscloud.ResourceTypeDocDBElasticCluster)
	if got, want := cluster.Payload["resource_id"], testClusterARN; got != want {
		t.Fatalf("cluster resource_id = %#v, want %q", got, want)
	}
	if got, want := cluster.Payload["arn"], testClusterARN; got != want {
		t.Fatalf("cluster arn = %#v, want %q", got, want)
	}
	if got, want := cluster.Payload["state"], "ACTIVE"; got != want {
		t.Fatalf("cluster state = %#v, want %q", got, want)
	}
	attrs := attributesOf(t, cluster)
	assertAttribute(t, attrs, "auth_type", "SECRET_ARN")
	assertAttribute(t, attrs, "admin_secret_configured", true)
	assertAttribute(t, attrs, "kms_key_id", testKMSARN)
	assertAttribute(t, attrs, "shard_capacity", int32(4))
	assertAttribute(t, attrs, "shard_count", int32(2))
	assertAttribute(t, attrs, "shard_instance_count", int32(3))
	assertAttribute(t, attrs, "subnet_ids", []string{"subnet-0a1b2c3d", "subnet-4e5f6a7b"})
	assertAttribute(t, attrs, "security_group_ids", []string{"sg-0123456789abcdef0"})

	// cluster -> subnet edges, keyed by bare subnet ids the EC2 scanner publishes.
	subnetEdges := relationshipsByType(t, envelopes, awscloud.RelationshipDocDBElasticClusterInSubnet)
	if len(subnetEdges) != 2 {
		t.Fatalf("subnet edges = %d, want 2", len(subnetEdges))
	}
	assertEdgeTarget(t, subnetEdges[0], awscloud.ResourceTypeEC2Subnet, "subnet-0a1b2c3d")
	if got := subnetEdges[0].Payload["target_arn"]; got != "" {
		t.Fatalf("subnet edge target_arn = %#v, want empty (bare id)", got)
	}

	// cluster -> security group edge, keyed by bare sg id.
	sgEdge := relationshipByType(t, envelopes, awscloud.RelationshipDocDBElasticClusterUsesSecurityGroup)
	assertEdgeTarget(t, sgEdge, awscloud.ResourceTypeEC2SecurityGroup, "sg-0123456789abcdef0")
	if got := sgEdge.Payload["target_arn"]; got != "" {
		t.Fatalf("sg edge target_arn = %#v, want empty (bare id)", got)
	}

	// cluster -> KMS key edge.
	kmsEdge := relationshipByType(t, envelopes, awscloud.RelationshipDocDBElasticClusterUsesKMSKey)
	assertEdgeTarget(t, kmsEdge, awscloud.ResourceTypeKMSKey, testKMSARN)
	if got, want := kmsEdge.Payload["target_arn"], testKMSARN; got != want {
		t.Fatalf("kms edge target_arn = %#v, want %q", got, want)
	}

	// cluster -> admin secret edge.
	secretEdge := relationshipByType(t, envelopes, awscloud.RelationshipDocDBElasticClusterUsesAdminSecret)
	assertEdgeTarget(t, secretEdge, awscloud.ResourceTypeSecretsManagerSecret, testSecretARN)
	if got, want := secretEdge.Payload["target_arn"], testSecretARN; got != want {
		t.Fatalf("secret edge target_arn = %#v, want %q", got, want)
	}

	// No document/credential leakage anywhere in the resource payloads.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		assertNoForbiddenKeys(t, envelope)
	}
}

func assertNoForbiddenKeys(t *testing.T, envelope facts.Envelope) {
	t.Helper()
	attrs, _ := envelope.Payload["attributes"].(map[string]any)
	for _, forbidden := range []string{
		"admin_user_name", "admin_username", "admin_user_password", "admin_password",
		"password", "cluster_endpoint", "endpoint", "connection_string",
		"documents", "collections", "indexes", "records", "query_results",
		"admin_secret_arn", "secret_arn",
	} {
		if _, exists := attrs[forbidden]; exists {
			t.Fatalf("%s attribute persisted; docdbelastic scanner must stay metadata-only and never persist credentials/endpoints", forbidden)
		}
	}
}

func TestScannerOmitsAdminSecretEdgeForPlainTextAuth(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Clusters: []Cluster{{
		ARN:      testClusterARN,
		Name:     "plain",
		AuthType: "PLAIN_TEXT",
		// No AdminSecretARN: no secret edge, admin_secret_configured false.
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == awscloud.RelationshipDocDBElasticClusterUsesAdminSecret {
			t.Fatalf("unexpected admin-secret edge emitted for PLAIN_TEXT auth")
		}
	}
	cluster := resourceByType(t, envelopes, awscloud.ResourceTypeDocDBElasticCluster)
	assertAttribute(t, attributesOf(t, cluster), "admin_secret_configured", false)
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Clusters: []Cluster{{
		ARN:    testClusterARN,
		Name:   "bare",
		Status: "ACTIVE",
		// No subnets, SGs, KMS, or secret: no edges.
	}}}}

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

func TestScannerOmitsKMSEdgeForNonARNKeyButKeepsValue(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Clusters: []Cluster{{
		ARN:      testClusterARN,
		Name:     "aliaskey",
		KMSKeyID: "1234abcd-12ab-34cd-56ef-1234567890ab",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	kmsEdge := relationshipByType(t, envelopes, awscloud.RelationshipDocDBElasticClusterUsesKMSKey)
	if got, want := kmsEdge.Payload["target_resource_id"], "1234abcd-12ab-34cd-56ef-1234567890ab"; got != want {
		t.Fatalf("kms target_resource_id = %#v, want %q", got, want)
	}
	if got := kmsEdge.Payload["target_arn"]; got != "" {
		t.Fatalf("kms target_arn = %#v, want empty for non-ARN key identifier", got)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	cluster := Cluster{
		ARN:              testClusterARN,
		Name:             "analytics",
		KMSKeyID:         testKMSARN,
		AuthType:         "SECRET_ARN",
		AdminSecretARN:   testSecretARN,
		SubnetIDs:        []string{"subnet-0a1b2c3d"},
		SecurityGroupIDs: []string{"sg-0123456789abcdef0"},
	}
	observations := clusterRelationships(boundary, cluster)
	if len(observations) != 4 {
		t.Fatalf("relationships = %d, want 4 (subnet, sg, kms, secret)", len(observations))
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
		Clusters: []Cluster{{ARN: testClusterARN, Name: "analytics"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "DocumentDB Elastic GetCluster throttled after SDK retries; cluster metadata omitted for this scan",
			SourceRecordID: "docdbelastic_clusters_throttled",
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

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing-client error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceDocDBElastic,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:docdbelastic:1",
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
	edges := relationshipsByType(t, envelopes, relationshipType)
	if len(edges) == 0 {
		t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	}
	return edges[0]
}

func relationshipsByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) []facts.Envelope {
	t.Helper()
	var edges []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			edges = append(edges, envelope)
		}
	}
	return edges
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
