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
package ifa
