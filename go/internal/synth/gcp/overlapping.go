// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/replay"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// OverlappingScopeOptions configures one seeded synthetic-corpus generation run
// that produces MULTIPLE scopes which deliberately SHARE resource identity — the
// exact inverse of GenerateMultiScope's disjoint-by-construction scopes. It is
// the #5007 "contention Odù" fixture generator
// (docs/internal/design/5007-cross-scope-node-ownership.md): every generated
// scope carries the same ProjectID, so every scope's resources fold to the SAME
// CloudResource node uid, and the K scopes' reducer intents all contend on one
// shared node — the cross-scope same-uid contention the owner ledger resolves.
type OverlappingScopeOptions struct {
	// Seed is the deterministic PRNG seed for the single underlying resource
	// set. The same (Seed, Scopes, ResourceCount, Divergent) always produces
	// byte-identical output.
	Seed uint64
	// Scopes is the number K of contending scopes to generate over the one
	// shared resource-identity set. Must be >= 2 (a single scope cannot
	// contend with itself).
	Scopes int
	// ResourceCount is the number of gcp_cloud_resource facts per scope.
	// Must be positive.
	ResourceCount int
	// Divergent, when true, mutates each scope's OBSERVED STATE (the resource
	// state field) so the K contending scopes carry the same uid but DIFFERENT
	// payloads — the divergent-payload contention case. When false the scopes
	// are byte-identical in payload and differ only in envelope provenance
	// (scope_id/generation_id -> distinct source_fact_id), the pure
	// envelope-contention case (the minimal #5007 repro).
	Divergent bool
	// CollectorLabel is the informational cassette.File.Collector value.
	// Defaults to "gcp_synthetic" when empty.
	CollectorLabel string
}

// validate fails closed on a malformed OverlappingScopeOptions.
func (o OverlappingScopeOptions) validate() error {
	if o.Scopes < 2 {
		return fmt.Errorf("synth/gcp: OverlappingScopeOptions.Scopes must be >= 2 for contention, got %d", o.Scopes)
	}
	if o.ResourceCount <= 0 {
		return fmt.Errorf("synth/gcp: OverlappingScopeOptions.ResourceCount must be positive, got %d", o.ResourceCount)
	}
	return nil
}

// overlappingScopeProjectID is the single shared GCP project ID every contending
// scope carries. Because the CloudResource node uid folds full_resource_name
// (which embeds ProjectID) in, one shared ProjectID means one shared uid set
// across every generated scope — the collision that makes this a contention
// fixture. It is distinct from multiScopeProjectPrefix and the demo-org project
// so a contention cassette can be replayed alongside either without unintended
// collisions between fixtures.
const overlappingScopeProjectID = "acme-demo-gcp-contended"

// overlappingScopeID derives the distinct durable scope identity for the i-th
// contending scope. The scopes must NOT share a scope_id (that would make them
// the same scope, fencing each other) — only their resource identity (uid)
// overlaps. The distinct scope_id also makes each scope's replayed
// source_fact_id distinct (fact_id folds scope_id in,
// go/internal/replay/cassette/source.go), which is exactly the per-scope
// provenance divergence the owner ledger's source_fact_id tie-break resolves.
func overlappingScopeID(index int) string {
	return fmt.Sprintf("gcp:contention:%s:scope:%02d", overlappingScopeProjectID, index)
}

