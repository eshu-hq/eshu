// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractAWSCloudImageEdgeRowsResolvesLambdaDigest is the positive case:
// an exact registry+repository@digest reference on a resolved
// lambda_function_uses_image relationship, with its source CloudResource
// present in the join index, produces exactly one edge row targeting the
// deterministic :ContainerImage uid.
func TestExtractAWSCloudImageEdgeRowsResolvesLambdaDigest(t *testing.T) {
	t.Parallel()

	fnARN := "arn:aws:lambda:us-east-1:123456789012:function:demo"
	resources := []facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id": "123456789012", "region": "us-east-1",
			"resource_type": "aws_lambda_function", "resource_id": fnARN, "arn": fnARN,
		}),
	}
	relationships := []facts.Envelope{
		awsRelationshipEnvelope(map[string]any{
			"account_id": "123456789012", "region": "us-east-1",
			"relationship_type":  lambdaFunctionUsesImageRelationshipType,
			"source_resource_id": fnARN, "source_arn": fnARN,
			"target_resource_id": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
			"target_type":        "container_image",
			"attributes": map[string]any{
				"package_type":       "Image",
				"resolved_image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc",
			},
		}),
	}

	rows, tally, quarantined, err := ExtractAWSCloudImageEdgeRows(resources, relationships)
	if err != nil {
		t.Fatalf("ExtractAWSCloudImageEdgeRows() error = %v, want nil", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %v, want none", quarantined)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	wantUID, ok := containerImageNodeUIDFromDigestRef("123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc")
	if !ok {
		t.Fatalf("containerImageNodeUIDFromDigestRef() ok = false")
	}
	if got := anyToString(row["target_uid"]); got != wantUID {
		t.Fatalf("target_uid = %q, want %q", got, wantUID)
	}
	if got := anyToString(row["resolution_mode"]); got != awsCloudImageResolutionMode {
		t.Fatalf("resolution_mode = %q, want %q", got, awsCloudImageResolutionMode)
	}
	if tally.resolved != 1 {
		t.Fatalf("tally.resolved = %d, want 1", tally.resolved)
	}
}

// TestExtractAWSCloudImageEdgeRowsSkipsECSTaskDefinitionAsPostgresOnlyPolicy
// is the negative case for the #5472 EXACT-ONLY policy decision: a tag-only
// ecs_task_definition_uses_image relationship never produces a row, even when
// its source resolves, and is tallied under the policy-skip reason rather than
// counted as an unresolved failure.
func TestExtractAWSCloudImageEdgeRowsSkipsECSTaskDefinitionAsPostgresOnlyPolicy(t *testing.T) {
	t.Parallel()

	tdARN := "arn:aws:ecs:us-east-1:123456789012:task-definition/demo:1"
	resources := []facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id": "123456789012", "region": "us-east-1",
			"resource_type": "aws_ecs_task_definition", "resource_id": tdARN, "arn": tdARN,
		}),
	}
	relationships := []facts.Envelope{
		awsRelationshipEnvelope(map[string]any{
			"account_id": "123456789012", "region": "us-east-1",
			"relationship_type":  ecsTaskDefinitionUsesImageRelationshipType,
			"source_resource_id": tdARN, "source_arn": tdARN,
			"target_resource_id": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
			"target_type":        "container_image",
		}),
	}

	rows, tally, _, err := ExtractAWSCloudImageEdgeRows(resources, relationships)
	if err != nil {
		t.Fatalf("ExtractAWSCloudImageEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 (tag-only stays postgres-only)", len(rows))
	}
	if got := tally.skipped[awsCloudImageSkipTagOnlyPostgresOnly]; got != 1 {
		t.Fatalf("tally.skipped[tag_only_postgres_only_policy] = %d, want 1", got)
	}
}

