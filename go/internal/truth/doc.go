// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package truth defines the canonical truth contracts shared across Eshu:
// the layered materialization contract and the unified evidence record.
//
// Layer enumerates the four bounded source layers: source_declaration,
// applied_declaration, observed_resource, and canonical_asset. Contract binds
// a canonical kind to the set of source layers a reducer accepts as evidence
// for that kind. Validate enforces non-empty source layers, rejects
// canonical_asset as a source, and rejects duplicates.
//
// Evidence is the single canonical evidence value (issue #3489). It carries
// BOTH a bounded [0,1] Confidence AND a byte-level Citation (file or entity
// locator, line range, byte offset/length, content hash, commit), plus typed
// Provenance. It replaces three former shapes that each carried only part of
// that contract: relationship evidence (confidence, no byte citation),
// citation records (byte citation, no confidence), and documentation packets.
// Validate bounds confidence, rejects inconsistent citations, and requires a
// known provenance basis.
//
// DeploymentTruthTier is the closed, strictly-ranked vocabulary for the
// strongest class of deployment evidence available for a traced workload
// (#5471): runtime_confirmed (a live observation, e.g. an exact
// kubernetes_live correlation) outranks provenance_ci_declared (CI/CD or
// supply-chain provenance), which outranks declared_ref (a named ref
// declared deployed; not yet wired), which outranks config_only (only
// config-materialized evidence). ClassifyDeploymentTruthTier is the single
// shared classifier trace_deployment_chain, supply_chain_impact, and
// service story all read through, so the tier a workload reports is
// consistent across every surface that emits it.
package truth