// GenerateOverlappingScope builds a deterministic, seeded synthetic GCP cassette
// whose opts.Scopes scopes deliberately share one resource-identity set, and
// returns its canonical v1 bytes. It generates the shared resource set ONCE via
// the single-scope Generate (with the fixed overlappingScope ProjectID), then
// replicates that scope opts.Scopes times under distinct scope_id/generation_id
// values so the replayed facts carry the same CloudResource node uid but
// distinct envelope provenance. When opts.Divergent, each replica's resource
// state is mutated so the shared-uid scopes also carry divergent OBSERVED STATE.
//
// Driven through the reducer, the K scopes produce K intents that all contend on
// the same CloudResource node with distinct source_order_key values, so the
// owner ledger (internal/graphowner) resolves the node to the
// max-(observed_at, source_fact_id) contributor regardless of commit order or
// worker count. See docs/internal/design/5007-cross-scope-node-ownership.md.
func GenerateOverlappingScope(opts OverlappingScopeOptions) ([]byte, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	base, err := Generate(Options{
		Seed:           opts.Seed,
		ProjectID:      overlappingScopeProjectID,
		ResourceCount:  opts.ResourceCount,
		CollectorLabel: opts.CollectorLabel,
	})
	if err != nil {
		return nil, fmt.Errorf("synth/gcp: generate contention base scope: %w", err)
	}
	baseFile, err := cassette.ParseAndValidate(base)
	if err != nil {
		return nil, fmt.Errorf("synth/gcp: parse contention base scope: %w", err)
	}
	if len(baseFile.Scopes) != 1 {
		return nil, fmt.Errorf("synth/gcp: contention base: expected exactly one scope from Generate, got %d", len(baseFile.Scopes))
	}
	baseScope := baseFile.Scopes[0]

	scopes := make([]cassette.Scope, 0, opts.Scopes)
	for i := 0; i < opts.Scopes; i++ {
		scopeID := overlappingScopeID(i)
		replica := baseScope
		replica.ScopeID = scopeID
		replica.PartitionKey = scopeID
		replica.GenerationID = replay.DerivedGenerationID(scopeID)
		replica.Facts = cloneOverlappingFacts(baseScope.Facts, i, opts.Divergent)
		scopes = append(scopes, replica)
	}

	merged := cassette.File{
		Collector:     Options{CollectorLabel: opts.CollectorLabel}.collectorLabel(),
		SchemaVersion: cassette.SchemaVersionV1,
		Scopes:        scopes,
	}
	rawMerged, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("synth/gcp: marshal contention cassette: %w", err)
	}
	canonical, err := canonicalizeValue(mustDecodeJSON(rawMerged))
	if err != nil {
		return nil, fmt.Errorf("synth/gcp: canonicalize contention cassette: %w", err)
	}
	if _, err := cassette.ParseAndValidate(canonical); err != nil {
		return nil, fmt.Errorf("synth/gcp: generated contention cassette failed validation: %w", err)
	}
	return canonical, nil
}

// cloneOverlappingFacts deep-copies a scope's facts for the i-th contending
// replica. Identity fields (full_resource_name, asset_type, project, location)
// are preserved verbatim so every replica folds to the same CloudResource node
// uid. When divergent is true, each fact's OBSERVED-STATE state field is
// suffixed with the scope index so the shared-uid scopes carry distinct observed
// state; the stable_fact_key is preserved so per-scope dedup still keys on the
// resource, not the mutated state.
func cloneOverlappingFacts(facts []cassette.Fact, scopeIndex int, divergent bool) []cassette.Fact {
	cloned := make([]cassette.Fact, 0, len(facts))
	for _, f := range facts {
		copyFact := f
		copyFact.Payload = clonePayload(f.Payload)
		if divergent {
			if state, ok := copyFact.Payload["state"].(string); ok {
				copyFact.Payload["state"] = fmt.Sprintf("%s-scope%02d", state, scopeIndex)
			}
		}
		cloned = append(cloned, copyFact)
	}
	return cloned
}

// clonePayload returns a shallow-then-nested copy of a fact payload map so a
// per-replica mutation cannot alias the shared base scope's payload. Only the
// top-level map is copied by value plus nested maps recursively; scalar and
// slice leaves are shared read-only (the generator never mutates a slice leaf).
func clonePayload(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(payload))
	for k, v := range payload {
		if nested, ok := v.(map[string]any); ok {
			out[k] = clonePayload(nested)
			continue
		}
		out[k] = v
	}
	return out
}
