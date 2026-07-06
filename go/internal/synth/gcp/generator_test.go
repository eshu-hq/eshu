// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// TestGenerateIsByteIdenticalForSameSeed proves the seeded generator produces
// a byte-identical v1 cassette across repeated runs of the same seed — the
// acceptance criterion issue #4581 leads with. This is the first test written
// (TDD): it must fail before Generate exists / before determinism is wired,
// then pass once the generator canonicalizes its output the same way
// go/internal/replay/recorder does.
func TestGenerateIsByteIdenticalForSameSeed(t *testing.T) {
	optsA := Options{Seed: 42, ProjectID: "synth-project", ResourceCount: 12}
	optsB := Options{Seed: 42, ProjectID: "synth-project", ResourceCount: 12}

	outA, err := Generate(optsA)
	if err != nil {
		t.Fatalf("Generate(seed=42) run 1: %v", err)
	}
	outB, err := Generate(optsB)
	if err != nil {
		t.Fatalf("Generate(seed=42) run 2: %v", err)
	}

	if !bytes.Equal(outA, outB) {
		t.Fatalf("Generate is not deterministic for the same seed: run1 %d bytes, run2 %d bytes differ", len(outA), len(outB))
	}
}

// TestGenerateDifferentSeedsDiffer proves the seed actually drives the
// generated content — a generator that ignored its seed would trivially pass
// the determinism test above while carrying no real per-seed corpus variety.
func TestGenerateDifferentSeedsDiffer(t *testing.T) {
	outA, err := Generate(Options{Seed: 1, ProjectID: "synth-project", ResourceCount: 12})
	if err != nil {
		t.Fatalf("Generate(seed=1): %v", err)
	}
	outB, err := Generate(Options{Seed: 2, ProjectID: "synth-project", ResourceCount: 12})
	if err != nil {
		t.Fatalf("Generate(seed=2): %v", err)
	}
	if bytes.Equal(outA, outB) {
		t.Fatal("Generate(seed=1) and Generate(seed=2) produced identical bytes; the seed is not driving generation")
	}
}

// TestSameProjectDifferentSeedsHaveDisjointReplayIdentities proves two corpora
// generated with the SAME ProjectID but DIFFERENT seeds carry disjoint replay
// identities — distinct scope_id, generation_id, and derived fact_id sets — so
// replaying both into one store yields two independent corpora rather than the
// later run fencing/overwriting the earlier one.
//
// Regression for the codex finding on PR #4762 (generator.go: scope identity
// was derived only from ProjectID). Replay derives fact_id from
// (scope_id, generation_id, stable_fact_key) via facts.StableID
// (go/internal/replay/cassette/source.go), and the stable keys here are
// project/resource-based (seed-independent), so seed-independent scope and
// generation identity made two seed-variants collide on every fact_id. This
// test drives the REAL derivation (facts.StableID), not a re-implementation.
func TestSameProjectDifferentSeedsHaveDisjointReplayIdentities(t *testing.T) {
	const project = "shared-project"
	idsA := replayIdentities(t, Options{Seed: 100, ProjectID: project, ResourceCount: 15})
	idsB := replayIdentities(t, Options{Seed: 200, ProjectID: project, ResourceCount: 15})

	if idsA.scopeID == idsB.scopeID {
		t.Errorf("scope_id collides across seeds for the same project: %q", idsA.scopeID)
	}
	if idsA.generationID == idsB.generationID {
		t.Errorf("generation_id collides across seeds for the same project: %q", idsA.generationID)
	}
	if len(idsA.factIDs) == 0 || len(idsB.factIDs) == 0 {
		t.Fatalf("expected non-empty fact sets, got %d and %d", len(idsA.factIDs), len(idsB.factIDs))
	}
	for factID := range idsA.factIDs {
		if idsB.factIDs[factID] {
			t.Errorf("fact_id %q is shared across seeds for the same project; replaying both would collide/fence", factID)
		}
	}
}

// replayIdentitySet captures the replay-time identities a generated cassette
// contributes: its single scope's scope_id and generation_id, plus the set of
// fact_ids the real cassette replay path would derive for its facts.
type replayIdentitySet struct {
	scopeID      string
	generationID string
	factIDs      map[string]bool
}

// replayIdentities generates a cassette for opts and returns the replay
// identities its facts would carry, computing each fact_id with the SAME
// facts.StableID derivation the cassette replay source uses
// (go/internal/replay/cassette/source.go:buildEnvelope). It is not a
// re-implementation of that derivation — it calls the production function — so
// a change to how fact_id is derived is reflected here automatically.
func replayIdentities(t *testing.T, opts Options) replayIdentitySet {
	t.Helper()
	out, err := Generate(opts)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	file, err := cassette.ParseAndValidate(out)
	if err != nil {
		t.Fatalf("ParseAndValidate: %v", err)
	}
	if len(file.Scopes) != 1 {
		t.Fatalf("expected exactly one scope, got %d", len(file.Scopes))
	}
	scope := file.Scopes[0]
	ids := replayIdentitySet{
		scopeID:      scope.ScopeID,
		generationID: scope.GenerationID,
		factIDs:      make(map[string]bool, len(scope.Facts)),
	}
	for _, fact := range scope.Facts {
		factID := facts.StableID("CassetteReplay", map[string]any{
			"scope_id":        scope.ScopeID,
			"generation_id":   scope.GenerationID,
			"stable_fact_key": fact.StableFactKey,
		})
		ids.factIDs[factID] = true
	}
	return ids
}

