// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// testCassette returns a small two-fact gcp_cloud_resource cassette used by
// every test in this file. Both facts carry every field
// factschema.DecodeGCPCloudResource requires (full_resource_name, asset_type)
// so a mutation is the only thing that can make decode fail.
func testCassette(t *testing.T) cassette.File {
	t.Helper()
	return cassette.File{
		Collector:     "gcp",
		SchemaVersion: cassette.SchemaVersionV1,
		Scopes: []cassette.Scope{
			{
				ScopeID:       "gcp:project:demo",
				SourceSystem:  "gcp",
				ScopeKind:     "project",
				CollectorKind: "gcp",
				GenerationID:  "gen-1",
				ObservedAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				Facts: []cassette.Fact{
					{
						FactKind:      "gcp_cloud_resource",
						StableFactKey: "gcp:project:demo:b-resource",
						SchemaVersion: "1.0.0",
						Payload: map[string]any{
							"full_resource_name": "//example.com/b",
							"asset_type":         "example.googleapis.com/B",
						},
					},
					{
						FactKind:      "gcp_cloud_resource",
						StableFactKey: "gcp:project:demo:a-resource",
						SchemaVersion: "1.0.0",
						Payload: map[string]any{
							"full_resource_name": "//example.com/a",
							"asset_type":         "example.googleapis.com/A",
						},
					},
				},
			},
		},
	}
}

func TestMutateCassetteSelectsDeterministicallyByStableFactKey(t *testing.T) {
	src := testCassette(t)

	_, mutated1, err := MutateCassette(src, MutateOptions{
		FactKind: "gcp_cloud_resource",
		Kind:     MutationMissingField,
		Field:    "asset_type",
		Count:    1,
	})
	if err != nil {
		t.Fatalf("MutateCassette() error = %v", err)
	}
	_, mutated2, err := MutateCassette(src, MutateOptions{
		FactKind: "gcp_cloud_resource",
		Kind:     MutationMissingField,
		Field:    "asset_type",
		Count:    1,
	})
	if err != nil {
		t.Fatalf("MutateCassette() second call error = %v", err)
	}

	if !reflect.DeepEqual(mutated1, mutated2) {
		t.Fatalf("MutateCassette() is not deterministic across repeated calls: %+v vs %+v", mutated1, mutated2)
	}
	if len(mutated1) != 1 {
		t.Fatalf("len(mutated1) = %d, want 1", len(mutated1))
	}
	// "a-resource" sorts before "b-resource" by StableFactKey even though it
	// is the SECOND fact in the source slice; a selection keyed on raw slice
	// order would pick "b-resource" instead.
	if got, want := mutated1[0].StableFactKey, "gcp:project:demo:a-resource"; got != want {
		t.Fatalf("selected StableFactKey = %q, want %q (selection must sort by StableFactKey, not slice order)", got, want)
	}
}

func TestMutateCassetteNeverMutatesSource(t *testing.T) {
	src := testCassette(t)
	originalPayloadLen := len(src.Scopes[0].Facts[1].Payload)
	originalSchemaVersion := src.Scopes[0].Facts[1].SchemaVersion

	if _, _, err := MutateCassette(src, MutateOptions{
		FactKind: "gcp_cloud_resource",
		Kind:     MutationMissingField,
		Field:    "asset_type",
		Count:    1,
	}); err != nil {
		t.Fatalf("MutateCassette() error = %v", err)
	}

	if len(src.Scopes[0].Facts[1].Payload) != originalPayloadLen {
		t.Fatalf("MutateCassette() mutated the source cassette's payload map in place")
	}
	if _, ok := src.Scopes[0].Facts[1].Payload["asset_type"]; !ok {
		t.Fatalf("MutateCassette() deleted asset_type from the source cassette, not just the copy")
	}

	if _, _, err := MutateCassette(src, MutateOptions{
		FactKind:    "gcp_cloud_resource",
		Kind:        MutationSchemaMajor,
		SchemaMajor: "99.0.0",
		Count:       1,
	}); err != nil {
		t.Fatalf("MutateCassette() error = %v", err)
	}
	if src.Scopes[0].Facts[1].SchemaVersion != originalSchemaVersion {
		t.Fatalf("MutateCassette() mutated the source cassette's schema_version in place")
	}
}

