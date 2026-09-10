// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// postureEnvelope builds an ec2_instance_posture envelope with the given fact
// ID, source-ref system, collector kind, and (optionally blank)
// instance_profile_arn payload field.
func postureEnvelope(factID, sourceSystem, collectorKind, instanceProfileARN string) facts.Envelope {
	env := facts.Envelope{
		FactID:        factID,
		FactKind:      facts.EC2InstancePostureFactKind,
		CollectorKind: collectorKind,
		SourceRef:     facts.Ref{SourceSystem: sourceSystem},
		Payload: map[string]any{
			"account_id": "123456789012",
			"region":     "us-east-1",
		},
	}
	if instanceProfileARN != "" {
		env.Payload["instance_profile_arn"] = instanceProfileARN
	}
	return env
}

// TestEC2PostureIntentBuildersPreserveContract proves the four
// posture-presence-triggered builders share the
// ec2_instance_node_materialization:<scope> entity key, anchor to the
// earliest posture fact, and apply the source-ref-first, trimmed-collector-
// fallback source rule.
func TestEC2PostureIntentBuildersPreserveContract(t *testing.T) {
	t.Parallel()

	const (
		scopeID      = "aws:123456789012:us-east-1:ec2"
		generationID = "aws-generation-1"
	)
	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		{FactKind: facts.AWSResourceFactKind, FactID: "resource-first"},
		postureEnvelope("posture-first", " aws ", " aws_cloud ", ""),
		postureEnvelope("posture-second", "ignored-source", "ignored-collector", ""),
	})

	tests := []struct {
		name      string
		build     func(string, string, projectorintent.FactLookup) (projectorintent.ReducerIntent, bool)
		domain    reducer.Domain
		entityKey string
		reason    string
	}{
		{
			name:      "instance node materialization",
			build:     BuildInstanceNodeMaterializationReducerIntent,
			domain:    reducer.DomainEC2InstanceNodeMaterialization,
			entityKey: "ec2_instance_node_materialization:" + scopeID,
			reason:    "ec2 instance posture facts observed",
		},
		{
			name:      "instance identity materialization",
			build:     BuildInstanceIdentityMaterializationReducerIntent,
			domain:    reducer.DomainEC2InstanceIdentityMaterialization,
			entityKey: "ec2_instance_node_materialization:" + scopeID,
			reason:    "ec2 instance posture observed for ec2 instance identity projection",
		},
		{
			name:      "internet exposure materialization",
			build:     BuildInternetExposureMaterializationReducerIntent,
			domain:    reducer.DomainEC2InternetExposureMaterialization,
			entityKey: "ec2_instance_node_materialization:" + scopeID,
			reason:    "ec2 instance posture observed",
		},
		{
			name:      "block device kms posture materialization",
			build:     BuildBlockDeviceKMSPostureMaterializationReducerIntent,
			domain:    reducer.DomainEC2BlockDeviceKMSPostureMaterialization,
			entityKey: "ec2_block_device_kms_posture_materialization:" + scopeID,
			reason:    "ec2 block-device posture observed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := test.build(scopeID, generationID, lookup)
			if !ok {
				t.Fatal("builder returned ok=false, want true")
			}
			want := projectorintent.ReducerIntent{
				ScopeID: scopeID, GenerationID: generationID,
				Domain:    test.domain,
				EntityKey: test.entityKey,
				Reason:    test.reason,
				FactID:    "posture-first", SourceSystem: "aws",
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("intent = %#v, want %#v", got, want)
			}
		})
	}
}

// TestEC2PostureIntentBuildersRejectMissingTrigger proves the four
// posture-presence-triggered builders return ok=false for a generation with
// no ec2_instance_posture fact.
func TestEC2PostureIntentBuildersRejectMissingTrigger(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{{
		FactKind: facts.AWSResourceFactKind,
		FactID:   "resource-only",
	}})
	builders := []struct {
		name  string
		build func(string, string, projectorintent.FactLookup) (projectorintent.ReducerIntent, bool)
	}{
		{"instance node materialization", BuildInstanceNodeMaterializationReducerIntent},
		{"instance identity materialization", BuildInstanceIdentityMaterializationReducerIntent},
		{"internet exposure materialization", BuildInternetExposureMaterializationReducerIntent},
		{"block device kms posture materialization", BuildBlockDeviceKMSPostureMaterializationReducerIntent},
		{"uses profile materialization", BuildUsesProfileMaterializationReducerIntent},
	}
	for _, builder := range builders {
		t.Run(builder.name, func(t *testing.T) {
			t.Parallel()
			got, ok := builder.build("scope", "generation", lookup)
			if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
				t.Fatalf("builder returned (%#v, %t), want zero intent and false", got, ok)
			}
		})
	}
}