// TestExtractAWSCloudImageEdgeRowsSkipsAmbiguousUnresolvedDigest is the
// ambiguous case: a lambda_function_uses_image relationship with no
// resolved_image_uri (a non-container-image function, or one AWS has not yet
// resolved a digest for) never produces a fabricated or guessed edge.
func TestExtractAWSCloudImageEdgeRowsSkipsAmbiguousUnresolvedDigest(t *testing.T) {
	t.Parallel()

	fnARN := "arn:aws:lambda:us-east-1:123456789012:function:demo"
	resources := []facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id": "123456789012", "region": "us-east-1",
			"resource_type": "aws_lambda_function", "resource_id": fnARN, "arn": fnARN,
		}),
	}
	relationships := []facts.Envelope{
		awsRelationshipEnvelope(map[string]any{
			"account_id": "123456789012", "region": "us-east-1",
			"relationship_type":  lambdaFunctionUsesImageRelationshipType,
			"source_resource_id": fnARN, "source_arn": fnARN,
			"target_resource_id": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
			"target_type":        "container_image",
			"attributes": map[string]any{
				"package_type": "Image",
			},
		}),
	}

	rows, tally, _, err := ExtractAWSCloudImageEdgeRows(resources, relationships)
	if err != nil {
		t.Fatalf("ExtractAWSCloudImageEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
	if got := tally.skipped[awsCloudImageSkipNoDigest]; got != 1 {
		t.Fatalf("tally.skipped[no_resolved_digest] = %d, want 1", got)
	}
}

// TestExtractAWSCloudImageEdgeRowsSkipsUnresolvedSource proves a source
// endpoint that was not scanned in this generation degrades gracefully
// (counted, not fabricated).
func TestExtractAWSCloudImageEdgeRowsSkipsUnresolvedSource(t *testing.T) {
	t.Parallel()

	fnARN := "arn:aws:lambda:us-east-1:123456789012:function:demo"
	relationships := []facts.Envelope{
		awsRelationshipEnvelope(map[string]any{
			"account_id": "123456789012", "region": "us-east-1",
			"relationship_type":  lambdaFunctionUsesImageRelationshipType,
			"source_resource_id": fnARN, "source_arn": fnARN,
			"target_resource_id": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
			"target_type":        "container_image",
			"attributes": map[string]any{
				"resolved_image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc",
			},
		}),
	}

	rows, tally, _, err := ExtractAWSCloudImageEdgeRows(nil, relationships)
	if err != nil {
		t.Fatalf("ExtractAWSCloudImageEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
	if got := tally.skipped[awsCloudImageSkipSourceUnresolved]; got != 1 {
		t.Fatalf("tally.skipped[source_unresolved] = %d, want 1", got)
	}
}

// TestExtractAWSCloudImageEdgeRowsIgnoresUnrelatedRelationshipTypes proves a
// third-party relationship_type (out of this domain's scope) is silently
// ignored rather than skipped/tallied — it belongs to a different domain.
func TestExtractAWSCloudImageEdgeRowsIgnoresUnrelatedRelationshipTypes(t *testing.T) {
	t.Parallel()

	fnARN := "arn:aws:lambda:us-east-1:123456789012:function:demo"
	roleARN := "arn:aws:iam::123456789012:role/demo"
	relationships := []facts.Envelope{
		awsRelationshipEnvelope(map[string]any{
			"account_id": "123456789012", "region": "us-east-1",
			"relationship_type":  "lambda_function_uses_execution_role",
			"source_resource_id": fnARN, "source_arn": fnARN,
			"target_resource_id": roleARN, "target_arn": roleARN,
			"target_type": "aws_iam_role",
		}),
	}

	rows, tally, _, err := ExtractAWSCloudImageEdgeRows(nil, relationships)
	if err != nil {
		t.Fatalf("ExtractAWSCloudImageEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
	if tally.totalSkipped() != 0 {
		t.Fatalf("tally.totalSkipped() = %d, want 0 (out-of-scope type is ignored, not skipped)", tally.totalSkipped())
	}
}

func TestContainerImageNodeUIDFromDigestRef(t *testing.T) {
	t.Parallel()

	t.Run("valid digest reference", func(t *testing.T) {
		got, ok := containerImageNodeUIDFromDigestRef(
			"123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc",
		)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := "oci-descriptor://123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("registry, repository, and digest are lowercased to match the OCI registry collector's normalization", func(t *testing.T) {
		// internal/collector/ociregistry/identity.go NormalizeRepositoryIdentity /
		// normalizeDigest unconditionally lowercase the scanned registry,
		// repository, and digest before computing the real :ContainerImage
		// node's repository_id/descriptor identity. resolved_image_uri comes
		// straight from the Lambda GetFunction API response and is never run
		// through that collector normalization, so this function must
		// lowercase independently or a mixed-case registry/repository/digest
		// would compute a uid that can never MATCH the real node.
		got, ok := containerImageNodeUIDFromDigestRef("Registry.Example.Com/Demo@SHA256:CC")
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := "oci-descriptor://registry.example.com/demo@sha256:cc"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("tag-only reference (no digest) is not resolvable", func(t *testing.T) {
		_, ok := containerImageNodeUIDFromDigestRef("demo:latest")
		if ok {
			t.Fatal("ok = true, want false for a tag-only reference")
		}
	})

	t.Run("empty reference is not resolvable", func(t *testing.T) {
		_, ok := containerImageNodeUIDFromDigestRef("")
		if ok {
			t.Fatal("ok = true, want false for an empty reference")
		}
	})

	t.Run("digest-only reference with no repository is not resolvable", func(t *testing.T) {
		_, ok := containerImageNodeUIDFromDigestRef("@sha256:cc")
		if ok {
			t.Fatal("ok = true, want false for an empty repository portion")
		}
	})
}
