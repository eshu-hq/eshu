// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package transfer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsTransferMetadataResourcesAndRelationships(t *testing.T) {
	serverID := "s-0123456789abcdef0"
	serverARN := "arn:aws:transfer:us-east-1:123456789012:server/" + serverID
	userARN := "arn:aws:transfer:us-east-1:123456789012:user/" + serverID + "/sftp-user"
	certificateARN := "arn:aws:acm:us-east-1:123456789012:certificate/abcd-1234"
	loggingRoleARN := "arn:aws:iam::123456789012:role/transfer-logging"
	userRoleARN := "arn:aws:iam::123456789012:role/transfer-access"
	logGroupARN := "arn:aws:logs:us-east-1:123456789012:log-group:/aws/transfer/" + serverID
	vpcEndpointID := "vpce-0a1b2c3d4e5f6"
	allocationID := "eipalloc-0123456789abcdef0"

	client := fakeClient{
		servers: []Server{{
			ARN:                       serverARN,
			ServerID:                  serverID,
			Domain:                    "S3",
			EndpointType:              "VPC",
			IdentityProviderType:      "SERVICE_MANAGED",
			State:                     "ONLINE",
			Protocols:                 []string{"SFTP", "FTPS"},
			UserCount:                 1,
			SecurityPolicyName:        "TransferSecurityPolicy-2024-01",
			IPAddressType:             "IPV4",
			VPCEndpointID:             vpcEndpointID,
			VPCID:                     "vpc-0123",
			AddressAllocationIDs:      []string{allocationID, allocationID},
			SubnetIDs:                 []string{"subnet-1"},
			SecurityGroupIDs:          []string{"sg-1"},
			CertificateARN:            certificateARN,
			LoggingRoleARN:            loggingRoleARN,
			StructuredLogDestinations: []string{logGroupARN},
		}},
		users: []User{{
			ServerID:          serverID,
			ARN:               userARN,
			UserName:          "sftp-user",
			HomeDirectory:     "/landing-bucket/home/sftp-user",
			HomeDirectoryType: "PATH",
			RoleARN:           userRoleARN,
			HomeDirectoryMappings: []HomeDirectoryMapping{{
				Entry:  "/",
				Target: "/landing-bucket/home/sftp-user",
			}},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Server resource.
	server := resourceByType(t, envelopes, awscloud.ResourceTypeTransferServer)
	if got, want := server.Payload["resource_id"], serverARN; got != want {
		t.Fatalf("server resource_id = %#v, want %q", got, want)
	}
	serverAttributes := attributesOf(t, server)
	if got, want := serverAttributes["identity_provider_type"], "SERVICE_MANAGED"; got != want {
		t.Fatalf("server identity_provider_type = %#v, want %q", got, want)
	}
	if got, want := serverAttributes["endpoint_type"], "VPC"; got != want {
		t.Fatalf("server endpoint_type = %#v, want %q", got, want)
	}
	for _, forbidden := range []string{"host_key_fingerprint", "host_key", "pre_authentication_login_banner", "post_authentication_login_banner"} {
		if _, exists := serverAttributes[forbidden]; exists {
			t.Fatalf("server %s attribute persisted; host keys and banners must never be stored", forbidden)
		}
	}

	// User resource.
	user := resourceByType(t, envelopes, awscloud.ResourceTypeTransferUser)
	if got, want := user.Payload["resource_id"], userARN; got != want {
		t.Fatalf("user resource_id = %#v, want %q", got, want)
	}
	userAttributes := attributesOf(t, user)
	if got, want := userAttributes["home_directory"], "/landing-bucket/home/sftp-user"; got != want {
		t.Fatalf("user home_directory = %#v, want %q", got, want)
	}
	for _, forbidden := range []string{"ssh_public_keys", "ssh_public_key_body", "policy", "posix_profile", "posix_uid", "posix_gid"} {
		if _, exists := userAttributes[forbidden]; exists {
			t.Fatalf("user %s attribute persisted; SSH keys, policy, and POSIX material must never be stored", forbidden)
		}
	}

	// server -> VPC endpoint (bare-ID keyed, no target ARN).
	vpcEndpoint := relationshipByType(t, envelopes, awscloud.RelationshipTransferServerUsesVPCEndpoint)
	if got, want := vpcEndpoint.Payload["target_type"], awscloud.ResourceTypeVPCEndpoint; got != want {
		t.Fatalf("server->vpc-endpoint target_type = %#v, want %q", got, want)
	}
	if got, want := vpcEndpoint.Payload["target_resource_id"], vpcEndpointID; got != want {
		t.Fatalf("server->vpc-endpoint target_resource_id = %#v, want %q", got, want)
	}
	if got := vpcEndpoint.Payload["target_arn"]; got != nil && got != "" {
		t.Fatalf("server->vpc-endpoint target_arn = %#v, want empty (bare-ID-keyed target)", got)
	}

	// server -> Elastic IP (bare allocation-ID keyed, deduplicated).
	if got := countRelationships(envelopes, awscloud.RelationshipTransferServerUsesElasticIP); got != 1 {
		t.Fatalf("server->eip relationship count = %d, want 1 (duplicates collapse)", got)
	}
	eip := relationshipByType(t, envelopes, awscloud.RelationshipTransferServerUsesElasticIP)
	if got, want := eip.Payload["target_type"], awscloud.ResourceTypeVPCElasticIP; got != want {
		t.Fatalf("server->eip target_type = %#v, want %q", got, want)
	}
	if got, want := eip.Payload["target_resource_id"], allocationID; got != want {
		t.Fatalf("server->eip target_resource_id = %#v, want %q", got, want)
	}

	// server -> ACM certificate (ARN keyed).
	certificate := relationshipByType(t, envelopes, awscloud.RelationshipTransferServerUsesACMCertificate)
	if got, want := certificate.Payload["target_type"], awscloud.ResourceTypeACMCertificate; got != want {
		t.Fatalf("server->acm target_type = %#v, want %q", got, want)
	}
	if got, want := certificate.Payload["target_resource_id"], certificateARN; got != want {
		t.Fatalf("server->acm target_resource_id = %#v, want %q", got, want)
	}
	if got, want := certificate.Payload["target_arn"], certificateARN; got != want {
		t.Fatalf("server->acm target_arn = %#v, want %q", got, want)
	}

	// server -> logging IAM role (ARN keyed).
	loggingRole := relationshipByType(t, envelopes, awscloud.RelationshipTransferServerUsesLoggingRole)
	if got, want := loggingRole.Payload["target_type"], awscloud.ResourceTypeIAMRole; got != want {
		t.Fatalf("server->logging-role target_type = %#v, want %q", got, want)
	}
	if got, want := loggingRole.Payload["target_resource_id"], loggingRoleARN; got != want {
		t.Fatalf("server->logging-role target_resource_id = %#v, want %q", got, want)
	}

	// server -> CloudWatch log group (ARN keyed).
	logGroup := relationshipByType(t, envelopes, awscloud.RelationshipTransferServerLogsToLogGroup)
	if got, want := logGroup.Payload["target_type"], awscloud.ResourceTypeCloudWatchLogsLogGroup; got != want {
		t.Fatalf("server->log-group target_type = %#v, want %q", got, want)
	}
	if got, want := logGroup.Payload["target_resource_id"], logGroupARN; got != want {
		t.Fatalf("server->log-group target_resource_id = %#v, want %q", got, want)
	}

	// user -> IAM role (ARN keyed).
	userRole := relationshipByType(t, envelopes, awscloud.RelationshipTransferUserUsesIAMRole)
	if got, want := userRole.Payload["source_resource_id"], userARN; got != want {
		t.Fatalf("user->role source_resource_id = %#v, want %q", got, want)
	}
	if got, want := userRole.Payload["target_type"], awscloud.ResourceTypeIAMRole; got != want {
		t.Fatalf("user->role target_type = %#v, want %q", got, want)
	}
	if got, want := userRole.Payload["target_resource_id"], userRoleARN; got != want {
		t.Fatalf("user->role target_resource_id = %#v, want %q", got, want)
	}

	// user -> S3 bucket home directory (synthesized partition-aware ARN).
	homeS3 := relationshipByType(t, envelopes, awscloud.RelationshipTransferUserHomeDirectoryInS3Bucket)
	if got, want := homeS3.Payload["target_type"], awscloud.ResourceTypeS3Bucket; got != want {
		t.Fatalf("user->s3 target_type = %#v, want %q", got, want)
	}
	if got, want := homeS3.Payload["target_resource_id"], "arn:aws:s3:::landing-bucket"; got != want {
		t.Fatalf("user->s3 target_resource_id = %#v, want %q", got, want)
	}
	if got, want := homeS3.Payload["target_arn"], "arn:aws:s3:::landing-bucket"; got != want {
		t.Fatalf("user->s3 target_arn = %#v, want %q", got, want)
	}
	homeS3Attributes := attributesOf(t, homeS3)
	if got, want := homeS3Attributes["bucket"], "landing-bucket"; got != want {
		t.Fatalf("user->s3 bucket attribute = %#v, want %q", got, want)
	}
	if got, want := homeS3Attributes["object_key_prefix"], "home/sftp-user"; got != want {
		t.Fatalf("user->s3 object_key_prefix attribute = %#v, want %q", got, want)
	}

	relguard.AssertObservations(t, allRelationshipObservations(t, envelopes)...)
}

func TestScannerEmitsEFSHomeDirectoryRelationship(t *testing.T) {
	serverID := "s-efs00000000000000"
	client := fakeClient{
		servers: []Server{{ServerID: serverID, ARN: "arn:aws:transfer:us-east-1:123456789012:server/" + serverID, Domain: "EFS"}},
		users: []User{{
			ServerID:      serverID,
			ARN:           "arn:aws:transfer:us-east-1:123456789012:user/" + serverID + "/efs-user",
			UserName:      "efs-user",
			HomeDirectory: "/fs-0a1b2c3d/home/efs-user",
			RoleARN:       "arn:aws:iam::123456789012:role/transfer-efs",
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	homeEFS := relationshipByType(t, envelopes, awscloud.RelationshipTransferUserHomeDirectoryInEFSFileSystem)
	wantARN := "arn:aws:elasticfilesystem:us-east-1:123456789012:file-system/fs-0a1b2c3d"
	if got, want := homeEFS.Payload["target_type"], awscloud.ResourceTypeEFSFileSystem; got != want {
		t.Fatalf("user->efs target_type = %#v, want %q", got, want)
	}
	if got, want := homeEFS.Payload["target_resource_id"], wantARN; got != want {
		t.Fatalf("user->efs target_resource_id = %#v, want %q", got, want)
	}
	if got, want := homeEFS.Payload["target_arn"], wantARN; got != want {
		t.Fatalf("user->efs target_arn = %#v, want %q", got, want)
	}
	attributes := attributesOf(t, homeEFS)
	if got, want := attributes["file_system_id"], "fs-0a1b2c3d"; got != want {
		t.Fatalf("user->efs file_system_id attribute = %#v, want %q", got, want)
	}
	relguard.AssertObservations(t, allRelationshipObservations(t, envelopes)...)
}

func TestScannerDerivesSynthesizedARNPartition(t *testing.T) {
	cases := []struct {
		name          string
		region        string
		wantBucketARN string
		wantEFSARN    string
	}{
		{"commercial", "us-east-1", "arn:aws:s3:::landing", "arn:aws:elasticfilesystem:us-east-1:123456789012:file-system/fs-0a1b2c3d"},
		{"govcloud", "us-gov-west-1", "arn:aws-us-gov:s3:::landing", "arn:aws-us-gov:elasticfilesystem:us-gov-west-1:123456789012:file-system/fs-0a1b2c3d"},
		{"china", "cn-north-1", "arn:aws-cn:s3:::landing", "arn:aws-cn:elasticfilesystem:cn-north-1:123456789012:file-system/fs-0a1b2c3d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := testBoundary()
			boundary.Region = tc.region
			client := fakeClient{
				servers: []Server{{ServerID: "s-1", ARN: "arn:aws:transfer:::server/s-1"}},
				users: []User{
					{ServerID: "s-1", ARN: "arn:partition:transfer:::user/s-1/s3", UserName: "s3", HomeDirectory: "/landing/x"},
					{ServerID: "s-1", ARN: "arn:partition:transfer:::user/s-1/efs", UserName: "efs", HomeDirectory: "/fs-0a1b2c3d/x"},
				},
			}
			envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
			if err != nil {
				t.Fatalf("Scan() error = %v, want nil", err)
			}
			homeS3 := relationshipByType(t, envelopes, awscloud.RelationshipTransferUserHomeDirectoryInS3Bucket)
			if got := homeS3.Payload["target_resource_id"]; got != tc.wantBucketARN {
				t.Fatalf("user->s3 target_resource_id = %#v, want %q", got, tc.wantBucketARN)
			}
			homeEFS := relationshipByType(t, envelopes, awscloud.RelationshipTransferUserHomeDirectoryInEFSFileSystem)
			if got := homeEFS.Payload["target_resource_id"]; got != tc.wantEFSARN {
				t.Fatalf("user->efs target_resource_id = %#v, want %q", got, tc.wantEFSARN)
			}
		})
	}
}

func TestScannerOmitsRelationshipsWhenAWSReportsNoJoinKey(t *testing.T) {
	client := fakeClient{
		servers: []Server{{
			ServerID:       "s-1",
			ARN:            "arn:aws:transfer:::server/s-1",
			CertificateARN: "not-an-arn",
			LoggingRoleARN: "transfer-logging",
		}},
		users: []User{{
			ServerID: "s-1",
			ARN:      "arn:aws:transfer:::user/s-1/u",
			UserName: "u",
			RoleARN:  "transfer-access",
		}},
	}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, relationshipType := range []string{
		awscloud.RelationshipTransferServerUsesVPCEndpoint,
		awscloud.RelationshipTransferServerUsesElasticIP,
		awscloud.RelationshipTransferServerUsesACMCertificate,
		awscloud.RelationshipTransferServerUsesLoggingRole,
		awscloud.RelationshipTransferServerLogsToLogGroup,
		awscloud.RelationshipTransferUserUsesIAMRole,
		awscloud.RelationshipTransferUserHomeDirectoryInS3Bucket,
		awscloud.RelationshipTransferUserHomeDirectoryInEFSFileSystem,
	} {
		if got := countRelationships(envelopes, relationshipType); got != 0 {
			t.Fatalf("relationship %q count = %d, want 0 when AWS reports no join key", relationshipType, got)
		}
	}
}

func TestUserHomeDirectoryEFSEdgeRequiresBoundaryAccountAndRegion(t *testing.T) {
	user := User{ServerID: "s-1", ARN: "arn:aws:transfer:::user/s-1/u", UserName: "u", HomeDirectory: "/fs-0a1b2c3d/x"}
	// A complete boundary produces the EFS edge.
	if got := userHomeDirectoryRelationship(testBoundary(), userResourceID(user), user); got == nil {
		t.Fatalf("user->efs relationship = nil for a complete boundary, want an edge")
	}
	// The envelope builder rejects boundaries without an account or region, so
	// the relationship helper defensively skips the EFS edge rather than
	// synthesizing a malformed join key when either is missing.
	for _, boundary := range []awscloud.Boundary{
		{Region: "us-east-1"},
		{AccountID: "123456789012"},
	} {
		if got := userHomeDirectoryRelationship(boundary, userResourceID(user), user); got != nil {
			t.Fatalf("user->efs relationship = %+v for boundary %+v, want nil", got, boundary)
		}
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceSNS

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client required")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceTransfer,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:transfer:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 20, 16, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	servers []Server
	users   []User
}

func (c fakeClient) ListServers(context.Context) ([]Server, error) { return c.servers, nil }
func (c fakeClient) ListUsers(context.Context) ([]User, error)     { return c.users, nil }

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

// allRelationshipObservations reconstructs the relationship observations the
// scanner emitted from the envelope payloads so the relguard runtime contract
// can be asserted directly on every edge's target_type and join-mode shape.
func allRelationshipObservations(t *testing.T, envelopes []facts.Envelope) []awscloud.RelationshipObservation {
	t.Helper()
	var observations []awscloud.RelationshipObservation
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		observation := awscloud.RelationshipObservation{
			RelationshipType: stringField(envelope, "relationship_type"),
			SourceResourceID: stringField(envelope, "source_resource_id"),
			TargetResourceID: stringField(envelope, "target_resource_id"),
			TargetARN:        stringField(envelope, "target_arn"),
			TargetType:       stringField(envelope, "target_type"),
		}
		observations = append(observations, observation)
	}
	return observations
}

func stringField(envelope facts.Envelope, key string) string {
	value, _ := envelope.Payload[key].(string)
	return value
}
