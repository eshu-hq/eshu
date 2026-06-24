// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package extraction computes the advisory collector extraction readiness
// checklist used by component diagnostics.
//
// It answers one question for each collector family: should this collector move
// out of the core Eshu repository, and if not, what is missing? The verdict is
// one of four classifications:
//
//   - KeepInTree: a correlation-critical core collector that creates or
//     preserves code-to-cloud join keys. It stays in tree until a separate
//     architecture gate proves a split keeps correlation correct.
//   - ExtractionCandidate: an eligible vendor-API or support-source collector
//     with no unmet criteria that has not yet been promoted to run out of tree.
//   - Blocked: an eligible family with at least one unmet extraction criterion,
//     reported as concrete blockers.
//   - ExternalReady: a family whose out-of-tree proof is complete and that runs
//     out of tree as its default path.
//
// The criteria mirror the "Extraction Criteria" table in
// docs/public/reference/collector-extraction-policy.md. Evaluate is deterministic
// and total: the same Profile always yields the same Readiness, and a profile
// that omits a criterion fails closed by treating it as unmet.
//
// The package is advisory only. It never moves code, disables a collector,
// mutates a manifest, or changes runtime behavior. Catalog returns the
// evidence-based verdict for every family the extraction policy tracks; the
// policy doc remains the single source of truth for which families are listed.
package extraction
