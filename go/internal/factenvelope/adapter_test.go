// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factenvelope

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

func TestInternalFromSDKFactMapsCanonicalFieldsAndClonesPayload(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.July, 8, 12, 30, 0, 0, time.UTC)
	payload := map[string]any{
		"resource_id": "res-1",
		"nested":      map[string]any{"name": "before"},
	}
	fact := sdkcollector.Fact{
		Kind:             "aws_resource",
		SchemaVersion:    "1.0.0",
		StableKey:        "aws:resource:res-1",
		SourceConfidence: sdkcollector.SourceConfidenceObserved,
		ObservedAt:       observedAt,
		Tombstone:        true,
		SourceRef: sdkcollector.SourceRef{
			SourceSystem: "aws",
			ScopeID:      "scope-from-ref",
			GenerationID: "generation-from-ref",
			FactKey:      "aws:resource:res-1",
			URI:          "aws://resource/res-1",
			RecordID:     "res-1",
		},
		Payload: payload,
	}
	opts := InternalEnvelopeOptions{
		ComponentID:   "component-1",
		ScopeID:       "scope-1",
		GenerationID:  "generation-1",
		CollectorKind: "extension",
		FencingToken:  42,
	}

	got := InternalFromSDKFact(fact, opts)

	if got.FactID == "" {
		t.Fatal("InternalFromSDKFact() left FactID blank")
	}
	if got.ScopeID != opts.ScopeID || got.GenerationID != opts.GenerationID {
		t.Fatalf("host-owned scope/generation = %q/%q, want %q/%q", got.ScopeID, got.GenerationID, opts.ScopeID, opts.GenerationID)
	}
	if got.FactKind != fact.Kind || got.StableFactKey != fact.StableKey {
		t.Fatalf("fact identity = %q/%q, want %q/%q", got.FactKind, got.StableFactKey, fact.Kind, fact.StableKey)
	}
	if got.SchemaVersion != fact.SchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", got.SchemaVersion, fact.SchemaVersion)
	}
	if got.CollectorKind != opts.CollectorKind || got.FencingToken != opts.FencingToken {
		t.Fatalf("host fields = %q/%d, want %q/%d", got.CollectorKind, got.FencingToken, opts.CollectorKind, opts.FencingToken)
	}
	if got.SourceConfidence != string(fact.SourceConfidence) || !got.ObservedAt.Equal(observedAt) || !got.IsTombstone {
		t.Fatalf("observation fields = confidence %q observed %s tombstone %v", got.SourceConfidence, got.ObservedAt, got.IsTombstone)
	}
	wantRef := facts.Ref{
		SourceSystem:   fact.SourceRef.SourceSystem,
		ScopeID:        fact.SourceRef.ScopeID,
		GenerationID:   fact.SourceRef.GenerationID,
		FactKey:        fact.SourceRef.FactKey,
		SourceURI:      fact.SourceRef.URI,
		SourceRecordID: fact.SourceRef.RecordID,
	}
	if got.SourceRef != wantRef {
		t.Fatalf("SourceRef = %#v, want %#v", got.SourceRef, wantRef)
	}
	if !reflect.DeepEqual(got.Payload, fact.Payload) {
		t.Fatalf("Payload = %#v, want %#v", got.Payload, fact.Payload)
	}

	payload["resource_id"] = "mutated"
	payload["nested"].(map[string]any)["name"] = "after"
	if got.Payload["resource_id"] != "res-1" || got.Payload["nested"].(map[string]any)["name"] != "before" {
		t.Fatalf("InternalFromSDKFact() did not clone payload before downstream handoff: %#v", got.Payload)
	}
}

func TestFactSchemaFromInternalNormalizesOnlyVersionlessSchemas(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		version string
		want    string
	}{
		{name: "empty", version: "", want: DefaultSchemaMajorVersion},
		{name: "persisted sentinel", version: PersistedVersionlessSchemaVersion, want: DefaultSchemaMajorVersion},
		{name: "supported", version: "1.2.3", want: "1.2.3"},
		{name: "unsupported major", version: "2.0.0", want: "2.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := facts.Envelope{
				FactKind:      "aws_resource",
				SchemaVersion: tc.version,
				StableFactKey: "aws:resource:res-1",
				Payload:       map[string]any{"resource_id": "res-1"},
			}

			got := FactSchemaFromInternal(env)
			if got.FactKind != env.FactKind || got.StableFactKey != env.StableFactKey {
				t.Fatalf("identity = %q/%q, want %q/%q", got.FactKind, got.StableFactKey, env.FactKind, env.StableFactKey)
			}
			if got.SchemaVersion != tc.want {
				t.Fatalf("SchemaVersion = %q, want %q", got.SchemaVersion, tc.want)
			}
			if !reflect.DeepEqual(got.Payload, env.Payload) {
				t.Fatalf("Payload = %#v, want %#v", got.Payload, env.Payload)
			}
		})
	}
}
