// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package controltower

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestResolveOrganizationsTarget(t *testing.T) {
	cases := []struct {
		name      string
		targetARN string
		wantOK    bool
		wantID    string
		wantType  string
	}{
		{
			name:      "ou",
			targetARN: "arn:aws:organizations::123456789012:ou/o-exampleorgid/ou-root-platform",
			wantOK:    true,
			wantID:    "ou-root-platform",
			wantType:  awscloud.ResourceTypeOrganizationsOrganizationalUnit,
		},
		{
			name:      "account",
			targetARN: "arn:aws:organizations::123456789012:account/o-exampleorgid/111122223333",
			wantOK:    true,
			wantID:    "111122223333",
			wantType:  awscloud.ResourceTypeOrganizationsAccount,
		},
		{
			name:      "root",
			targetARN: "arn:aws:organizations::123456789012:root/o-exampleorgid/r-root",
			wantOK:    true,
			wantID:    "r-root",
			wantType:  awscloud.ResourceTypeOrganizationsRoot,
		},
		{
			name:      "govcloud partition preserved in arn",
			targetARN: "arn:aws-us-gov:organizations::123456789012:ou/o-gov/ou-gov-team",
			wantOK:    true,
			wantID:    "ou-gov-team",
			wantType:  awscloud.ResourceTypeOrganizationsOrganizationalUnit,
		},
		{name: "empty", targetARN: "", wantOK: false},
		{name: "not an arn", targetARN: "ou-root-platform", wantOK: false},
		{name: "not organizations service", targetARN: "arn:aws:controltower:us-east-1:123456789012:enabledcontrol/x", wantOK: false},
		{name: "unknown family", targetARN: "arn:aws:organizations::123456789012:policy/o-x/p-1", wantOK: false},
		{name: "no resource id segment", targetARN: "arn:aws:organizations::123456789012:ou", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := resolveOrganizationsTarget(tc.targetARN)
			if ok != tc.wantOK {
				t.Fatalf("resolveOrganizationsTarget(%q) ok = %v, want %v", tc.targetARN, ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if got.ResourceID != tc.wantID {
				t.Fatalf("ResourceID = %q, want %q", got.ResourceID, tc.wantID)
			}
			if got.ResourceType != tc.wantType {
				t.Fatalf("ResourceType = %q, want %q", got.ResourceType, tc.wantType)
			}
			if got.ARN != tc.targetARN {
				t.Fatalf("ARN = %q, want %q (original ARN preserved for provenance)", got.ARN, tc.targetARN)
			}
		})
	}
}