func TestMutateCassetteMissingFieldQuarantinesNotDeadLetters(t *testing.T) {
	src := testCassette(t)

	dup, mutated, err := MutateCassette(src, MutateOptions{
		FactKind: "gcp_cloud_resource",
		Kind:     MutationMissingField,
		Field:    "asset_type",
		Count:    1,
	})
	if err != nil {
		t.Fatalf("MutateCassette() error = %v", err)
	}
	if len(mutated) != 1 {
		t.Fatalf("len(mutated) = %d, want 1", len(mutated))
	}

	fact := findFact(t, dup, mutated[0].StableFactKey)
	_, decodeErr := factschema.DecodeGCPCloudResource(factschema.Envelope{
		FactKind:      fact.FactKind,
		SchemaVersion: fact.SchemaVersion,
		StableFactKey: fact.StableFactKey,
		Payload:       fact.Payload,
	})
	if decodeErr == nil {
		t.Fatalf("DecodeGCPCloudResource() on a missing-field-mutated fact succeeded, want a decode error")
	}
	var classified *factschema.DecodeError
	if !errors.As(decodeErr, &classified) {
		t.Fatalf("DecodeGCPCloudResource() error = %v, want a *factschema.DecodeError", decodeErr)
	}
	if classified.Classification != factschema.ClassificationInputInvalid {
		t.Fatalf("Classification = %q, want %q", classified.Classification, factschema.ClassificationInputInvalid)
	}
	if classified.Field != "asset_type" {
		t.Fatalf("Field = %q, want %q", classified.Field, "asset_type")
	}
	// The reducer's partitionDecodeFailures (go/internal/reducer/factschema_decode.go)
	// routes every ClassificationInputInvalid error EXCEPT ErrUnsupportedSchemaMajor
	// to a per-fact QUARANTINE, not a durable fact_work_items dead-letter. A
	// missing-field mutation must not wrap that sentinel, or this fact would
	// escalate to the fatal/dead-letter path instead of the quarantine path
	// this mutation kind exists to exercise.
	if errors.Is(decodeErr, factschema.ErrUnsupportedSchemaMajor) {
		t.Fatalf("missing-field mutation produced an ErrUnsupportedSchemaMajor error; it must quarantine, not dead-letter")
	}
}

// TestMutateCassetteSchemaMajorReachesDeadLetterPath proves the CONTRACTS-SEAM
// half of MutationSchemaMajor's guarantee in hermetic isolation: decoding the
// mutated fact directly through factschema.DecodeGCPCloudResource returns
// ErrUnsupportedSchemaMajor, which go/internal/reducer's partitionDecodeFailures
// explicitly excludes from per-fact quarantine (factschema_decode.go:139-141)
// were a fact to reach that seam. It does NOT assert this is the exact
// runtime path exercised end to end: driving this mutation through a real
// stack (scripts/verify-ifa-dead-letter-determinism.sh) showed the projector's
// OWN, earlier admission-time schema-version gate
// (go/internal/projector/schema_version_admission.go) rejects a
// core-registered fact kind's unsupported major BEFORE the reducer's
// typed-decode seam is ever reached, dead-lettering the whole projector work
// item with failure_class="projection_bug" rather than the reducer's
// "input_invalid". Both paths are FATAL (never quarantined), which is the
// property this mutation kind needs; see MutationKind's doc comment for the
// full empirical picture.
func TestMutateCassetteSchemaMajorReachesDeadLetterPath(t *testing.T) {
	src := testCassette(t)

	dup, mutated, err := MutateCassette(src, MutateOptions{
		FactKind:    "gcp_cloud_resource",
		Kind:        MutationSchemaMajor,
		SchemaMajor: "99.0.0",
		Count:       1,
	})
	if err != nil {
		t.Fatalf("MutateCassette() error = %v", err)
	}
	if len(mutated) != 1 {
		t.Fatalf("len(mutated) = %d, want 1", len(mutated))
	}

	fact := findFact(t, dup, mutated[0].StableFactKey)
	if fact.SchemaVersion != "99.0.0" {
		t.Fatalf("mutated fact SchemaVersion = %q, want %q", fact.SchemaVersion, "99.0.0")
	}

	_, decodeErr := factschema.DecodeGCPCloudResource(factschema.Envelope{
		FactKind:      fact.FactKind,
		SchemaVersion: fact.SchemaVersion,
		StableFactKey: fact.StableFactKey,
		Payload:       fact.Payload,
	})
	if decodeErr == nil {
		t.Fatalf("DecodeGCPCloudResource() on a schema-major-mutated fact succeeded, want a decode error")
	}
	// This is the FATAL branch partitionDecodeFailures excludes from
	// quarantine (factschema_decode.go:139-141) — the contracts-seam half of
	// the guarantee; see the test's own doc comment for the empirically
	// observed end-to-end path and its actual failure_class.
	if !errors.Is(decodeErr, factschema.ErrUnsupportedSchemaMajor) {
		t.Fatalf("DecodeGCPCloudResource() error = %v, want errors.Is ErrUnsupportedSchemaMajor", decodeErr)
	}
	var classified *factschema.DecodeError
	if !errors.As(decodeErr, &classified) {
		t.Fatalf("DecodeGCPCloudResource() error = %v, want a *factschema.DecodeError", decodeErr)
	}
	if classified.Classification != factschema.ClassificationInputInvalid {
		t.Fatalf("Classification = %q, want %q", classified.Classification, factschema.ClassificationInputInvalid)
	}
}

