// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestOCIRegistryCassetteDecodesCleanlyThroughSeam is the durable guard against
// the stale-cassette regression the Wave 4b typed-payload migration surfaced:
// the B-7 golden-corpus cassette (testdata/cassettes/ociregistry/
// supply-chain-demo.json) must carry the shape the CURRENT collector emitters
// produce (go/internal/collector/ociregistry/*.NewXEnvelope), because the
// projector now decodes each oci fact through the typed factschema seam. A
// cassette payload whose optional field carries a shape the typed struct cannot
// decode (the historical bug: manifest "layers": 12 as an int, where the
// collector emits a descriptor array) dead-letters the whole fact through the
// projector's per-fact quarantine, producing ZERO manifest nodes and a red
// golden-corpus gate — with no unit-test signal, because the gate runs Docker.
//
// This test closes that gap without Docker: it reads the real checked-in
// cassette, feeds each oci fact through the SAME extractOCIRegistryRows path the
// projector runs, and asserts that (a) no valid cassette fact is quarantined as
// input_invalid, and (b) every consumed oci kind in the cassette materializes
// its canonical row. If a future cassette edit drifts from the collector's emit
// shape into a shape the typed decode rejects, this fails immediately at
// `go test ./internal/projector`, long before the Docker gate.
func TestOCIRegistryCassetteDecodesCleanlyThroughSeam(t *testing.T) {
	t.Parallel()

	envelopes := loadOCICassetteEnvelopes(t)
	if len(envelopes) == 0 {
		t.Fatal("cassette carried no oci_registry facts; the golden-corpus gate would project nothing")
	}

	mat := &CanonicalMaterialization{}
	quarantined := extractOCIRegistryRows(mat, envelopes)

	// Every valid cassette fact must decode cleanly — a quarantine here means the
	// cassette drifted from the collector's emit shape into an input_invalid
	// shape (the stale-cassette regression this test exists to catch).
	if len(quarantined) != 0 {
		for _, q := range quarantined {
			t.Errorf("cassette fact %s (%s) quarantined as input_invalid on field %q; the cassette payload has drifted from the current collector emitter's shape — reconcile testdata/cassettes/ociregistry/supply-chain-demo.json to go/internal/collector/ociregistry/*.NewXEnvelope", q.factID, q.factKind, q.field)
		}
		t.FailNow()
	}

	// The cassette carries a repository + an image_manifest fact; both must
	// materialize their canonical rows so the golden-corpus gate's
	// OciImageManifest node-count and RUNS_IMAGE edge assertions can pass.
	if mat.OCIRegistryRepository == nil {
		t.Error("cassette repository fact did not materialize an OCIRegistryRepository row")
	}
	if len(mat.OCIImageManifests) == 0 {
		t.Error("cassette image_manifest fact did not materialize an OCIImageManifest row; the golden-corpus gate's node_count_OciImageManifest and RUNS_IMAGE checks would fail")
	}
}

// loadOCICassetteEnvelopes reads the real checked-in oci_registry cassette and
// converts each recorded fact into a facts.Envelope carrying the fact kind,
// schema version, and payload the projector's extractOCIRegistryRows consumes.
// It intentionally reads the same file the golden-corpus gate replays, so a
// drift in that file is caught here.
func loadOCICassetteEnvelopes(t *testing.T) []facts.Envelope {
	t.Helper()

	// This file lives at <repoRoot>/go/internal/projector/; the cassette lives at
	// <repoRoot>/testdata/cassettes/ociregistry/supply-chain-demo.json.
	wd, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve absolute path: %v", err)
	}
	cassettePath := filepath.Join(wd, "..", "..", "..", "testdata", "cassettes", "ociregistry", "supply-chain-demo.json")

	raw, err := os.ReadFile(cassettePath)
	if err != nil {
		t.Fatalf("read oci cassette %s: %v", cassettePath, err)
	}

	var cassette struct {
		Scopes []struct {
			GenerationID string `json:"generation_id"`
			ScopeID      string `json:"scope_id"`
			Facts        []struct {
				FactKind      string         `json:"fact_kind"`
				SchemaVersion string         `json:"schema_version"`
				StableFactKey string         `json:"stable_fact_key"`
				Payload       map[string]any `json:"payload"`
			} `json:"facts"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &cassette); err != nil {
		t.Fatalf("unmarshal oci cassette: %v", err)
	}

	var envelopes []facts.Envelope
	for _, scope := range cassette.Scopes {
		for i, fact := range scope.Facts {
			envelopes = append(envelopes, facts.Envelope{
				FactID:        ociCassetteFactID(fact.FactKind, i),
				ScopeID:       scope.ScopeID,
				GenerationID:  scope.GenerationID,
				FactKind:      fact.FactKind,
				SchemaVersion: fact.SchemaVersion,
				StableFactKey: fact.StableFactKey,
				Payload:       fact.Payload,
			})
		}
	}
	return envelopes
}

// ociCassetteFactID synthesizes a stable, unique fact id for a cassette fact so
// a quarantine message can name the offending fact deterministically.
func ociCassetteFactID(factKind string, index int) string {
	return "cassette:" + factKind + ":" + string(rune('0'+index))
}
