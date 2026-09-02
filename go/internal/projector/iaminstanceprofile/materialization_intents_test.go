// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iaminstanceprofile

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	testScopeID      = "aws:123456789012:aws-global:iam"
	testGenerationID = "aws-generation-1"
)

func instanceProfileEnvelope(factID, sourceSystem, collectorKind string, roleARNs ...string) facts.Envelope {
	roles := make([]any, 0, len(roleARNs))
	for _, arn := range roleARNs {
		roles = append(roles, arn)
	}
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       testScopeID,
		GenerationID:  testGenerationID,
		FactKind:      facts.AWSResourceFactKind,
		SchemaVersion: facts.AWSResourceSchemaVersion,
		CollectorKind: collectorKind,
		ObservedAt:    time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: sourceSystem,
		},
		Payload: map[string]any{
			"account_id":    "123456789012",
			"region":        "aws-global",
			"resource_type": "aws_iam_instance_profile",
			"resource_id":   "arn:aws:iam::123456789012:instance-profile/app",
			"arn":           "arn:aws:iam::123456789012:instance-profile/app",
			"role_arns":     roles,
		},
	}
}

func iamRoleResourceEnvelope(factID string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       testScopeID,
		GenerationID:  testGenerationID,
		FactKind:      facts.AWSResourceFactKind,
		SchemaVersion: facts.AWSResourceSchemaVersion,
		CollectorKind: "aws_cloud",
		SourceRef: facts.Ref{
			SourceSystem: "aws",
		},
		Payload: map[string]any{
			"account_id":    "123456789012",
			"region":        "aws-global",
			"resource_type": "aws_iam_role",
			"resource_id":   "arn:aws:iam::123456789012:role/app",
			"arn":           "arn:aws:iam::123456789012:role/app",
		},
	}
}

// TestBuildIAMInstanceProfileRoleMaterializationReducerIntent proves the
// builder triggers only on an aws_resource fact whose decoded resource_type is
// aws_iam_instance_profile, anchors to the earliest such fact in original
// input order (a no-role profile included, for stale-edge retraction), keys
// the intent on the shared aws_resource_materialization entity so the reducer
// handler's readiness gate resolves the canonical-nodes phase the AWS node
// builders publish, and derives the two-tier source-system label
// (SourceRef.SourceSystem trimmed, falling back to a trimmed CollectorKind)
// the root awsCloudRuntimeDriftSourceSystem helper produced before the
// extraction.
func TestBuildIAMInstanceProfileRoleMaterializationReducerIntent(t *testing.T) {
	t.Parallel()

	t.Run("queues from instance-profile presence, anchored to the earliest profile fact", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			instanceProfileEnvelope("fact-profile-empty", "aws", "aws_cloud"),
			instanceProfileEnvelope("fact-profile-role", "aws", "aws_cloud",
				"arn:aws:iam::123456789012:role/app"),
		})
		got, ok := BuildIAMInstanceProfileRoleMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID: testScopeID, GenerationID: testGenerationID,
			Domain:    reducer.DomainIAMInstanceProfileRoleMaterialization,
			EntityKey: "aws_resource_materialization:" + testScopeID,
			Reason:    "iam instance profiles observed",
			FactID:    "fact-profile-empty", SourceSystem: "aws",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("queues for a no-role profile so stale HAS_ROLE edges still retract", func(t *testing.T) {
		t.Parallel()
		// Retraction safety (#1299): a generation whose only instance profile
		// carries an empty role_arns list must still enqueue, because the
		// reducer handler has to retract HAS_ROLE edges a prior generation
		// wrote. Gating on non-empty role_arns would leak the stale edge.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			instanceProfileEnvelope("fact-profile-empty", "aws", "aws_cloud"),
		})
		got, ok := BuildIAMInstanceProfileRoleMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.FactID != "fact-profile-empty" {
			t.Fatalf("FactID = %q, want no-role profile to trigger stale-edge retract", got.FactID)
		}
	})

	t.Run("skips aws_resource facts that are not instance profiles", func(t *testing.T) {
		t.Parallel()
		// The predicate filters on the decoded resource_type FIELD, not on
		// aws_resource fact-kind presence: a generation carrying only an IAM
		// role resource must not enqueue, and a generation carrying a role
		// resource ahead of a profile must anchor to the profile.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			iamRoleResourceEnvelope("fact-role"),
		})
		got, ok := BuildIAMInstanceProfileRoleMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t), want zero intent and false", got, ok)
		}

		lookup = projectorintent.NewFactLookup([]facts.Envelope{
			iamRoleResourceEnvelope("fact-role"),
			instanceProfileEnvelope("fact-profile-after-role", "aws", "aws_cloud"),
		})
		got, ok = BuildIAMInstanceProfileRoleMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true (profile after a role resource)")
		}
		if got.FactID != "fact-profile-after-role" {
			t.Fatalf("FactID = %q, want the profile fact, not the earlier role resource", got.FactID)
		}
	})

	t.Run("skips an undecodable aws_resource fact", func(t *testing.T) {
		t.Parallel()
		// A profile fact missing a required identity field fails the typed
		// decode, so the predicate rejects it instead of enqueuing an intent
		// the reducer would dead-letter as input_invalid.
		invalid := instanceProfileEnvelope("fact-invalid-profile", "aws", "aws_cloud",
			"arn:aws:iam::123456789012:role/app")
		delete(invalid.Payload, "resource_id")
		lookup := projectorintent.NewFactLookup([]facts.Envelope{invalid})
		got, ok := BuildIAMInstanceProfileRoleMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t), want zero intent and false", got, ok)
		}
	})

	t.Run("prefers a trimmed SourceRef.SourceSystem for the source label", func(t *testing.T) {
		t.Parallel()
		// Two-tier pin, first tier: the pre-extraction root helper
		// (awsCloudRuntimeDriftSourceSystem) preferred a non-blank trimmed
		// SourceRef.SourceSystem over CollectorKind. A single-tier
		// CollectorKind-only substitute fails here.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			instanceProfileEnvelope("fact-profile-1", "  aws  ", "aws_cloud_collector"),
		})
		got, ok := BuildIAMInstanceProfileRoleMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.SourceSystem != "aws" {
			t.Fatalf("SourceSystem = %q, want %q (trimmed SourceRef.SourceSystem wins over CollectorKind)", got.SourceSystem, "aws")
		}
	})

	t.Run("falls back to a trimmed CollectorKind when SourceSystem is blank", func(t *testing.T) {
		t.Parallel()
		// Two-tier pin, second tier: a whitespace-only SourceRef.SourceSystem
		// falls through to the trimmed CollectorKind, and a blank source ref
		// does not drop the intent.
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			instanceProfileEnvelope("fact-profile-1", "  ", " aws_cloud_collector "),
		})
		got, ok := BuildIAMInstanceProfileRoleMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.SourceSystem != "aws_cloud_collector" {
			t.Fatalf("SourceSystem = %q, want %q (trimmed CollectorKind fallback)", got.SourceSystem, "aws_cloud_collector")
		}
	})
}
