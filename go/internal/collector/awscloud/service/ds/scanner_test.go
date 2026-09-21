// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ds

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsDirectoriesTrustsSharesAndRelationships(t *testing.T) {
	client := fakeClient{
		directories: []Directory{
			{
				ID:                "d-1234567890",
				Name:              "corp.example.com",
				ShortName:         "CORP",
				Type:              "MicrosoftAD",
				Edition:           "Enterprise",
				Size:              "Large",
				Stage:             "Active",
				Description:       "primary managed AD",
				VPCID:             "vpc-aaa",
				SubnetIDs:         []string{"subnet-1", "subnet-2"},
				SecurityGroupID:   "sg-123",
				AvailabilityZones: []string{"us-east-1a", "us-east-1b"},
				LDAPSStatuses:     []string{"Enabled"},
				SsoEnabled:        true,
				Tags:              map[string]string{"Environment": "prod"},
			},
			{
				ID:        "d-0987654321",
				Name:      "connector.example.com",
				Type:      "ADConnector",
				Size:      "Small",
				Stage:     "Active",
				VPCID:     "vpc-bbb",
				SubnetIDs: []string{"subnet-9"},
			},
		},
		trusts: map[string][]Trust{
			"d-1234567890": {
				{
					ID:               "t-aaaa111122",
					DirectoryID:      "d-1234567890",
					RemoteDomainName: "remote.example.com",
					Direction:        "Two-Way",
					Type:             "Forest",
					State:            "Verified",
					SelectiveAuth:    "Enabled",
				},
			},
		},
		shares: map[string][]SharedDirectory{
			"d-1234567890": {
				{
					OwnerAccountID:    "123456789012",
					OwnerDirectoryID:  "d-1234567890",
					SharedAccountID:   "210987654321",
					SharedDirectoryID: "d-shared00001",
					ShareMethod:       "HANDSHAKE",
					ShareStatus:       "Shared",
				},
			},
		},
		ldaps: map[string][]LDAPSSetting{
			"d-1234567890": {{Status: "Enabled"}},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	if got, want := countResources(envelopes, awscloud.ResourceTypeDSDirectory), 2; got != want {
		t.Fatalf("directory resources = %d, want %d", got, want)
	}
	if got, want := countResources(envelopes, awscloud.ResourceTypeDSTrust), 1; got != want {
		t.Fatalf("trust resources = %d, want %d", got, want)
	}
	if got, want := countResources(envelopes, awscloud.ResourceTypeDSSharedDirectory), 1; got != want {
		t.Fatalf("shared-directory resources = %d, want %d", got, want)
	}

	// Directory resource_id must be the bare directory ID so the FSx AD-directory
	// edge (target_resource_id = bare d-xxxx) resolves against it.
	directory := resourceByID(t, envelopes, "d-1234567890")
	if got, want := directory.Payload["resource_type"], awscloud.ResourceTypeDSDirectory; got != want {
		t.Fatalf("directory resource_type = %#v, want %q", got, want)
	}
	attrs := attributesOf(t, directory)
	if got, want := attrs["directory_type"], "MicrosoftAD"; got != want {
		t.Fatalf("directory_type = %#v, want %q", got, want)
	}
	if got, want := attrs["edition"], "Enterprise"; got != want {
		t.Fatalf("edition = %#v, want %q", got, want)
	}
	if got, want := attrs["size"], "Large"; got != want {
		t.Fatalf("size = %#v, want %q", got, want)
	}
	if _, ok := attrs["ldaps_statuses"]; !ok {
		t.Fatalf("directory attributes missing ldaps_statuses")
	}

	// Relationships present.
	assertRelationship(t, envelopes, awscloud.RelationshipDSDirectoryInVPC)
	assertRelationship(t, envelopes, awscloud.RelationshipDSDirectoryInSubnet)
	assertRelationship(t, envelopes, awscloud.RelationshipDSTrustTargetsDirectory)
	assertRelationship(t, envelopes, awscloud.RelationshipDSSharedDirectoryTargetsOwnerDirectory)
	assertRelationship(t, envelopes, awscloud.RelationshipDSSharedDirectoryTargetsOwnerAccount)

	// VPC edges: one per directory (2). Subnet edges: 2 + 1 = 3.
	if got, want := countRelationships(envelopes, awscloud.RelationshipDSDirectoryInVPC), 2; got != want {
		t.Fatalf("directory-in-vpc relationships = %d, want %d", got, want)
	}
	if got, want := countRelationships(envelopes, awscloud.RelationshipDSDirectoryInSubnet), 3; got != want {
		t.Fatalf("directory-in-subnet relationships = %d, want %d", got, want)
	}

	// Every relationship carries a non-empty target_type + target_resource_id.
	assertRelationshipJoinKeys(t, envelopes)

	// VPC/subnet edges target the bare AWS ID (joins aws_ec2_vpc / aws_ec2_subnet).
	vpcEdge := relationshipByType(t, envelopes, awscloud.RelationshipDSDirectoryInVPC)
	if got, want := vpcEdge.Payload["target_resource_id"], "vpc-aaa"; got != want {
		t.Fatalf("directory->vpc target_resource_id = %#v, want bare %q", got, want)
	}
	if got, want := vpcEdge.Payload["target_type"], awscloud.ResourceTypeEC2VPC; got != want {
		t.Fatalf("directory->vpc target_type = %#v, want %q", got, want)
	}

	// Trust->directory edge targets the bare directory id (joins the directory fact).
	trustEdge := relationshipByType(t, envelopes, awscloud.RelationshipDSTrustTargetsDirectory)
	if got, want := trustEdge.Payload["target_resource_id"], "d-1234567890"; got != want {
		t.Fatalf("trust->directory target_resource_id = %#v, want %q", got, want)
	}
	if got, want := trustEdge.Payload["target_type"], awscloud.ResourceTypeDSDirectory; got != want {
		t.Fatalf("trust->directory target_type = %#v, want %q", got, want)
	}

	// Shared-directory->owner-account edge targets the bare account id (no ARN synthesis).
	ownerAccountEdge := relationshipByType(t, envelopes, awscloud.RelationshipDSSharedDirectoryTargetsOwnerAccount)
	if got, want := ownerAccountEdge.Payload["target_resource_id"], "123456789012"; got != want {
		t.Fatalf("shared-directory->owner-account target_resource_id = %#v, want %q", got, want)
	}
	if got, _ := ownerAccountEdge.Payload["target_arn"].(string); got != "" {
		t.Fatalf("shared-directory->owner-account target_arn = %q, want empty (no ARN synthesis)", got)
	}

	// Trust resource_id is the bare trust id.
	trust := resourceByID(t, envelopes, "t-aaaa111122")
	trustAttrs := attributesOf(t, trust)
	if got, want := trustAttrs["trust_direction"], "Two-Way"; got != want {
		t.Fatalf("trust_direction = %#v, want %q", got, want)
	}
}

// TestScannerNeverPersistsSecrets proves the scanner never stores directory
// admin passwords, the RADIUS shared secret, or AD Connector service-account
// credentials. The scanner-owned types have no field for these values; this
// guards against a future regression that smuggles them into an attribute map.
func TestScannerNeverPersistsSecrets(t *testing.T) {
	forbidden := []string{
		"password", "admin_password", "directory_password", "shared_secret",
		"radius_shared_secret", "secret", "credentials", "customer_user_name",
		"user_name", "username", "service_account",
	}

	client := fakeClient{
		directories: []Directory{
			{ID: "d-1234567890", Name: "corp.example.com", Type: "MicrosoftAD", Stage: "Active", VPCID: "vpc-aaa", SubnetIDs: []string{"subnet-1"}},
		},
		trusts: map[string][]Trust{
			"d-1234567890": {{ID: "t-aaaa111122", DirectoryID: "d-1234567890", State: "Verified"}},
		},
		shares: map[string][]SharedDirectory{
			"d-1234567890": {{OwnerAccountID: "123456789012", OwnerDirectoryID: "d-1234567890", SharedAccountID: "210987654321"}},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attributes, ok := envelope.Payload["attributes"].(map[string]any)
		if !ok {
			continue
		}
		for key := range attributes {
			for _, bad := range forbidden {
				if key == bad {
					t.Fatalf("attribute %q persisted on %v; DS scanner must never store secrets",
						key, envelope.Payload["resource_type"])
				}
			}
		}
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceFSx

	if _, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary); err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	if _, err := (Scanner{}).Scan(context.Background(), testBoundary()); err == nil {
		t.Fatalf("Scan() error = nil, want missing client error")
	}
}

func TestScannerDefaultsServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = ""
	envelopes, err := (Scanner{Client: fakeClient{
		directories: []Directory{{ID: "d-1234567890", Name: "corp.example.com", Type: "SimpleAD", Stage: "Active"}},
	}}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countResources(envelopes, awscloud.ResourceTypeDSDirectory); got != 1 {
		t.Fatalf("directory resources = %d, want 1", got)
	}
}

// TestScannerPropagatesTrustError proves the scanner surfaces a per-directory
// trust list error instead of swallowing it.
func TestScannerPropagatesTrustError(t *testing.T) {
	client := erroringClient{
		directories: []Directory{{ID: "d-1234567890", Name: "corp.example.com", Type: "MicrosoftAD", Stage: "Active"}},
	}
	if _, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary()); err == nil {
		t.Fatalf("Scan() error = nil, want trust list error to propagate")
	}
}

type erroringClient struct {
	directories []Directory
}

func (c erroringClient) ListDirectories(context.Context) ([]Directory, error) {
	return c.directories, nil
}

func (c erroringClient) ListTrusts(context.Context, string) ([]Trust, error) {
	return nil, errTrust
}

func (c erroringClient) ListSharedDirectories(context.Context, string) ([]SharedDirectory, error) {
	return nil, nil
}

func (c erroringClient) ListLDAPSSettings(context.Context, string) ([]LDAPSSetting, error) {
	return nil, nil
}

var errTrust = &scanError{msg: "boom"}

type scanError struct{ msg string }

func (e *scanError) Error() string { return e.msg }
