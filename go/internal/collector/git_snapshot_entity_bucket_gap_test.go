// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/hcl"
	jsonparser "github.com/eshu-hq/eshu/go/internal/parser/json"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestSnapshotEmitsTerraformBlockAndCloudFormationExtendedContentEntities is
// the issue #5531 regression: content/shape's contentEntityBuckets has
// registered "terraform_blocks" -> TerraformBlock and the three CloudFormation
// extended buckets ("cloudformation_conditions" -> CloudFormationCondition,
// "cloudformation_cross_stack_imports" -> CloudFormationImport,
// "cloudformation_cross_stack_exports" -> CloudFormationExport) since before
// this fix, and both the HCL and JSON parsers populate those payload buckets
// for real Terraform/CloudFormation source (see hcl/parser.go's
// parseTerraformBlocks and json/language.go's CloudFormation branch). But the
// collector's snapshotEntityBuckets -- the SECOND, independently
// hand-maintained bucket->label list entityBucketsFromParsed walks -- never
// registered these four buckets, so the collector silently dropped every one
// of these entities before a content_entity fact was ever built: no fact, no
// content row, no graph node, no error, no failing unit test. content/shape's
// own materialize_test.go (TestMaterializeCarriesCloudFormationExtendedBuckets
// and the TerraformBlock case) and query/entity_content_iac_fallback_test.go
// already prove the shape and query layers treat these as live, intended
// entity types; this test proves the collector emission step that sits
// between the real parser and those layers.
func TestSnapshotEmitsTerraformBlockAndCloudFormationExtendedContentEntities(t *testing.T) {
	t.Parallel()

	t.Run("terraform_block", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "main.tf")
		body := `terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}
`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write main.tf fixture: %v", err)
		}

		payload, err := hcl.Parse(path, false, shared.Options{})
		if err != nil {
			t.Fatalf("hcl.Parse() error = %v", err)
		}

		snapshots := snapshotsFromParsedPayload(t, "main.tf", payload)
		if !hasEntityType(snapshots, "TerraformBlock") {
			t.Fatalf("no TerraformBlock content entity emitted from main.tf; the collector's snapshotEntityBuckets is missing the \"terraform_blocks\" parser bucket (entityBucketsFromParsed silently drops it, so no graph node ever materializes). Got snapshots: %+v", snapshots)
		}
	})

	t.Run("cloudformation_extended_buckets", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "stack.json")
		body := `{
  "AWSTemplateFormatVersion": "2010-09-09",
  "Conditions": {
    "IsProd": {"Fn::Equals": ["a", "b"]}
  },
  "Resources": {
    "Queue": {
      "Type": "AWS::SQS::Queue",
      "Properties": {
        "QueueName": {"Fn::ImportValue": "SharedQueueName"}
      }
    }
  },
  "Outputs": {
    "BucketArn": {
      "Export": {"Name": "Stack-BucketArn"}
    }
  }
}
`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write stack.json fixture: %v", err)
		}

		payload, err := jsonparser.Parse(path, false, shared.Options{}, jsonparser.Config{})
		if err != nil {
			t.Fatalf("jsonparser.Parse() error = %v", err)
		}

		snapshots := snapshotsFromParsedPayload(t, "stack.json", payload)
		for _, wantLabel := range []string{"CloudFormationCondition", "CloudFormationImport", "CloudFormationExport"} {
			if !hasEntityType(snapshots, wantLabel) {
				t.Errorf("no %s content entity emitted from stack.json; the collector's snapshotEntityBuckets is missing the matching CloudFormation parser bucket (entityBucketsFromParsed silently drops it, so no graph node ever materializes). Got snapshots: %+v", wantLabel, snapshots)
			}
		}
	})
}

// hasEntityType reports whether any snapshot carries the given EntityType.
func hasEntityType(snapshots []ContentEntitySnapshot, entityType string) bool {
	for i := range snapshots {
		if snapshots[i].EntityType == entityType {
			return true
		}
	}
	return false
}
