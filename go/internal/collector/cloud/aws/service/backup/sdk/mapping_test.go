// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsbackup "github.com/aws/aws-sdk-go-v2/service/backup"
	awsbackuptypes "github.com/aws/aws-sdk-go-v2/service/backup/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
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
				BackupPlanId:  awsv2.String(planID),
				SelectionId:   awsv2.String("sel-neq"),
				SelectionName: awsv2.String("not-prod"),
				IamRoleArn:    awsv2.String(roleARN),
				CreationDate:  awsv2.Time(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)),
			}},
		}},
		getBackupSelections: map[string]*awsbackup.GetBackupSelectionOutput{
			"sel-neq": {
				BackupSelection: &awsbackuptypes.BackupSelection{
					IamRoleArn:    awsv2.String(roleARN),
					SelectionName: awsv2.String("not-prod"),
					ListOfTags: []awsbackuptypes.Condition{{
						ConditionType:  awsbackuptypes.ConditionType("STRINGNOTLIKE"),
						ConditionKey:   awsv2.String("aws:ResourceTag/env"),
						ConditionValue: awsv2.String("prod*"),
					}},
				},
			},
		},
	}
	adapter := &Client{
		client:   fake,
		boundary: aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceBackup},
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
