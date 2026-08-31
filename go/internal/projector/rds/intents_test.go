// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rds

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	testScopeID      = "aws:111111111111:us-east-1:rds"
	testGenerationID = "aws-generation-1"
)

func postureEnvelope(factID string, publiclyAccessible bool) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		FactKind:      facts.RDSInstancePostureFactKind,
		SchemaVersion: facts.RDSPostureSchemaVersionV1,
		CollectorKind: "aws_cloud",
		ObservedAt:    time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "aws",
		},
		Payload: map[string]any{
			"resource_id":         "orders-db",
			"resource_type":       "aws_rds_db_instance",
			"publicly_accessible": publiclyAccessible,
		},
	}
}

// TestBuildRDSPostureMaterializationReducerIntent proves the builder enqueues
// from rds_instance_posture presence alone (public and private instances
// both trigger), anchors to the first posture fact, and shares the
// aws_resource_materialization entity key so the reducer's readiness gate
// resolves the same CloudResource canonical-nodes phase.
func TestBuildRDSPostureMaterializationReducerIntent(t *testing.T) {
	t.Parallel()

	t.Run("queues from private posture presence", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			postureEnvelope("fact-rds-posture-1", false),
		})
		got, ok := BuildRDSPostureMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := projectorintent.ReducerIntent{
			ScopeID: testScopeID, GenerationID: testGenerationID,
			Domain:    reducer.DomainRDSPostureMaterialization,
			EntityKey: "aws_resource_materialization:" + testScopeID,
			Reason:    "rds posture facts observed",
			FactID:    "fact-rds-posture-1", SourceSystem: "aws",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intent = %#v, want %#v", got, want)
		}
	})

	t.Run("queues from public posture presence", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			postureEnvelope("fact-rds-posture-public", true),
		})
		got, ok := BuildRDSPostureMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.FactID != "fact-rds-posture-public" {
			t.Fatalf("FactID = %q, want %q", got.FactID, "fact-rds-posture-public")
		}
	})

	t.Run("anchors to the first posture fact", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup([]facts.Envelope{
			postureEnvelope("fact-first", false),
			postureEnvelope("fact-second", true),
		})
		got, ok := BuildRDSPostureMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.FactID != "fact-first" {
			t.Fatalf("FactID = %q, want %q", got.FactID, "fact-first")
		}
	})

	t.Run("does not queue without posture facts", func(t *testing.T) {
		t.Parallel()
		lookup := projectorintent.NewFactLookup(nil)
		got, ok := BuildRDSPostureMaterializationReducerIntent(testScopeID, testGenerationID, lookup)
		if ok || !reflect.DeepEqual(got, projectorintent.ReducerIntent{}) {
			t.Fatalf("returned (%#v, %t), want zero intent and false", got, ok)
		}
	})
}
