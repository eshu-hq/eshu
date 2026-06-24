// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storagegateway

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsStorageGatewayMetadataResourcesAndRelationships(t *testing.T) {
	gatewayARN := "arn:aws:storagegateway:us-east-1:123456789012:gateway/sgw-12345678"
	volumeARN := "arn:aws:storagegateway:us-east-1:123456789012:gateway/sgw-12345678/volume/vol-abc"
	shareARN := "arn:aws:storagegateway:us-east-1:123456789012:share/share-abc"
	roleARN := "arn:aws:iam::123456789012:role/sgw-s3-access"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
	logGroupARN := "arn:aws:logs:us-east-1:123456789012:log-group:/aws/storagegateway/audit:*"

	client := fakeClient{
		gateways: []Gateway{{
			ARN:                   gatewayARN,
			ID:                    "sgw-12345678",
			Name:                  "prod-file-gateway",
			Type:                  "FILE_S3",
			State:                 "RUNNING",
			OperationalState:      "ACTIVE",
			EndpointType:          "STANDARD",
			HostEnvironment:       "EC2",
			Timezone:              "GMT",
			EC2InstanceID:         "i-0abc",
			SoftwareVersion:       "2.7",
			CloudWatchLogGroup:    logGroupARN,
			VPCEndpoint:           "vpce-0a1b2c3d4e5f6a7b8",
			Tags:                  map[string]string{"env": "prod"},
			NetworkInterfaceCount: 1,
		}},
		volumes: []Volume{{
			ARN:              volumeARN,
			ID:               "vol-abc",
			Type:             "CACHED",
			SizeInBytes:      107374182400,
			AttachmentStatus: "ATTACHED",
			GatewayARN:       gatewayARN,
			GatewayID:        "sgw-12345678",
		}},
		shares: []FileShare{{
			ARN:                 shareARN,
			ID:                  "share-abc",
			Name:                "orders",
			Protocol:            "NFS",
			Type:                "NFS",
			Status:              "AVAILABLE",
			GatewayARN:          gatewayARN,
			LocationARN:         "arn:aws:s3:::orders-archive/prefix/",
			BucketRegion:        "us-east-1",
			Role:                roleARN,
			KMSKey:              kmsARN,
			EncryptionType:      "SseKms",
			DefaultStorageClass: "S3_STANDARD",
			ObjectACL:           "private",
			AuditDestinationARN: logGroupARN,
			ReadOnly:            false,
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	gateway := resourceByType(t, envelopes, awscloud.ResourceTypeStorageGatewayGateway)
	if got, want := gateway.Payload["resource_id"], gatewayARN; got != want {
		t.Fatalf("gateway resource_id = %#v, want %q", got, want)
	}
	gatewayAttributes := attributesOf(t, gateway)
	if got, want := gatewayAttributes["gateway_type"], "FILE_S3"; got != want {
		t.Fatalf("gateway gateway_type = %#v, want %q", got, want)
	}
	if got, want := gatewayAttributes["endpoint_type"], "STANDARD"; got != want {
		t.Fatalf("gateway endpoint_type = %#v, want %q", got, want)
	}

	volume := resourceByType(t, envelopes, awscloud.ResourceTypeStorageGatewayVolume)
	if got, want := volume.Payload["resource_id"], volumeARN; got != want {
		t.Fatalf("volume resource_id = %#v, want %q", got, want)
	}
	volumeAttributes := attributesOf(t, volume)
	if got, want := volumeAttributes["volume_type"], "CACHED"; got != want {
		t.Fatalf("volume volume_type = %#v, want %q", got, want)
	}
	if got, want := volumeAttributes["size_in_bytes"], int64(107374182400); got != want {
		t.Fatalf("volume size_in_bytes = %#v, want %d", got, want)
	}

	share := resourceByType(t, envelopes, awscloud.ResourceTypeStorageGatewayFileShare)
	if got, want := share.Payload["resource_id"], shareARN; got != want {
		t.Fatalf("share resource_id = %#v, want %q", got, want)
	}
	shareAttributes := attributesOf(t, share)
	if got, want := shareAttributes["protocol"], "NFS"; got != want {
		t.Fatalf("share protocol = %#v, want %q", got, want)
	}
	// Metadata-only: object contents and access lists must never be persisted.
	for _, forbidden := range []string{"client_list", "admin_user_list", "valid_user_list", "object_contents", "objects"} {
		if _, exists := shareAttributes[forbidden]; exists {
			t.Fatalf("share %s attribute persisted; access lists and object contents must stay out of facts", forbidden)
		}
	}

	volumeGateway := relationshipByType(t, envelopes, awscloud.RelationshipStorageGatewayVolumeOnGateway)
	if got, want := volumeGateway.Payload["source_resource_id"], volumeARN; got != want {
		t.Fatalf("volume->gateway source_resource_id = %#v, want %q", got, want)
	}
	if got, want := volumeGateway.Payload["target_resource_id"], gatewayARN; got != want {
		t.Fatalf("volume->gateway target_resource_id = %#v, want %q", got, want)
	}
	if got, want := volumeGateway.Payload["target_type"], awscloud.ResourceTypeStorageGatewayGateway; got != want {
		t.Fatalf("volume->gateway target_type = %#v, want %q", got, want)
	}

	shareGateway := relationshipByType(t, envelopes, awscloud.RelationshipStorageGatewayFileShareOnGateway)
	if got, want := shareGateway.Payload["target_resource_id"], gatewayARN; got != want {
		t.Fatalf("share->gateway target_resource_id = %#v, want %q", got, want)
	}
	if got, want := shareGateway.Payload["target_type"], awscloud.ResourceTypeStorageGatewayGateway; got != want {
		t.Fatalf("share->gateway target_type = %#v, want %q", got, want)
	}

	shareS3 := relationshipByType(t, envelopes, awscloud.RelationshipStorageGatewayFileShareStoresInS3Bucket)
	if got, want := shareS3.Payload["target_resource_id"], "arn:aws:s3:::orders-archive"; got != want {
		t.Fatalf("share->s3 target_resource_id = %#v, want %q", got, want)
	}
	if got, want := shareS3.Payload["target_arn"], "arn:aws:s3:::orders-archive"; got != want {
		t.Fatalf("share->s3 target_arn = %#v, want %q", got, want)
	}
	if got, want := shareS3.Payload["target_type"], awscloud.ResourceTypeS3Bucket; got != want {
		t.Fatalf("share->s3 target_type = %#v, want %q", got, want)
	}
	shareS3Attributes := attributesOf(t, shareS3)
	if got, want := shareS3Attributes["bucket"], "orders-archive"; got != want {
		t.Fatalf("share->s3 bucket attribute = %#v, want %q", got, want)
	}
	if got, want := shareS3Attributes["object_key_prefix"], "prefix/"; got != want {
		t.Fatalf("share->s3 object_key_prefix attribute = %#v, want %q", got, want)
	}

	shareRole := relationshipByType(t, envelopes, awscloud.RelationshipStorageGatewayFileShareUsesIAMRole)
	if got, want := shareRole.Payload["target_resource_id"], roleARN; got != want {
		t.Fatalf("share->role target_resource_id = %#v, want %q", got, want)
	}
	if got, want := shareRole.Payload["target_type"], awscloud.ResourceTypeIAMRole; got != want {
		t.Fatalf("share->role target_type = %#v, want %q", got, want)
	}

	shareKMS := relationshipByType(t, envelopes, awscloud.RelationshipStorageGatewayFileShareUsesKMSKey)
	if got, want := shareKMS.Payload["target_resource_id"], kmsARN; got != want {
		t.Fatalf("share->kms target_resource_id = %#v, want %q", got, want)
	}
	if got, want := shareKMS.Payload["target_type"], awscloud.ResourceTypeKMSKey; got != want {
		t.Fatalf("share->kms target_type = %#v, want %q", got, want)
	}

	shareLog := relationshipByType(t, envelopes, awscloud.RelationshipStorageGatewayFileShareLogsToCloudWatch)
	if got, want := shareLog.Payload["target_resource_id"], logGroupARN; got != want {
		t.Fatalf("share->log target_resource_id = %#v, want %q", got, want)
	}
	if got, want := shareLog.Payload["target_type"], awscloud.ResourceTypeCloudWatchLogsLogGroup; got != want {
		t.Fatalf("share->log target_type = %#v, want %q", got, want)
	}

	gatewayVPCE := relationshipByType(t, envelopes, awscloud.RelationshipStorageGatewayGatewayUsesVPCEndpoint)
	if got, want := gatewayVPCE.Payload["source_resource_id"], gatewayARN; got != want {
		t.Fatalf("gateway->vpce source_resource_id = %#v, want %q", got, want)
	}
	if got, want := gatewayVPCE.Payload["target_resource_id"], "vpce-0a1b2c3d4e5f6a7b8"; got != want {
		t.Fatalf("gateway->vpce target_resource_id = %#v, want %q", got, want)
	}
	if got, want := gatewayVPCE.Payload["target_type"], awscloud.ResourceTypeVPCEndpoint; got != want {
		t.Fatalf("gateway->vpce target_type = %#v, want %q", got, want)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinContract(t *testing.T) {
	gatewayARN := "arn:aws:storagegateway:us-east-1:123456789012:gateway/sgw-1"
	observations := []awscloud.RelationshipObservation{}
	if r := volumeOnGatewayRelationship(testBoundary(), Volume{ARN: "arn:aws:storagegateway:us-east-1:123456789012:gateway/sgw-1/volume/vol-1", GatewayARN: gatewayARN}); r != nil {
		observations = append(observations, *r)
	}
	share := FileShare{
		ARN:                 "arn:aws:storagegateway:us-east-1:123456789012:share/share-1",
		GatewayARN:          gatewayARN,
		LocationARN:         "arn:aws:s3:::bucket/key/",
		Role:                "arn:aws:iam::123456789012:role/r",
		KMSKey:              "arn:aws:kms:us-east-1:123456789012:key/abc",
		AuditDestinationARN: "arn:aws:logs:us-east-1:123456789012:log-group:/g:*",
	}
	for _, r := range []*awscloud.RelationshipObservation{
		fileShareOnGatewayRelationship(testBoundary(), share),
		fileShareS3BucketRelationship(testBoundary(), share),
		fileShareRoleRelationship(testBoundary(), share),
		fileShareKMSKeyRelationship(testBoundary(), share),
		fileShareLogGroupRelationship(testBoundary(), share),
	} {
		if r != nil {
			observations = append(observations, *r)
		}
	}
	if r := gatewayVPCEndpointRelationship(testBoundary(), Gateway{ARN: gatewayARN, VPCEndpoint: "vpce-123"}); r != nil {
		observations = append(observations, *r)
	}
	if len(observations) != 7 {
		t.Fatalf("built %d relationship observations, want 7", len(observations))
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerFileShareS3RelationshipDerivesPartition(t *testing.T) {
	cases := []struct {
		name        string
		locationARN string
		wantBucket  string
	}{
		{"commercial", "arn:aws:s3:::commercial-bucket/data/", "arn:aws:s3:::commercial-bucket"},
		{"govcloud", "arn:aws-us-gov:s3:::gov-bucket/data/", "arn:aws-us-gov:s3:::gov-bucket"},
		{"china", "arn:aws-cn:s3:::cn-bucket/data/", "arn:aws-cn:s3:::cn-bucket"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			share := FileShare{ARN: "arn:aws:storagegateway:us-east-1:123456789012:share/s", LocationARN: tc.locationARN}
			r := fileShareS3BucketRelationship(testBoundary(), share)
			if r == nil {
				t.Fatalf("share->s3 relationship = nil, want one")
			}
			if got := r.TargetResourceID; got != tc.wantBucket {
				t.Fatalf("share->s3 target_resource_id = %q, want %q", got, tc.wantBucket)
			}
			if got := r.TargetARN; got != tc.wantBucket {
				t.Fatalf("share->s3 target_arn = %q, want %q", got, tc.wantBucket)
			}
		})
	}
}

func TestScannerOmitsS3RelationshipForAccessPointLocation(t *testing.T) {
	share := FileShare{
		ARN:         "arn:aws:storagegateway:us-east-1:123456789012:share/s",
		LocationARN: "arn:aws:s3:us-east-1:123456789012:accesspoint/my-ap/prefix/",
	}
	if r := fileShareS3BucketRelationship(testBoundary(), share); r != nil {
		t.Fatalf("share->s3 relationship = %#v, want nil for access-point location", r)
	}
}

func TestScannerOmitsRoleKMSAndLogRelationshipsWhenNotARN(t *testing.T) {
	client := fakeClient{shares: []FileShare{{
		ARN:                 "arn:aws:storagegateway:us-east-1:123456789012:share/s",
		Role:                "not-an-arn",
		KMSKey:              "alias/my-key",
		AuditDestinationARN: "",
	}}}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, relationshipType := range []string{
		awscloud.RelationshipStorageGatewayFileShareUsesIAMRole,
		awscloud.RelationshipStorageGatewayFileShareUsesKMSKey,
		awscloud.RelationshipStorageGatewayFileShareLogsToCloudWatch,
	} {
		if got := countRelationships(envelopes, relationshipType); got != 0 {
			t.Fatalf("%s relationship count = %d, want 0 for non-ARN identity", relationshipType, got)
		}
	}
}

func TestScannerOmitsVPCEndpointRelationshipForNonVpceValue(t *testing.T) {
	cases := []string{
		"vpce-0a1b2c3d4e5f6a7b8.s3.us-east-1.vpce.amazonaws.com",
		"10.0.0.5",
		"",
		"my-gateway-endpoint",
	}
	for _, value := range cases {
		client := fakeClient{gateways: []Gateway{{ARN: "arn:aws:storagegateway:us-east-1:123456789012:gateway/g", VPCEndpoint: value}}}
		envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
		if err != nil {
			t.Fatalf("Scan() error = %v, want nil", err)
		}
		if got := countRelationships(envelopes, awscloud.RelationshipStorageGatewayGatewayUsesVPCEndpoint); got != 0 {
			t.Fatalf("gateway->vpce count = %d, want 0 for VPCEndpoint %q", got, value)
		}
	}
}

func TestScannerOmitsGatewayEdgesWhenGatewayARNMissing(t *testing.T) {
	client := fakeClient{
		volumes: []Volume{{ARN: "arn:aws:storagegateway:us-east-1:123456789012:gateway/g/volume/v", GatewayARN: ""}},
		shares:  []FileShare{{ARN: "arn:aws:storagegateway:us-east-1:123456789012:share/s", GatewayARN: ""}},
	}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipStorageGatewayVolumeOnGateway); got != 0 {
		t.Fatalf("volume->gateway count = %d, want 0 when gateway ARN missing", got)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipStorageGatewayFileShareOnGateway); got != 0 {
		t.Fatalf("share->gateway count = %d, want 0 when gateway ARN missing", got)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3
	if _, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary); err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	if _, err := (Scanner{}).Scan(context.Background(), testBoundary()); err == nil {
		t.Fatalf("Scan() error = nil, want client required")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceStorageGateway,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:storagegateway:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 20, 16, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	gateways []Gateway
	volumes  []Volume
	shares   []FileShare
}

func (c fakeClient) ListGateways(context.Context) ([]Gateway, error)     { return c.gateways, nil }
func (c fakeClient) ListVolumes(context.Context) ([]Volume, error)       { return c.volumes, nil }
func (c fakeClient) ListFileShares(context.Context) ([]FileShare, error) { return c.shares, nil }

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
	t.Fatalf("missing resource_type %q in %d envelopes", resourceType, len(envelopes))
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
	t.Fatalf("missing relationship_type %q in %d envelopes", relationshipType, len(envelopes))
	return facts.Envelope{}
}

func countRelationships(envelopes []facts.Envelope, relationshipType string) int {
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