func TestMutateCassetteRejectsUnknownKindAndMissingRequiredOption(t *testing.T) {
	src := testCassette(t)

	if _, _, err := MutateCassette(src, MutateOptions{FactKind: "gcp_cloud_resource", Kind: "bogus"}); err == nil {
		t.Fatalf("MutateCassette() with an unknown mutation kind succeeded, want an error")
	}
	if _, _, err := MutateCassette(src, MutateOptions{FactKind: "gcp_cloud_resource", Kind: MutationMissingField}); err == nil {
		t.Fatalf("MutateCassette() with MutationMissingField and no Field succeeded, want an error")
	}
	if _, _, err := MutateCassette(src, MutateOptions{FactKind: "gcp_cloud_resource", Kind: MutationSchemaMajor}); err == nil {
		t.Fatalf("MutateCassette() with MutationSchemaMajor and no SchemaMajor succeeded, want an error")
	}
	if _, _, err := MutateCassette(src, MutateOptions{Kind: MutationMissingField, Field: "asset_type"}); err == nil {
		t.Fatalf("MutateCassette() with no FactKind succeeded, want an error")
	}
}

func TestMutateCassetteRejectsCountExceedingAvailableFacts(t *testing.T) {
	src := testCassette(t)
	if _, _, err := MutateCassette(src, MutateOptions{
		FactKind: "gcp_cloud_resource",
		Kind:     MutationMissingField,
		Field:    "asset_type",
		Count:    3,
	}); err == nil {
		t.Fatalf("MutateCassette() with Count exceeding available facts succeeded, want an error")
	}
}

func TestMutateCassetteRejectsFieldAbsentFromPayload(t *testing.T) {
	src := testCassette(t)
	if _, _, err := MutateCassette(src, MutateOptions{
		FactKind: "gcp_cloud_resource",
		Kind:     MutationMissingField,
		Field:    "does_not_exist",
		Count:    1,
	}); err == nil {
		t.Fatalf("MutateCassette() with a field absent from every candidate payload succeeded, want an error")
	}
}

// findFact locates the fact with the given stable key in f, failing the test
// if it is not found.
func findFact(t *testing.T, f cassette.File, stableFactKey string) cassette.Fact {
	t.Helper()
	for _, s := range f.Scopes {
		for _, fact := range s.Facts {
			if fact.StableFactKey == stableFactKey {
				return fact
			}
		}
	}
	t.Fatalf("fact %q not found in cassette", stableFactKey)
	return cassette.Fact{}
}