// TestGenerateProducesValidV1Cassette proves the generated bytes parse and
// validate as a schema_version "1" cassette through the real fail-closed
// codec (go/internal/replay/cassette), not merely as arbitrary JSON.
func TestGenerateProducesValidV1Cassette(t *testing.T) {
	out, err := Generate(Options{Seed: 7, ProjectID: "synth-project", ResourceCount: 20})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	file, err := cassette.ParseAndValidate(out)
	if err != nil {
		t.Fatalf("generated cassette failed fail-closed validation: %v", err)
	}
	if file.SchemaVersion != cassette.SchemaVersionV1 {
		t.Fatalf("schema_version = %q, want %q", file.SchemaVersion, cassette.SchemaVersionV1)
	}
	if len(file.Scopes) == 0 {
		t.Fatal("generated cassette has zero scopes")
	}
	if len(file.Scopes[0].Facts) == 0 {
		t.Fatal("generated cassette's scope has zero facts")
	}
}

// TestGeneratePayloadsDecodeThroughContractsSeam proves every generated fact
// decodes cleanly through the real sdk/go/factschema decode seam for its
// kind — the schema-validity acceptance criterion, exercised through the
// actual production decode path rather than a re-implementation of it.
func TestGeneratePayloadsDecodeThroughContractsSeam(t *testing.T) {
	out, err := Generate(Options{Seed: 99, ProjectID: "synth-project", ResourceCount: 30})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	file, err := cassette.ParseAndValidate(out)
	if err != nil {
		t.Fatalf("ParseAndValidate: %v", err)
	}

	seenKinds := map[string]int{}
	for _, scope := range file.Scopes {
		for _, fact := range scope.Facts {
			seenKinds[fact.FactKind]++
			env := factschema.Envelope{
				FactKind:      fact.FactKind,
				SchemaVersion: fact.SchemaVersion,
				Payload:       fact.Payload,
			}
			if err := decodeByKind(env); err != nil {
				t.Fatalf("fact kind %q failed contracts decode: %v", fact.FactKind, err)
			}
		}
	}
	if len(seenKinds) == 0 {
		t.Fatal("no facts were generated to decode")
	}
}

// decodeByKind dispatches env to the matching sdk/go/factschema Decode
// function, mirroring how a reducer handler would decode the same kind. It
// is test-only dispatch glue, not a production decode path.
func decodeByKind(env factschema.Envelope) error {
	switch env.FactKind {
	case factschema.FactKindGCPCloudResource:
		_, err := factschema.DecodeGCPCloudResource(env)
		return err
	case factschema.FactKindGCPCloudRelationship:
		_, err := factschema.DecodeGCPCloudRelationship(env)
		return err
	case factschema.FactKindGCPCollectionWarning:
		_, err := factschema.DecodeGCPCollectionWarning(env)
		return err
	case factschema.FactKindGCPDNSRecord:
		_, err := factschema.DecodeGCPDNSRecord(env)
		return err
	case factschema.FactKindGCPIAMPolicyObservation:
		_, err := factschema.DecodeGCPIAMPolicyObservation(env)
		return err
	default:
		return errUnknownFactKindForTest(env.FactKind)
	}
}

type errUnknownFactKindForTest string

func (e errUnknownFactKindForTest) Error() string {
	return "test: generated an undecoded fact kind " + string(e)
}

// TestGenerateFailsClosedOnUnknownKind proves the generator refuses to emit a
// fact kind that has no registered schema, per the fail-closed acceptance
// criterion, rather than silently emitting an unvalidated payload.
func TestGenerateFailsClosedOnUnknownKind(t *testing.T) {
	_, err := generateFact("gcp_no_such_kind", "1.0.0", map[string]any{"x": "y"})
	if err == nil {
		t.Fatal("generateFact for an unregistered kind returned no error; want fail-closed rejection")
	}
}

// TestGenerateCanonicalizesIdempotently proves re-canonicalizing the
// generated output is a no-op, matching replay.Canonicalize's documented
// idempotence contract and the recorder precedent's load-back guard.
func TestGenerateCanonicalizesIdempotently(t *testing.T) {
	out, err := Generate(Options{Seed: 5, ProjectID: "synth-project", ResourceCount: 10})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var decoded any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("re-decode generated cassette: %v", err)
	}
	recanonicalized, err := canonicalizeValue(decoded)
	if err != nil {
		t.Fatalf("re-canonicalize: %v", err)
	}
	if !bytes.Equal(out, recanonicalized) {
		t.Fatal("Generate output is not idempotent under re-canonicalization")
	}
}
