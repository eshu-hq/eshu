// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimageidentity

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBuildContainerImageIdentityReducerIntentNoFactNoIntent(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{FactKind: "file", Payload: map[string]any{"language": "go"}}})
	if _, ok := BuildContainerImageIdentityReducerIntent("scope-1", "gen-1", lookup); ok {
		t.Fatal("queued a container_image_identity intent without any identity-relevant fact")
	}
}

func TestBuildContainerImageIdentityReducerIntentEmptyGeneration(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup(nil)
	if _, ok := BuildContainerImageIdentityReducerIntent("scope-1", "gen-1", lookup); ok {
		t.Fatal("queued a container_image_identity intent for a generation with no facts at all")
	}
}

func TestBuildContainerImageIdentityReducerIntentFromOCIManifestFact(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: "file", Payload: map[string]any{"language": "go"}},
		{
			FactKind:      facts.OCIImageManifestFactKind,
			FactID:        "manifest-fact-1",
			SourceRef:     facts.Ref{SourceSystem: "  registry.example.com  "},
			CollectorKind: "oci_registry",
		},
	})
	intent, ok := BuildContainerImageIdentityReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for an oci.image_manifest fact")
	}
	if intent.Domain != reducer.DomainContainerImageIdentity {
		t.Fatalf("intent.Domain = %q, want container_image_identity", intent.Domain)
	}
	if intent.EntityKey != "container_image_identity:scope-1" {
		t.Fatalf("intent.EntityKey = %q", intent.EntityKey)
	}
	if intent.Reason != "container image identity evidence observed" {
		t.Fatalf("intent.Reason = %q", intent.Reason)
	}
	if intent.FactID != "manifest-fact-1" {
		t.Fatalf("intent.FactID = %q, want the manifest fact", intent.FactID)
	}
	// Proves the SourceSystem substitution decided during extraction: this
	// family's pre-extraction local helper (trim SourceRef.SourceSystem, else
	// trim CollectorKind) was byte-identical to projectorintent.SourceSystem,
	// so it was dropped in favor of the shared seam. A trimmed non-empty
	// SourceRef.SourceSystem wins over CollectorKind.
	if intent.SourceSystem != "registry.example.com" {
		t.Fatalf("intent.SourceSystem = %q, want the trimmed SourceRef.SourceSystem tier", intent.SourceSystem)
	}
}

func TestBuildContainerImageIdentityReducerIntentSourceSystemFallsBackToCollectorKind(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{
			FactKind:      facts.OCIImageManifestFactKind,
			FactID:        "manifest-fact-2",
			SourceRef:     facts.Ref{SourceSystem: "   "},
			CollectorKind: "  oci_registry  ",
		},
	})
	intent, ok := BuildContainerImageIdentityReducerIntent("scope-1", "gen-1", lookup)
	if !ok {
		t.Fatal("no intent queued for an oci.image_manifest fact")
	}
	if intent.SourceSystem != "oci_registry" {
		t.Fatalf("intent.SourceSystem = %q, want the trimmed CollectorKind fallback tier", intent.SourceSystem)
	}
}

func TestTriggerFactAWSRelationshipTargetingContainerImage(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactKind: facts.AWSRelationshipFactKind,
		Payload: map[string]any{
			"account_id":         "111111111111",
			"region":             "us-east-1",
			"relationship_type":  "USES_IMAGE",
			"source_resource_id": "arn:aws:ecs:us-east-1:111111111111:service/svc",
			"target_resource_id": "arn:aws:ecr:us-east-1:111111111111:repository/repo",
			"target_type":        "container_image",
		},
	}
	if !triggerFact(envelope) {
		t.Fatal("an aws_relationship fact targeting a container_image must trigger container_image_identity")
	}
}

func TestTriggerFactAWSRelationshipNotTargetingContainerImage(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactKind: facts.AWSRelationshipFactKind,
		Payload: map[string]any{
			"account_id":         "111111111111",
			"region":             "us-east-1",
			"relationship_type":  "USES_INSTANCE",
			"source_resource_id": "arn:aws:ecs:us-east-1:111111111111:service/svc",
			"target_resource_id": "arn:aws:ec2:us-east-1:111111111111:instance/i-1",
			"target_type":        "ec2_instance",
		},
	}
	if triggerFact(envelope) {
		t.Fatal("an aws_relationship fact targeting a non-container-image type must not trigger container_image_identity")
	}
}

func TestTriggerFactAWSRelationshipUndecodable(t *testing.T) {
	t.Parallel()

	// Missing the required source_arn/target_arn fields the typed decode
	// enforces; the trigger must discard the decode error and report false,
	// not panic or propagate the error.
	envelope := facts.Envelope{
		FactKind: facts.AWSRelationshipFactKind,
		Payload:  map[string]any{},
	}
	if triggerFact(envelope) {
		t.Fatal("an undecodable aws_relationship fact must not trigger container_image_identity")
	}
}

// TestTriggerFactDockerfileTombstoneRemoval covers the removal path moved
// here from the root package's dockerfile test file: a deleted Dockerfile
// must still trigger the domain so the retract-first pass clears the stale
// DERIVED_FROM edge. A tombstoned file fact can arrive with no
// parsed_file_data at all, so the trigger recognizes a Dockerfile by name.
func TestTriggerFactDockerfileTombstoneRemoval(t *testing.T) {
	t.Parallel()

	tombstone := facts.Envelope{
		FactID:       "fact-dockerfile-tombstone",
		ScopeID:      "repo://github.com/example/lineage-app",
		GenerationID: "gen-2",
		FactKind:     containerImageIdentityFileFactKind,
		ObservedAt:   time.Date(2026, time.July, 20, 10, 0, 0, 0, time.UTC),
		SourceRef:    facts.Ref{SourceSystem: "git"},
		IsTombstone:  true,
		Payload: map[string]any{
			"repo_id":       "repository:github.com/example/lineage-app",
			"path":          "/repo/Dockerfile",
			"relative_path": "Dockerfile",
			"name":          "Dockerfile",
			"language":      "dockerfile",
		},
	}

	if !triggerFact(tombstone) {
		t.Fatal("a tombstoned Dockerfile file fact must trigger the container image identity domain so the stale DERIVED_FROM edge is retracted")
	}
}
