// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workspaces

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testAccount     = "123456789012"
	testRegion      = "us-east-1"
	testWorkspaceID = "ws-1234567890"
	testDirectoryID = "d-1234567890"
	testBundleID    = "wsb-1234567890"
	testIPGroupID   = "wsipg-1234567890"
	testSubnetA     = "subnet-aaaa1111"
	testSubnetB     = "subnet-bbbb2222"
	testSecGroupID  = "sg-cccc3333"
	testRoleARN     = "arn:aws:iam::123456789012:role/workspaces_DefaultRole"
	testKMSARN      = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
)

func fullSnapshot() Snapshot {
	return Snapshot{
		Directories: []Directory{{
			ID:                       testDirectoryID,
			Name:                     "corp.example.com",
			Alias:                    "corp",
			State:                    "REGISTERED",
			DirectoryType:            "AD_CONNECTOR",
			Tenancy:                  "SHARED",
			IamRoleID:                testRoleARN,
			WorkspaceSecurityGroupID: testSecGroupID,
			SubnetIDs:                []string{testSubnetA, testSubnetB},
			IPGroupIDs:               []string{testIPGroupID},
			Tags:                     map[string]string{"Environment": "prod"},
		}},
		Bundles: []Bundle{{
			ID:                testBundleID,
			Name:              "Standard with Amazon Linux 2",
			Description:       "standard bundle",
			Owner:             "AMAZON",
			BundleType:        "REGULAR",
			ComputeType:       "STANDARD",
			RootVolumeSizeGib: "80",
			UserVolumeSizeGib: "50",
			ImageID:           "wsi-abc123",
			State:             "AVAILABLE",
			CreationTime:      time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			Tags:              map[string]string{"Team": "platform"},
		}},
		IPGroups: []IPGroup{{
			ID:          testIPGroupID,
			Name:        "office-cidrs",
			Description: "corp office ranges",
			Rules: []IPRule{
				{CIDR: "203.0.113.0/24", Description: "hq"},
				{CIDR: "198.51.100.0/24"},
			},
			Tags: map[string]string{"Owner": "netsec"},
		}},
		Workspaces: []Workspace{{
			ID:                          testWorkspaceID,
			Name:                        "alice-desktop",
			DirectoryID:                 testDirectoryID,
			BundleID:                    testBundleID,
			State:                       "AVAILABLE",
			ComputerName:                "EC2AMAZ-ABC",
			UserName:                    "alice",
			VolumeEncryptionKey:         testKMSARN,
			RootVolumeEncryptionEnabled: true,
			UserVolumeEncryptionEnabled: true,
			Tags:                        map[string]string{"CostCenter": "1234"},
		}},
	}
}

func wantWorkspaceARN() string {
	return "arn:aws:workspaces:us-east-1:123456789012:workspace/" + testWorkspaceID
}

func wantDirectoryARN() string {
	return "arn:aws:workspaces:us-east-1:123456789012:directory/" + testDirectoryID
}

func wantBundleARN() string {
	return "arn:aws:workspaces:us-east-1:123456789012:workspacebundle/" + testBundleID
}

func wantIPGroupARN() string {
	return "arn:aws:workspaces:us-east-1:123456789012:workspaceipgroup/" + testIPGroupID
}

