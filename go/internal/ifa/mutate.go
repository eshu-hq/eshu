// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// MutationKind selects how MutateCassette corrupts each selected fact's
// payload. Both kinds make sdk/go/factschema's decode seam classify the fact
// ClassificationInputInvalid, but the surrounding runtime treats them very
// differently — proven empirically with
// scripts/verify-ifa-dead-letter-determinism.sh against a real Postgres +
// NornicDB stack, not just by reading the decode seam:
//
//   - A missing required field passes every earlier admission gate (its
//     schema_version is untouched) and is QUARANTINED per fact once a
//     canonical extractor or reducer handler decodes it:
//     go/internal/reducer/factschema_decode.go's partitionDecodeFailures (and
//     its projector-side twin, go/internal/projector/factschema_quarantine.go)
//     skip that one fact, increment a metric, and log a structured error,
//     but the surrounding work item still SUCCEEDS. No fact_work_items row is
//     ever written for it — this outcome is NOT durable and NOT comparable by
//     a fact_work_items query. Confirmed empirically: driving a
//     missing-field-mutated cassette produced 0 dead_letter rows and the
//     "reducer input_invalid fact quarantined" log line.
//   - An unsupported schema major on a fact kind core owns a registered
//     version for (facts.SchemaVersion, e.g. gcp_cloud_resource -> "1.1.0")
//     is caught EARLIER than the reducer's typed-decode seam: the
//     projector's own per-fact admission gate
//     (go/internal/projector/schema_version_admission.go's
//     validateFactSchemaVersion, called from buildProjection in
//     go/internal/projector/runtime.go) rejects it before canonicalization
//     even starts. That gate fails the WHOLE projector work item for the
//     scope/generation on the FIRST offending fact — not a per-fact skip —
//     so the reducer's own follow-up materialization intents (e.g.
//     gcp_resource_materialization) are never even enqueued. The projector
//     work item then dead-letters durably: fact_work_items.status=
//     'dead_letter', stage='projector', domain='source_local'. Confirmed
//     empirically the durable failure_class landed as "projection_bug" (the
//     projector's own operator-facing triage label from
//     go/internal/projector's dead-letter triage, NOT the reducer's
//     "input_invalid" factDecodeError.FailureClass() literal) — do not assume
//     the string "input_invalid" appears in this row; assert on
//     status='dead_letter' and compare the whole DeadLetterRecord set instead
//     of pattern-matching one failure_class value.
//
// This distinction is why step 3a of
// docs/internal/design/4389-ifa-conformance-platform.md ("identical
// fact_work_items dead-letter set across N") must inject MutationSchemaMajor,
// not MutationMissingField, to produce evidence a determinism matrix can
// compare — MutationSchemaMajor is the only one of the two that reaches a
// durable, comparable dead-letter row at all, regardless of which exact
// failure_class label the runtime assigns it.
type MutationKind string

const (
	// MutationMissingField deletes one required payload key from a selected
	// fact, producing a per-fact QUARANTINE (metric + log, no durable
	// fact_work_items row) once a canonical extractor or reducer handler
	// decodes it. Kept as a mutation kind so callers (and this package's own
	// tests) can assert that non-durable outcome explicitly rather than
	// merely assuming it.
	MutationMissingField MutationKind = "missing-field"

	// MutationSchemaMajor overwrites a selected fact's schema_version with an
	// unsupported major (for example "99.0.0"). For a fact kind core
	// registers a schema version for, this trips the projector's own
	// admission-time schema-version gate before the reducer's typed-decode
	// seam is ever reached, failing and durably dead-lettering the WHOLE
	// projector work item for that scope/generation (see the MutationKind doc
	// comment for the exact call path and the empirical failure_class
	// caveat). This is the mutation kind step 3a's cross-run
	// dead-letter-set comparison needs.
	MutationSchemaMajor MutationKind = "schema-major"
)

// MutateOptions configures MutateCassette.
type MutateOptions struct {
	// FactKind selects which fact kind's facts are eligible for mutation (for
	// example "gcp_cloud_resource"). Required.
	FactKind string
	// Kind selects the corruption applied to each selected fact. Required.
	Kind MutationKind
	// Field is the required payload key to delete. Required for
	// MutationMissingField; ignored otherwise.
	Field string
	// SchemaMajor is the replacement schema_version string. Required for
	// MutationSchemaMajor; ignored otherwise.
	SchemaMajor string
	// Count is the number of facts to mutate. Values <= 0 default to 1.
	Count int
}

// MutatedFact names one fact MutateCassette corrupted, so a caller can log or
// assert against exactly which fact was targeted.
type MutatedFact struct {
	// ScopeID is the durable scope identity the mutated fact belongs to.
	ScopeID string
	// GenerationID is the durable generation identity the mutated fact
	// belongs to.
	GenerationID string
	// StableFactKey is the mutated fact's deduplication key.
	StableFactKey string
	// FactKind is the mutated fact's kind.
	FactKind string
	// Field is the payload key that was deleted. Empty for
	// MutationSchemaMajor, which corrupts schema_version instead of a payload
	// field.
	Field string
}

