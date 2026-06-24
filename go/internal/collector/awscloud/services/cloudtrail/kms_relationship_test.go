// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudtrail

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestTrailKMSRelationshipTargetFields proves the trail-to-KMS relationship
// always tags TargetType as aws_kms_key, always carries the raw key identifier
// as TargetResourceID, and only populates TargetARN when the CloudTrail
// KmsKeyId is actually ARN-shaped. A bare key id or alias must not be treated
// as an ARN, so downstream consumers never see a non-ARN value in target_arn.
func TestTrailKMSRelationshipTargetFields(t *testing.T) {
	const trailARN = "arn:aws:cloudtrail:us-east-1:123456789012:trail/management"
	cases := []struct {
		name      string
		keyID     string
		wantARN   string
		wantTrgID string
	}{
		{
			name:      "arn key id sets target arn",
			keyID:     "arn:aws:kms:us-east-1:123456789012:key/abcd-1234",
			wantARN:   "arn:aws:kms:us-east-1:123456789012:key/abcd-1234",
			wantTrgID: "arn:aws:kms:us-east-1:123456789012:key/abcd-1234",
		},
		{
			name:      "bare key id leaves target arn empty",
			keyID:     "abcd-1234-5678-90ab-cdef",
			wantARN:   "",
			wantTrgID: "abcd-1234-5678-90ab-cdef",
		},
		{
			name:      "alias leaves target arn empty",
			keyID:     "alias/my-cloudtrail-key",
			wantARN:   "",
			wantTrgID: "alias/my-cloudtrail-key",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := trailRelationships(testBoundary(), Trail{
				ARN:      trailARN,
				Name:     "management",
				KMSKeyID: tc.keyID,
			})
			rel := relationshipOfType(t, out, awscloud.RelationshipCloudTrailTrailUsesKMSKey)
			assertKMSRelationship(t, rel, tc.wantTrgID, tc.wantARN)
		})
	}
}

// TestEventDataStoreKMSRelationshipTargetFields proves the Lake event data
// store KMS relationship follows the same convention as trailRelationships:
// TargetType is aws_kms_key, TargetResourceID is the raw key identifier, and
// TargetARN is only set for ARN-shaped identifiers.
func TestEventDataStoreKMSRelationshipTargetFields(t *testing.T) {
	const storeARN = "arn:aws:cloudtrail:us-east-1:123456789012:eventdatastore/edsabc"
	cases := []struct {
		name      string
		keyID     string
		wantARN   string
		wantTrgID string
	}{
		{
			name:      "arn key id sets target arn",
			keyID:     "arn:aws:kms:us-east-1:123456789012:key/store-1111",
			wantARN:   "arn:aws:kms:us-east-1:123456789012:key/store-1111",
			wantTrgID: "arn:aws:kms:us-east-1:123456789012:key/store-1111",
		},
		{
			name:      "bare key id leaves target arn empty",
			keyID:     "store-1111-2222",
			wantARN:   "",
			wantTrgID: "store-1111-2222",
		},
		{
			name:      "alias leaves target arn empty",
			keyID:     "alias/lake-store-key",
			wantARN:   "",
			wantTrgID: "alias/lake-store-key",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rel, ok := eventDataStoreKMSRelationship(testBoundary(), EventDataStore{
				ARN:      storeARN,
				KMSKeyID: tc.keyID,
			})
			if !ok {
				t.Fatalf("eventDataStoreKMSRelationship() ok = false, want true")
			}
			if rel.RelationshipType != awscloud.RelationshipCloudTrailEventDataStoreUsesKMSKey {
				t.Fatalf("relationship_type = %q, want %q", rel.RelationshipType, awscloud.RelationshipCloudTrailEventDataStoreUsesKMSKey)
			}
			assertKMSRelationship(t, rel, tc.wantTrgID, tc.wantARN)
		})
	}
}

func relationshipOfType(t *testing.T, out []awscloud.RelationshipObservation, relationshipType string) awscloud.RelationshipObservation {
	t.Helper()
	for _, rel := range out {
		if rel.RelationshipType == relationshipType {
			return rel
		}
	}
	t.Fatalf("missing relationship_type %q (n=%d)", relationshipType, len(out))
	return awscloud.RelationshipObservation{}
}

func assertKMSRelationship(t *testing.T, rel awscloud.RelationshipObservation, wantTargetID, wantTargetARN string) {
	t.Helper()
	if got, want := rel.TargetType, "aws_kms_key"; got != want {
		t.Fatalf("TargetType = %q, want %q", got, want)
	}
	if got := rel.TargetResourceID; got != wantTargetID {
		t.Fatalf("TargetResourceID = %q, want %q", got, wantTargetID)
	}
	if got := rel.TargetARN; got != wantTargetARN {
		t.Fatalf("TargetARN = %q, want %q", got, wantTargetARN)
	}
}
