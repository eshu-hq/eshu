// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ifa implements the Ifá conformance platform's contract layer: an
// Odù (facts.Envelope inputs, optionally loaded through the projector
// FactStore.LoadFacts seam) canonicalized with the shared replay
// canonicalizer, plus P1's derived expectations and coverage reconciliation.
//
// Derive (expectations.go) computes, for every fact-kind-registry entry, its
// replay-coverage query binding, its fixturepack payload-schema derivation,
// and whether its facts ever reach relationships.DiscoverEvidence — nothing
// hand-listed. RepositoryCatalog and DiscoveredEvidence (evidence.go) run the
// production evidence extractor over an Odù's own facts; EvidenceSatisfies
// checks a B-12 evidence-narrowed required correlation against the result.
// ValidateOduPayloads (schema.go) validates an Odù's facts against the SDK's
// payload-schema conformance seam. RunCoverage (coverage.go) reconciles the
// derived surfaces against Ifá's own coverage manifest
// (specs/ifa-coverage-manifest.v1.yaml) by reusing
// go/internal/replaycoverage's Reconcile/BuildReport/Findings machinery
// unchanged. This package still does not import collector or parser
// internals: the production extractor and SDK validator are its only
// derivation seams into that layer.
//
// RoundTripTypedPayloads (roundtrip.go) is the P1 terminal "contract system
// alive" proof (issue #4804): it decodes every fact in an Odù through its
// kind's factschema Decode* function and re-encodes it, asserting the
// re-encoded payload's canonical bytes exactly match the original — proving
// the SDK's typed struct for that fact kind neither drops nor reshapes a
// field the collector emitted, a stronger claim than payload-schema
// conformance alone. demoOrgRoundtripOdu seeds the catalog with every fact
// the demo-org synthetic GCP cassette (go/internal/synth/gcp) generates,
// replayed through the production cassette.Source seam via
// gcp.DemoOrgFactEnvelopes. Importing synth/gcp is boundary-legal: it is a
// synthetic fixture generator that itself never imports collector internals
// and emits only through the same typed factschema Encode* seam a real
// collector would use, so this package still touches no collector or parser
// internals directly.
//
// RunMaterializedEdgeCoverage derives reducer edge-family requirements and
// resolves them against hand-derived Odù expectations. SQL relationships
// require baseline, delta-tombstone, and fault dimensions; the determinism
// matrix proves baseline and accumulated gen-2 delta truth with live exact-set
// assertions across all nine SQL writer-registry types, including
// REFERENCES_TABLE and WRITES_TO, while an unproven fault dimension remains
// explicitly waived. The registry-derived inventory fails closed if a future
// SQL edge type is added without a matching Odù expectation.
//
// P5 adds the Layer 3 load vocabulary. AmplifyAtSlot (amplify.go) replays one
// base Odù across a scale-lab slot's disjoint synthetic scopes through the
// family-native generator (go/internal/synth/gcp.GenerateMultiScope),
// deliberately rejecting the determinism-unsafe generic scope_id/stable_fact_key
// rewrite the ADR's Layer 3 landmine warns against. ScaleSlot (slots.go) adopts
// specs/scale-lab-corpus.v1.yaml's corpus slots as the load taxonomy, binding
// each to a fan-out and a perfcontract enforcement class (smoke/small hermetic,
// medium+ operator-gated) rather than inventing a second taxonomy or perf
// contract. The runtime scenario runners over these — the throughput Odù and the
// #3560 saturation regression — live in the sibling subpackages
// go/internal/ifa/throughput and go/internal/ifa/saturation, keeping this core
// package pure and deterministic (no wall-clock, concurrency, or storage seams).
// RepoDependencyBackfillProofOdu is likewise a lazy, uncataloged storage-proof
// scenario: it preserves the repository-dependency truth fixture while
// reproducing retained worst-scope relationship-backfill cardinality and skew.
package ifa