func TestScannerEmitsWorkSpacesMetadataAndRelationships(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{snapshot: fullSnapshot()}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Workspace node keyed by the synthesized partition-aware ARN.
	workspace := resourceByType(t, envelopes, awscloud.ResourceTypeWorkSpacesWorkspace)
	if got, want := workspace.Payload["resource_id"], wantWorkspaceARN(); got != want {
		t.Fatalf("workspace resource_id = %#v, want %q", got, want)
	}
	if got, want := workspace.Payload["arn"], wantWorkspaceARN(); got != want {
		t.Fatalf("workspace arn = %#v, want %q", got, want)
	}
	if got, want := workspace.Payload["state"], "AVAILABLE"; got != want {
		t.Fatalf("workspace state = %#v, want %q", got, want)
	}
	wsAttrs := attributesOf(t, workspace)
	assertAttribute(t, wsAttrs, "workspace_id", testWorkspaceID)
	assertAttribute(t, wsAttrs, "user_name", "alice")
	assertAttribute(t, wsAttrs, "root_volume_encryption_enabled", true)

	// Directory node keyed by the synthesized WorkSpaces directory ARN, NOT the
	// bare DS directory id (which is what the DS scanner publishes).
	directory := resourceByType(t, envelopes, awscloud.ResourceTypeWorkSpacesDirectory)
	if got, want := directory.Payload["resource_id"], wantDirectoryARN(); got != want {
		t.Fatalf("directory resource_id = %#v, want %q", got, want)
	}
	dirAttrs := attributesOf(t, directory)
	assertAttribute(t, dirAttrs, "directory_id", testDirectoryID)
	assertAttribute(t, dirAttrs, "directory_type", "AD_CONNECTOR")
	assertAttribute(t, dirAttrs, "subnet_ids", []string{testSubnetA, testSubnetB})

	// Bundle node.
	bundle := resourceByType(t, envelopes, awscloud.ResourceTypeWorkSpacesBundle)
	if got, want := bundle.Payload["resource_id"], wantBundleARN(); got != want {
		t.Fatalf("bundle resource_id = %#v, want %q", got, want)
	}
	bundleAttrs := attributesOf(t, bundle)
	assertAttribute(t, bundleAttrs, "compute_type", "STANDARD")
	assertAttribute(t, bundleAttrs, "owner", "AMAZON")
	assertAttribute(t, bundleAttrs, "root_volume_size_gib", "80")

	// IP access control group node.
	ipGroup := resourceByType(t, envelopes, awscloud.ResourceTypeWorkSpacesIPGroup)
	if got, want := ipGroup.Payload["resource_id"], wantIPGroupARN(); got != want {
		t.Fatalf("ip group resource_id = %#v, want %q", got, want)
	}

	// workspace -> directory edge keyed by the directory node's published ARN.
	wsDir := relationshipByType(t, envelopes, awscloud.RelationshipWorkSpacesWorkspaceInDirectory)
	assertEdgeTarget(t, wsDir, awscloud.ResourceTypeWorkSpacesDirectory, wantDirectoryARN())
	if got, want := wsDir.Payload["source_resource_id"], wantWorkspaceARN(); got != want {
		t.Fatalf("workspace->directory source_resource_id = %#v, want %q", got, want)
	}

	// workspace -> bundle edge.
	wsBundle := relationshipByType(t, envelopes, awscloud.RelationshipWorkSpacesWorkspaceUsesBundle)
	assertEdgeTarget(t, wsBundle, awscloud.ResourceTypeWorkSpacesBundle, wantBundleARN())

	// workspace -> KMS key edge keyed by the reported key ARN.
	wsKMS := relationshipByType(t, envelopes, awscloud.RelationshipWorkSpacesWorkspaceUsesKMSKey)
	assertEdgeTarget(t, wsKMS, awscloud.ResourceTypeKMSKey, testKMSARN)
	if got, want := wsKMS.Payload["target_arn"], testKMSARN; got != want {
		t.Fatalf("workspace->kms target_arn = %#v, want %q", got, want)
	}

	// directory -> DS directory edge keyed by the BARE directory id the DS
	// scanner publishes (this is the critical cross-service join, distinct from
	// the internal workspace->directory edge above).
	dirDS := relationshipByType(t, envelopes, awscloud.RelationshipWorkSpacesDirectoryUsesDSDirectory)
	assertEdgeTarget(t, dirDS, awscloud.ResourceTypeDSDirectory, testDirectoryID)
	if got, want := dirDS.Payload["source_resource_id"], wantDirectoryARN(); got != want {
		t.Fatalf("directory->ds source_resource_id = %#v, want %q", got, want)
	}
	if got := dirDS.Payload["target_arn"]; got != "" {
		t.Fatalf("directory->ds target_arn = %#v, want empty (bare DS id, not ARN)", got)
	}

	// directory -> security group edge keyed by the bare sg id.
	dirSG := relationshipByType(t, envelopes, awscloud.RelationshipWorkSpacesDirectoryUsesSecurityGroup)
	assertEdgeTarget(t, dirSG, awscloud.ResourceTypeEC2SecurityGroup, testSecGroupID)

	// directory -> IAM role edge keyed by the role ARN.
	dirRole := relationshipByType(t, envelopes, awscloud.RelationshipWorkSpacesDirectoryUsesIAMRole)
	assertEdgeTarget(t, dirRole, awscloud.ResourceTypeIAMRole, testRoleARN)
	if got, want := dirRole.Payload["target_arn"], testRoleARN; got != want {
		t.Fatalf("directory->iam target_arn = %#v, want %q", got, want)
	}

	// directory -> IP group edge keyed by the IP group node's published ARN.
	dirIPGroup := relationshipByType(t, envelopes, awscloud.RelationshipWorkSpacesDirectoryUsesIPGroup)
	assertEdgeTarget(t, dirIPGroup, awscloud.ResourceTypeWorkSpacesIPGroup, wantIPGroupARN())

	// directory -> subnet edges (both subnets, bare ids).
	subnetTargets := relationshipTargets(envelopes, awscloud.RelationshipWorkSpacesDirectoryInSubnet)
	if len(subnetTargets) != 2 {
		t.Fatalf("directory->subnet edges = %d, want 2 (%#v)", len(subnetTargets), subnetTargets)
	}
	for _, want := range []string{testSubnetA, testSubnetB} {
		if !subnetTargets[want] {
			t.Fatalf("missing directory->subnet edge for %q in %#v", want, subnetTargets)
		}
	}

	// No credential / connection / session leakage anywhere in resource payloads.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"password", "registration_code", "ip_address", "ipv6_address",
			"connection_string", "credentials", "secret", "session",
			"customer_user_name", "dns_ip_addresses", "registration",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; WorkSpaces scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerSynthesizesGovCloudARNs(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	client := fakeClient{snapshot: Snapshot{
		Workspaces: []Workspace{{
			ID:          testWorkspaceID,
			DirectoryID: testDirectoryID,
		}},
		Directories: []Directory{{ID: testDirectoryID}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	workspace := resourceByType(t, envelopes, awscloud.ResourceTypeWorkSpacesWorkspace)
	wantARN := "arn:aws-us-gov:workspaces:us-gov-west-1:123456789012:workspace/" + testWorkspaceID
	if got := workspace.Payload["resource_id"]; got != wantARN {
		t.Fatalf("GovCloud workspace resource_id = %#v, want %q", got, wantARN)
	}
	// directory -> DS edge still keys on the bare directory id in GovCloud.
	dirDS := relationshipByType(t, envelopes, awscloud.RelationshipWorkSpacesDirectoryUsesDSDirectory)
	if got := dirDS.Payload["target_resource_id"]; got != testDirectoryID {
		t.Fatalf("GovCloud directory->ds target = %#v, want bare %q", got, testDirectoryID)
	}
}

func TestScannerSynthesizesChinaARNs(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "cn-north-1"
	client := fakeClient{snapshot: Snapshot{Bundles: []Bundle{{ID: testBundleID, Name: "b"}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	bundle := resourceByType(t, envelopes, awscloud.ResourceTypeWorkSpacesBundle)
	wantARN := "arn:aws-cn:workspaces:cn-north-1:123456789012:workspacebundle/" + testBundleID
	if got := bundle.Payload["resource_id"]; got != wantARN {
		t.Fatalf("China bundle resource_id = %#v, want %q", got, wantARN)
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Workspaces:  []Workspace{{ID: testWorkspaceID}},       // no directory, bundle, or key
		Directories: []Directory{{ID: testDirectoryID + "x"}}, // no subnets, sg, role, ip groups
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		// Only the directory->DS edge should survive (a directory always has a DS id).
		if got := envelope.Payload["relationship_type"]; got != awscloud.RelationshipWorkSpacesDirectoryUsesDSDirectory {
			t.Fatalf("unexpected relationship %#v emitted with absent dependencies", got)
		}
	}
}

func TestScannerOmitsKMSEdgeForNonARNKeyButKeepsValue(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Workspaces: []Workspace{{
		ID:                  testWorkspaceID,
		VolumeEncryptionKey: "1234abcd-12ab-34cd-56ef-1234567890ab",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	wsKMS := relationshipByType(t, envelopes, awscloud.RelationshipWorkSpacesWorkspaceUsesKMSKey)
	if got, want := wsKMS.Payload["target_resource_id"], "1234abcd-12ab-34cd-56ef-1234567890ab"; got != want {
		t.Fatalf("kms target_resource_id = %#v, want %q", got, want)
	}
	if got := wsKMS.Payload["target_arn"]; got != "" {
		t.Fatalf("kms target_arn = %#v, want empty for bare key id", got)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	snapshot := fullSnapshot()
	var observations []awscloud.RelationshipObservation
	for _, workspace := range snapshot.Workspaces {
		for _, rel := range []*awscloud.RelationshipObservation{
			workspaceInDirectoryRelationship(boundary, workspace),
			workspaceUsesBundleRelationship(boundary, workspace),
			workspaceUsesKMSKeyRelationship(boundary, workspace),
		} {
			if rel == nil {
				t.Fatalf("expected non-nil workspace relationship for fully populated fixture")
			}
			observations = append(observations, *rel)
		}
	}
	for _, directory := range snapshot.Directories {
		observations = append(observations, directoryRelationships(boundary, directory)...)
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
		Workspaces: []Workspace{{ID: testWorkspaceID}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "WorkSpaces DescribeTags throttled after SDK retries; tags omitted for this scan",
			SourceRecordID: "workspaces_tags_throttled",
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
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}