// MutateCassette returns a deep copy of src with exactly opts.Count facts of
// opts.FactKind corrupted per opts.Kind. Facts are selected deterministically
// by ascending StableFactKey across every scope (ties broken by scope order,
// then fact order within the scope), so re-running MutateCassette with the
// same options against the same cassette always corrupts the identical
// facts — the property the P3 determinism matrix needs to compare dead-letter
// sets across independent runs.
//
// src is never mutated. This exists specifically so a committed testdata
// cassette (for example testdata/cassettes/gcpcloud/supply-chain-demo.json)
// can be loaded, mutated in memory, and written to a scratch path without ever
// touching the checked-in file — see AGENTS.md's "never edit the committed
// cassette" invariant for this package's callers.
func MutateCassette(src cassette.File, opts MutateOptions) (cassette.File, []MutatedFact, error) {
	if strings.TrimSpace(opts.FactKind) == "" {
		return cassette.File{}, nil, errors.New("ifa: mutate cassette: fact kind is required")
	}
	switch opts.Kind {
	case MutationMissingField:
		if strings.TrimSpace(opts.Field) == "" {
			return cassette.File{}, nil, errors.New("ifa: mutate cassette: field is required for missing-field mutation")
		}
	case MutationSchemaMajor:
		if strings.TrimSpace(opts.SchemaMajor) == "" {
			return cassette.File{}, nil, errors.New("ifa: mutate cassette: schema major is required for schema-major mutation")
		}
	default:
		return cassette.File{}, nil, fmt.Errorf("ifa: mutate cassette: unknown mutation kind %q", opts.Kind)
	}

	count := opts.Count
	if count <= 0 {
		count = 1
	}

	dup, err := cloneCassette(src)
	if err != nil {
		return cassette.File{}, nil, err
	}

	targets := selectMutationTargets(dup, opts.FactKind)
	if len(targets) < count {
		return cassette.File{}, nil, fmt.Errorf(
			"ifa: mutate cassette: found %d fact(s) of kind %q, want at least %d",
			len(targets), opts.FactKind, count,
		)
	}

	mutated := make([]MutatedFact, 0, count)
	for _, t := range targets[:count] {
		fact := &dup.Scopes[t.scopeIdx].Facts[t.factIdx]
		if err := applyMutation(fact, opts); err != nil {
			return cassette.File{}, nil, err
		}
		mutated = append(mutated, MutatedFact{
			ScopeID:       dup.Scopes[t.scopeIdx].ScopeID,
			GenerationID:  dup.Scopes[t.scopeIdx].GenerationID,
			StableFactKey: fact.StableFactKey,
			FactKind:      fact.FactKind,
			Field:         opts.Field,
		})
	}

	return dup, mutated, nil
}

// mutationTarget locates one candidate fact by its position in dup.Scopes, so
// selectMutationTargets can sort candidates by their fact's StableFactKey
// without copying the fact itself.
type mutationTarget struct {
	scopeIdx int
	factIdx  int
}

// selectMutationTargets returns every fact of factKind across every scope in
// f, sorted deterministically by (StableFactKey, scope order, fact order).
// StableFactKey is the primary sort key because it is the cassette's own
// durable identity for a fact — sorting on it (rather than raw slice order,
// which is whatever order the recording tool happened to emit) is what makes
// MutateCassette's selection stable across a cassette that gets re-recorded
// with reordered scopes or facts but the same fact identities.
func selectMutationTargets(f cassette.File, factKind string) []mutationTarget {
	var targets []mutationTarget
	for si, s := range f.Scopes {
		for fi, fact := range s.Facts {
			if fact.FactKind == factKind {
				targets = append(targets, mutationTarget{scopeIdx: si, factIdx: fi})
			}
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		a := f.Scopes[targets[i].scopeIdx].Facts[targets[i].factIdx]
		b := f.Scopes[targets[j].scopeIdx].Facts[targets[j].factIdx]
		if a.StableFactKey != b.StableFactKey {
			return a.StableFactKey < b.StableFactKey
		}
		if targets[i].scopeIdx != targets[j].scopeIdx {
			return targets[i].scopeIdx < targets[j].scopeIdx
		}
		return targets[i].factIdx < targets[j].factIdx
	})
	return targets
}

// applyMutation corrupts one fact in place per opts.Kind. fact is a pointer
// into the caller's already-cloned cassette, never the original src passed to
// MutateCassette.
func applyMutation(fact *cassette.Fact, opts MutateOptions) error {
	switch opts.Kind {
	case MutationMissingField:
		if _, ok := fact.Payload[opts.Field]; !ok {
			return fmt.Errorf(
				"ifa: mutate cassette: fact %q has no field %q to delete",
				fact.StableFactKey, opts.Field,
			)
		}
		delete(fact.Payload, opts.Field)
	case MutationSchemaMajor:
		fact.SchemaVersion = opts.SchemaMajor
	}
	return nil
}

// cloneCassette returns a deep copy of f via a JSON round trip, so
// MutateCassette never shares a nested Payload map with the caller's src —
// mirroring this package's existing Odù-immutability rule (see AGENTS.md:
// "Odù facts are treated as immutable inputs; clone envelopes before
// rendering").
func cloneCassette(f cassette.File) (cassette.File, error) {
	data, err := json.Marshal(f)
	if err != nil {
		return cassette.File{}, fmt.Errorf("ifa: clone cassette: marshal: %w", err)
	}
	var dup cassette.File
	if err := json.Unmarshal(data, &dup); err != nil {
		return cassette.File{}, fmt.Errorf("ifa: clone cassette: unmarshal: %w", err)
	}
	return dup, nil
}
