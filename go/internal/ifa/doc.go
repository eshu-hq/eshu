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
package ifa
