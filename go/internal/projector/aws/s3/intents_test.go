// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package s3

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	testScopeID      = "aws:111111111111:us-east-1:s3"
	testGenerationID = "aws-generation-1"
)

func postureEnvelope(factID, loggingTarget string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		FactKind:      facts.S3BucketPostureFactKind,
		SchemaVersion: facts.S3BucketPostureSchemaVersionV1,
		CollectorKind: "aws_cloud",
		ObservedAt:    time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "aws",
		},
		Payload: map[string]any{
			"account_id":            "111111111111",
			"region":                "us-east-1",
			"bucket_arn":            "arn:aws:s3:::orders",
			"bucket_name":           "orders",
			"logging_target_bucket": loggingTarget,
		},
	}
}

func grantEnvelope(factID, principalKind, principalValue, grantOutcome string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		FactKind:      facts.S3ExternalPrincipalGrantFactKind,
		SchemaVersion: facts.S3ExternalPrincipalGrantSchemaVersionV1,
		CollectorKind: "aws_cloud",
		ObservedAt:    time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "aws",
		},
		Payload: map[string]any{
			"account_id":       "111111111111",
			"region":           "us-east-1",
			"bucket_arn":       "arn:aws:s3:::orders-artifacts",
			"bucket_name":      "orders-artifacts",
			"principal_kind":   principalKind,
			"principal_value":  principalValue,
			"grant_outcome":    grantOutcome,
			"is_cross_account": grantOutcome == "cross_account",
			"is_public":        grantOutcome == "public",
		},
	}
}

// TestBuildInternetExposureMaterializationReducerIntent proves the builder
// enqueues from posture-fact presence alone, anchors to the first fact, and
// shares the aws_resource_materialization entity key.
func TestBuildInternetExposureMaterializationReducerIntent(t *testing.T) {
	t.Parallel()

	t.Run("queues from posture presence", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			postureEnvelope("fact-posture-1", ""),
		})
		got, ok := BuildInternetExposureMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID: testScopeID, GenerationID: testGenerationID,
			Domain:    reducer.DomainS3InternetExposureMaterialization,
			EntityKey: "aws_resource_materialization:" + testScopeID,
			Reason:    "s3 bucket posture observed",
			FactID:    "fact-posture-1", SourceSystem: "aws",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("does not queue without posture facts", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup(nil)
		got, ok := BuildInternetExposureMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t), want zero intent and false", got, ok)
		}
	})
}

// TestBuildExternalPrincipalGrantMaterializationReducerIntent proves the
// builder enqueues from grant-fact presence, anchors to the first fact, and
// shares the aws_resource_materialization entity key.
func TestBuildExternalPrincipalGrantMaterializationReducerIntent(t *testing.T) {
	t.Parallel()

	t.Run("queues from grant presence", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			grantEnvelope("fact-grant-1", "aws_account", "999988887777", "cross_account"),
		})
		got, ok := BuildExternalPrincipalGrantMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID: testScopeID, GenerationID: testGenerationID,
			Domain:    reducer.DomainS3ExternalPrincipalGrantMaterialization,
			EntityKey: "aws_resource_materialization:" + testScopeID,
			Reason:    "s3 external principal grant observed",
			FactID:    "fact-grant-1", SourceSystem: "aws",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("does not queue without grant facts", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			postureEnvelope("fact-posture", ""),
		})
		got, ok := BuildExternalPrincipalGrantMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t), want zero intent and false", got, ok)
		}
	})
}

// TestBuildLogsToMaterializationReducerIntent covers the one builder that
// decodes the posture payload: it must skip logging-disabled-only
// generations, anchor to the first logging-enabled fact, and treat an
// undecodable posture fact as no match rather than an error.
func TestBuildLogsToMaterializationReducerIntent(t *testing.T) {
	t.Parallel()

	t.Run("queues when a logging target is present", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			// A logging-disabled posture fact alone must not trigger the edge intent.
			postureEnvelope("fact-disabled", ""),
			postureEnvelope("fact-logging", "central-logs"),
		})
		got, ok := BuildLogsToMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID: testScopeID, GenerationID: testGenerationID,
			Domain:    reducer.DomainS3LogsToMaterialization,
			EntityKey: "aws_resource_materialization:" + testScopeID,
			Reason:    "s3 bucket access logging observed",
			FactID:    "fact-logging", SourceSystem: "aws",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("does not queue without a logging target", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			postureEnvelope("fact-disabled", ""),
		})
		got, ok := BuildLogsToMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t), want zero intent and false", got, ok)
		}
	})

	t.Run("treats an undecodable posture fact as no match", func(t *testing.T) {
		t.Parallel()
		invalid := postureEnvelope("fact-invalid-logging", "central-logs")
		delete(invalid.Payload, "account_id")
		lookup := projectorintent.NewFactLookup([]facts.Envelope{invalid})
		got, ok := BuildLogsToMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t), want zero intent and false", got, ok)
		}
	})
}

// TestDecodeS3BucketPosture proves the local decode helper both round-trips a
// valid payload and surfaces a non-nil error for a missing required field,
// matching the contract the sole caller
// (BuildLogsToMaterializationReducerIntent) depends on.
func TestDecodeS3BucketPosture(t *testing.T) {
	t.Parallel()

	t.Run("valid payload decodes the logging target bucket", func(t *testing.T) {
		t.Parallel()
		env := postureEnvelope("fact-posture-1", "central-logs")
		posture, err := decodeS3BucketPosture(env)
		if err != nil {
			t.Fatalf("decodeS3BucketPosture() error = %v, want nil", err)
		}
		if got, want := derefString(posture.LoggingTargetBucket), "central-logs"; got != want {
			t.Fatalf("LoggingTargetBucket = %q, want %q", got, want)
		}
	})

	t.Run("missing required field errors", func(t *testing.T) {
		t.Parallel()
		env := postureEnvelope("fact-invalid", "")
		delete(env.Payload, "account_id")
		if _, err := decodeS3BucketPosture(env); err == nil {
			t.Fatal("decodeS3BucketPosture() error = nil, want non-nil")
		}
	})
}

func TestDerefString(t *testing.T) {
	t.Parallel()

	if got := derefString(nil); got != "" {
		t.Fatalf("derefString(nil) = %q, want empty", got)
	}
	value := "central-logs"
	if got := derefString(&value); got != value {
		t.Fatalf("derefString(&value) = %q, want %q", got, value)
	}
}
