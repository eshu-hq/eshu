// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package transfer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestPartitionDerivesFromBoundaryRegion(t *testing.T) {
	cases := map[string]string{
		"us-east-1":      "aws",
		"eu-west-3":      "aws",
		"us-gov-west-1":  "aws-us-gov",
		"us-gov-east-1":  "aws-us-gov",
		"cn-north-1":     "aws-cn",
		"cn-northwest-1": "aws-cn",
		"":               "aws",
	}
	for region, want := range cases {
		if got := partition(awscloud.Boundary{Region: region}); got != want {
			t.Fatalf("partition(region=%q) = %q, want %q", region, got, want)
		}
	}
}

func TestFirstPathSegmentSplitsHomeDirectory(t *testing.T) {
	cases := []struct {
		path          string
		wantSegment   string
		wantRemainder string
		wantOK        bool
	}{
		{"/landing-bucket/home/user", "landing-bucket", "home/user", true},
		{"/landing-bucket", "landing-bucket", "", true},
		{"landing-bucket/x", "landing-bucket", "x", true},
		{"/fs-0a1b2c3d/home", "fs-0a1b2c3d", "home", true},
		{"/", "", "", false},
		{"", "", "", false},
		{"   ", "", "", false},
	}
	for _, tc := range cases {
		segment, remainder, ok := firstPathSegment(tc.path)
		if segment != tc.wantSegment || remainder != tc.wantRemainder || ok != tc.wantOK {
			t.Fatalf("firstPathSegment(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.path, segment, remainder, ok, tc.wantSegment, tc.wantRemainder, tc.wantOK)
		}
	}
}

func TestLooksLikeEFSFileSystemID(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"fs-0a1b2c3d", true},          // 8-hex legacy EFS id
		{"fs-0123456789abcdef0", true}, // 17-hex EFS id
		{"landing-bucket", false},      // not an fs- value
		// An S3 bucket name may legally start with "fs-"; it must NOT be
		// misclassified as EFS (the bug this guards).
		{"fs-mybucket", false},
		{"fs-1", false},        // too short to be a real EFS id
		{"fs-0a1b2c3g", false}, // non-hex character
	}
	for _, tc := range cases {
		if got := looksLikeEFSFileSystemID(tc.value); got != tc.want {
			t.Fatalf("looksLikeEFSFileSystemID(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

// TestServerRelationshipsSourceMatchesNodeIdentity guards the join: server->*
// edges must be sourced on the identity the server resource node publishes
// (ARN-preferred), not the bare ServerID, or they dangle whenever the ARN is
// present.
func TestServerRelationshipsSourceMatchesNodeIdentity(t *testing.T) {
	server := Server{
		ServerID:       "s-abc",
		ARN:            "arn:aws:transfer:us-east-1:123456789012:server/s-abc",
		EndpointType:   "VPC_ENDPOINT",
		LoggingRoleARN: "arn:aws:iam::123456789012:role/transfer-logging",
		VPCEndpointID:  "vpce-0123456789abcdef0",
	}
	rels := serverRelationships(testBoundary(), server)
	if len(rels) == 0 {
		t.Fatal("expected at least one server relationship")
	}
	for _, rel := range rels {
		if rel.SourceResourceID != server.ARN {
			t.Fatalf("edge %q source_resource_id = %q, want the server ARN %q (else it dangles)",
				rel.RelationshipType, rel.SourceResourceID, server.ARN)
		}
	}
}

func TestUserResourceIDFallsBackToServerUserComposite(t *testing.T) {
	if got := userResourceID(User{ARN: "arn:aws:transfer:::user/s-1/u"}); got != "arn:aws:transfer:::user/s-1/u" {
		t.Fatalf("userResourceID(arn) = %q, want the ARN", got)
	}
	if got := userResourceID(User{ServerID: "s-1", UserName: "u"}); got != "s-1/u" {
		t.Fatalf("userResourceID(no arn) = %q, want %q", got, "s-1/u")
	}
	if got := userResourceID(User{}); got != "" {
		t.Fatalf("userResourceID(empty) = %q, want empty", got)
	}
}