// TestBuildInstanceNodeMaterializationReducerIntentSourceFallsBackToTrimmedCollector
// proves the shared SourceSystem rule: a blank source-ref system falls back
// to the trimmed collector kind.
func TestBuildInstanceNodeMaterializationReducerIntentSourceFallsBackToTrimmedCollector(t *testing.T) {
	t.Parallel()

	lookup := projectorintent.NewFactLookup([]facts.Envelope{
		postureEnvelope("posture-1", "   ", " aws_cloud ", ""),
	})
	got, ok := BuildInstanceNodeMaterializationReducerIntent("scope", "generation", lookup)
	if !ok {
		t.Fatal("BuildInstanceNodeMaterializationReducerIntent() ok = false, want true")
	}
	if got.SourceSystem != "aws_cloud" {
		t.Fatalf("SourceSystem = %q, want %q", got.SourceSystem, "aws_cloud")
	}
}

// TestBuildUsesProfileMaterializationReducerIntent covers the one builder
// that decodes the posture payload: it must skip instances with no attached
// profile, anchor to the first profile-bearing fact, carry its own distinct
// entity key, and treat an undecodable posture fact as no match rather than
// an error.
func TestBuildUsesProfileMaterializationReducerIntent(t *testing.T) {
	t.Parallel()

	const (
		scopeID      = "aws:111122223333:us-east-1:ec2"
		generationID = "aws-generation-1"
	)

	t.Run("skips instance with no attached profile", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			postureEnvelope("posture-noprofile", "aws", "aws_cloud", ""),
		})
		got, ok := BuildUsesProfileMaterializationReducerIntent(scopeID, generationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("builder returned (%#v, %t), want zero intent and false", got, ok)
		}
	})

	t.Run("anchors to the profile-bearing fact and carries its own entity key", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			postureEnvelope("posture-noprofile", "aws", "aws_cloud", ""),
			postureEnvelope("posture-profile", "aws", "aws_cloud",
				"arn:aws:iam::111122223333:instance-profile/app"),
		})
		got, ok := BuildUsesProfileMaterializationReducerIntent(scopeID, generationID, lookup)
		if !ok {
			t.Fatal("BuildUsesProfileMaterializationReducerIntent() ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID: scopeID, GenerationID: generationID,
			Domain:    reducer.DomainEC2UsesProfileMaterialization,
			EntityKey: "ec2_uses_profile_materialization:" + scopeID,
			Reason:    "ec2 instance profile usage observed",
			FactID:    "posture-profile", SourceSystem: "aws",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("treats an undecodable posture fact as no match", func(t *testing.T) {
		t.Parallel()
		invalid := postureEnvelope("posture-invalid", "aws", "aws_cloud",
			"arn:aws:iam::111122223333:instance-profile/app")
		delete(invalid.Payload, "account_id")
		lookup := projectorintent.NewFactLookup([]facts.Envelope{invalid})
		got, ok := BuildUsesProfileMaterializationReducerIntent(scopeID, generationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("builder returned (%#v, %t), want zero intent and false", got, ok)
		}
	})
}

// TestDecodeEC2InstancePosture proves the local decode helper both
// round-trips a valid payload and surfaces a non-nil error for a missing
// required field, matching the contract the sole caller
// (BuildUsesProfileMaterializationReducerIntent) depends on.
func TestDecodeEC2InstancePosture(t *testing.T) {
	t.Parallel()

	t.Run("valid payload decodes the instance profile arn", func(t *testing.T) {
		t.Parallel()
		env := postureEnvelope("posture-1", "aws", "aws_cloud",
			"arn:aws:iam::111122223333:instance-profile/app")
		posture, err := decodeEC2InstancePosture(env)
		if err != nil {
			t.Fatalf("decodeEC2InstancePosture() error = %v, want nil", err)
		}
		if got, want := derefString(posture.InstanceProfileARN), "arn:aws:iam::111122223333:instance-profile/app"; got != want {
			t.Fatalf("InstanceProfileARN = %q, want %q", got, want)
		}
	})

	t.Run("missing required field errors", func(t *testing.T) {
		t.Parallel()
		env := postureEnvelope("posture-invalid", "aws", "aws_cloud", "")
		delete(env.Payload, "account_id")
		if _, err := decodeEC2InstancePosture(env); err == nil {
			t.Fatal("decodeEC2InstancePosture() error = nil, want non-nil")
		}
	})
}

func TestDerefString(t *testing.T) {
	t.Parallel()

	if got := derefString(nil); got != "" {
		t.Fatalf("derefString(nil) = %q, want empty", got)
	}
	value := "arn:aws:iam::111122223333:instance-profile/app"
	if got := derefString(&value); got != value {
		t.Fatalf("derefString(&value) = %q, want %q", got, value)
	}
}
