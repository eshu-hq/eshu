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

// TestTerraformStateCassetteDecodesCleanlyThroughSeam is the durable guard
// against the stale-cassette regression the Wave 4b typed-payload migration
// surfaced (the same class of bug the k8s and oci_registry migrations hit
// first): the B-7 golden-corpus cassette (testdata/cassettes/terraformstate/
// supply-chain-demo.json) must carry the shape the CURRENT collector emitter
// produces (go/internal/collector/terraformstate/*.go), because the projector
// now decodes each terraform_state fact through the typed factschema seam. A
// cassette payload whose required field is absent, or whose fact_kind string
// does not match the wire constant, dead-letters the whole fact through the
// projector's per-fact quarantine (or never matches the extractor's switch at
// all), producing ZERO resource/module/output nodes and a red golden-corpus
// gate — with no unit-test signal, because the gate runs Docker.
//
// This test closes that gap without Docker: it reads the real checked-in
// cassette, feeds each terraform_state fact through the SAME
// extractTerraformStateRows path the projector runs, and asserts that (a) no
// valid cassette fact is quarantined as input_invalid, and (b) every consumed
// terraform_state kind in the cassette materializes its canonical row. If a
// future cassette edit drifts from the collector's emit shape into a shape the
// typed decode rejects, this fails immediately at `go test ./internal/projector`,
// long before the Docker gate.
func TestTerraformStateCassetteDecodesCleanlyThroughSeam(t *testing.T) {
	t.Parallel()

	envelopes := loadTerraformStateCassetteEnvelopes(t)
	if len(envelopes) == 0 {
		t.Fatal("cassette carried no terraform_state facts; the golden-corpus gate would project nothing")
	}

	mat := &CanonicalMaterialization{ScopeID: "cassette-tfstate-scd"}
	quarantined := extractTerraformStateRows(mat, envelopes)

	// Every valid cassette fact must decode cleanly — a quarantine here means
	// the cassette drifted from the collector's emit shape into an
	// input_invalid shape (the stale-cassette regression this test exists to
	// catch).
	if len(quarantined) != 0 {
		for _, q := range quarantined {
			t.Errorf("cassette fact %s (%s) quarantined as input_invalid on field %q; the cassette payload has drifted from the current collector emitter's shape — reconcile testdata/cassettes/terraformstate/supply-chain-demo.json to go/internal/collector/terraformstate/*.go", q.factID, q.factKind, q.field)
		}
		t.FailNow()
	}

	// The cassette carries a snapshot + two ECS resources + modules + a tag
	// observation + an output; every consumed kind must materialize so the
	// golden-corpus gate's TerraformResource node-count check ("at least one
	// ECS resource expected") can pass.
	if len(mat.TerraformStateResources) == 0 {
		t.Error("cassette resource facts did not materialize any TerraformStateResource row; the golden-corpus gate's TerraformResource node-count check would fail")
	}
	if len(mat.TerraformStateModules) == 0 {
		t.Error("cassette module facts did not materialize any TerraformStateModule row")
	}
	if len(mat.TerraformStateOutputs) == 0 {
		t.Error("cassette output fact did not materialize a TerraformStateOutput row")
	}
	var sawTagJoin bool
	for _, resource := range mat.TerraformStateResources {
		if len(resource.TagKeyHashes) > 0 {
			sawTagJoin = true
		}
	}
	if !sawTagJoin {
		t.Error("cassette tag_observation fact did not join to any resource's TagKeyHashes")
	}
}

// loadTerraformStateCassetteEnvelopes reads the real checked-in terraform_state
// cassette and converts each recorded fact into a facts.Envelope carrying the
// fact kind, schema version, and payload the projector's
// extractTerraformStateRows consumes. It intentionally reads the same file the
// golden-corpus gate replays, so a drift in that file is caught here.
func loadTerraformStateCassetteEnvelopes(t *testing.T) []facts.Envelope {
	t.Helper()

	// This file lives at <repoRoot>/go/internal/projector/; the cassette lives
	// at <repoRoot>/testdata/cassettes/terraformstate/supply-chain-demo.json.
	wd, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve absolute path: %v", err)
	}
	cassettePath := filepath.Join(wd, "..", "..", "..", "testdata", "cassettes", "terraformstate", "supply-chain-demo.json")

	raw, err := os.ReadFile(cassettePath)
	if err != nil {
		t.Fatalf("read terraform_state cassette %s: %v", cassettePath, err)
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
		t.Fatalf("unmarshal terraform_state cassette: %v", err)
	}

	var envelopes []facts.Envelope
	for _, scope := range cassette.Scopes {
		for i, fact := range scope.Facts {
			envelopes = append(envelopes, facts.Envelope{
				FactID:        terraformStateCassetteFactID(fact.FactKind, i),
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

// terraformStateCassetteFactID synthesizes a stable, unique fact id for a
// cassette fact so a quarantine message can name the offending fact
// deterministically.
func terraformStateCassetteFactID(factKind string, index int) string {
	return "cassette:" + factKind + ":" + string(rune('0'+index))
}
