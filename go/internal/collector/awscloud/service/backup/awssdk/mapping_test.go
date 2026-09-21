// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsbackup "github.com/aws/aws-sdk-go-v2/service/backup"
	awsbackuptypes "github.com/aws/aws-sdk-go-v2/service/backup/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestConditionTypeOperator asserts every AWS Backup ListOfTags ConditionType
// enum value maps to the camelCase operator vocabulary the scanner persists,
// matching the Conditions-block operators in mergeTagConditions. Unknown or
// empty values fall back to the raw enum string so a future AWS enum
// expansion is recorded faithfully rather than mislabeled.
func TestConditionTypeOperator(t *testing.T) {
	cases := []struct {
		name string
		in   awsbackuptypes.ConditionType
		want string
	}{
		{name: "stringequals", in: awsbackuptypes.ConditionType("STRINGEQUALS"), want: "StringEquals"},
		{name: "stringnotequals", in: awsbackuptypes.ConditionType("STRINGNOTEQUALS"), want: "StringNotEquals"},
		{name: "stringlike", in: awsbackuptypes.ConditionType("STRINGLIKE"), want: "StringLike"},
		{name: "stringnotlike", in: awsbackuptypes.ConditionType("STRINGNOTLIKE"), want: "StringNotLike"},
		{name: "empty defaults to stringequals", in: awsbackuptypes.ConditionType(""), want: "StringEquals"},
		{name: "unknown future enum kept raw", in: awsbackuptypes.ConditionType("STRINGUNKNOWN"), want: "STRINGUNKNOWN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := conditionTypeOperator(tc.in); got != tc.want {
				t.Fatalf("conditionTypeOperator(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestClientListBackupSelectionsMapsNonEqualsListOfTagsOperator proves the
// adapter derives the persisted operator from the source ConditionType for a
// ListOfTags entry that is not StringEquals. Before the fix the operator was
// hardcoded to "StringEquals", silently mislabeling negated tag selections.
func TestClientListBackupSelectionsMapsNonEqualsListOfTagsOperator(t *testing.T) {
	planID := "plan-neq"
	roleARN := "arn:aws:iam::123456789012:role/backup"
	fake := &fakeBackupAPI{
		listBackupSelections: []*awsbackup.ListBackupSelectionsOutput{{
			BackupSelectionsList: []awsbackuptypes.BackupSelectionsListMember{{
				BackupPlanId:  aws.String(planID),
				SelectionId:   aws.String("sel-neq"),
				SelectionName: aws.String("not-prod"),
				IamRoleArn:    aws.String(roleARN),
				CreationDate:  aws.Time(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)),
			}},
		}},
		getBackupSelections: map[string]*awsbackup.GetBackupSelectionOutput{
			"sel-neq": {
				BackupSelection: &awsbackuptypes.BackupSelection{
					IamRoleArn:    aws.String(roleARN),
					SelectionName: aws.String("not-prod"),
					ListOfTags: []awsbackuptypes.Condition{{
						ConditionType:  awsbackuptypes.ConditionType("STRINGNOTLIKE"),
						ConditionKey:   aws.String("aws:ResourceTag/env"),
						ConditionValue: aws.String("prod*"),
					}},
				},
			},
		},
	}
	adapter := &Client{
		client:   fake,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceBackup},
	}
	selections, err := adapter.ListBackupSelections(context.Background(), planID)
	if err != nil {
		t.Fatalf("ListBackupSelections() error = %v", err)
	}
	if got, want := len(selections), 1; got != want {
		t.Fatalf("len(selections) = %d, want %d", got, want)
	}
	conds := selections[0].TagConditions
	if got, want := len(conds), 1; got != want {
		t.Fatalf("len(TagConditions) = %d, want %d", got, want)
	}
	if got, want := conds[0].Operator, "StringNotLike"; got != want {
		t.Fatalf("ListOfTags operator = %q, want %q (must derive from ConditionType, not be hardcoded)", got, want)
	}
	if got, want := conds[0].Key, "aws:ResourceTag/env"; got != want {
		t.Fatalf("ListOfTags key = %q, want %q", got, want)
	}
	if got, want := conds[0].Value, "prod*"; got != want {
		t.Fatalf("ListOfTags value = %q, want %q", got, want)
	}
}
