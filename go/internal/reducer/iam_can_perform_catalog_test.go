// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"
)

// TestIAMCanPerformCatalogIsWellFormed locks the closed CAN_PERFORM action
// vocabulary against the most likely authoring mistakes: a non-lowercase action
// (which would never match the lowercased aws_iam_permission actions), an empty
// action or expected type, or a duplicate action. The catalog is the security
// review focus, so a malformed entry is a correctness bug, not a style nit.
func TestIAMCanPerformCatalogIsWellFormed(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{})
	validTypes := map[string]struct{}{
		iamCanPerformResourceTypeS3Bucket:    {},
		iamCanPerformResourceTypeKMSKey:      {},
		iamCanPerformResourceTypeSecret:      {},
		iamCanPerformResourceTypeSSMParam:    {},
		iamCanPerformResourceTypeDynamoDB:    {},
		iamCanPerformResourceTypeEC2Instance: {},
		iamCanPerformResourceTypeRDSInstance: {},
		iamCanPerformResourceTypeLambdaFunc:  {},
	}
	for _, entry := range iamCanPerformCatalog {
		if strings.TrimSpace(entry.Action) == "" {
			t.Fatalf("catalog entry has empty action: %+v", entry)
		}
		if entry.Action != strings.ToLower(entry.Action) {
			t.Fatalf("catalog action %q is not lowercase (aws_iam_permission lowercases actions)", entry.Action)
		}
		if _, dup := seen[entry.Action]; dup {
			t.Fatalf("duplicate catalog action %q", entry.Action)
		}
		seen[entry.Action] = struct{}{}
		if _, ok := validTypes[entry.ExpectedResourceType]; !ok {
			t.Fatalf("catalog action %q has unrecognized expected resource type %q", entry.Action, entry.ExpectedResourceType)
		}
	}
}

// TestIAMCanPerformCatalogShipsStarterVocabulary pins the §3 starter set so a
// later edit that drops or renames a starter action is caught. The catalog is
// expandable, but the documented starter vocabulary is a contract.
func TestIAMCanPerformCatalogShipsStarterVocabulary(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"s3:getobject":                  iamCanPerformResourceTypeS3Bucket,
		"s3:putobject":                  iamCanPerformResourceTypeS3Bucket,
		"s3:deletebucket":               iamCanPerformResourceTypeS3Bucket,
		"kms:decrypt":                   iamCanPerformResourceTypeKMSKey,
		"secretsmanager:getsecretvalue": iamCanPerformResourceTypeSecret,
		"ssm:getparameter":              iamCanPerformResourceTypeSSMParam,
		"dynamodb:getitem":              iamCanPerformResourceTypeDynamoDB,
		"ec2:terminateinstances":        iamCanPerformResourceTypeEC2Instance,
		"rds:deletedbinstance":          iamCanPerformResourceTypeRDSInstance,
	}
	byAction := iamCanPerformCatalogByAction()
	if len(byAction) < len(want) {
		t.Fatalf("catalog has %d entries, fewer than the %d starter actions", len(byAction), len(want))
	}
	for action, typ := range want {
		entry, ok := byAction[action]
		if !ok {
			t.Fatalf("starter action %q missing from catalog", action)
		}
		if entry.ExpectedResourceType != typ {
			t.Fatalf("action %q expected type = %q, want %q", action, entry.ExpectedResourceType, typ)
		}
	}
}

// TestIAMCanPerformCatalogActionsSet proves the action-union helper reflects the
// catalog exactly so the Deny / conditioned-skip accounting keys on the right set.
func TestIAMCanPerformCatalogActionsSet(t *testing.T) {
	t.Parallel()

	actions := iamCanPerformCatalogActions()
	if len(actions) != len(iamCanPerformCatalog) {
		t.Fatalf("action set size = %d, want %d", len(actions), len(iamCanPerformCatalog))
	}
	if _, ok := actions["kms:decrypt"]; !ok {
		t.Fatal("catalog action set must include kms:decrypt")
	}
	if _, ok := actions["iam:passrole"]; ok {
		t.Fatal("CAN_PERFORM catalog must not include escalation-only actions like iam:passrole")
	}
}

// TestSortedCanPerformActions proves the action property is deduped and sorted so
// the idempotent SET is byte-stable across retries.
func TestSortedCanPerformActions(t *testing.T) {
	t.Parallel()

	got := sortedCanPerformActions(map[string]struct{}{
		"s3:putobject": {},
		"s3:getobject": {},
	})
	if len(got) != 2 || got[0] != "s3:getobject" || got[1] != "s3:putobject" {
		t.Fatalf("sortedCanPerformActions = %v, want [s3:getobject s3:putobject]", got)
	}
	if sortedCanPerformActions(nil) != nil {
		t.Fatal("empty action set must return nil")
	}
}
