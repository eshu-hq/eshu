// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package capabilitycatalog reconciles Eshu capability truth into one
// deterministic, auditable catalog.
//
// The capability matrix (specs/capability-matrix.v1.yaml plus
// specs/capability-matrix/*.yaml fragments) is the machine-readable source of
// truth for per-profile support, truth ceilings, declared tools, and proof
// signals. The editorial overlay (specs/capability-catalog.v1.yaml) supplies the
// metadata that cannot be derived from the matrix: display names, owning
// packages, operational maturity overrides (gated, degraded), known gaps,
// linked issues, docs, and the exemption and non-MCP-surface lists that record
// intentional reconciliation gaps. The authorization catalog
// (specs/authorization-catalog.v1.yaml) supplies the v1 built-in roles, data
// classes, permission families, bootstrap-owner posture, and custom-policy
// deferral that are attached to generated entries.
//
// Build merges the matrix and overlay with live code Signals (the MCP tool
// registry) and returns a Catalog plus reconciliation Findings. BuildFromSpecs
// also loads the authorization catalog so every real capability gets explicit
// role, action, scope, and data-class metadata. A non-empty Findings slice means
// a public surface lacks a catalog entry, a catalog entry claims a surface with
// no code evidence, the overlay is stale, or authorization metadata is missing
// or malformed. Authorization reconciliation also verifies that every default
// role for a permission family explicitly grants that family action with the
// family data classes and scope levels. The package does not import the MCP or
// query packages; callers inject their registries through Signals so the catalog
// stays free of HTTP and graph dependencies.
//
// Build derives each entry's maturity from the matrix support statuses
// (general_availability, experimental, preview, not_implemented). The overlay
// may override maturity with the operational states the matrix cannot express
// (gated, degraded), and the entry records both the effective and the derived
// maturity so drift between them stays visible.
// LoadProductClaimLedger, ParseProductClaimMarkers, and CheckProductClaims extend
// the same reconciliation discipline to broad public prose by binding guarded
// source markers and whole-line quotes to capability ids, owner paths, generated
// public surfaces, deterministic proof, generated surface counts, catalog proof
// signals, semantic-output posture, and tracking issue state.
// Matrix-declared p95 latency and max-scope budget claims are retained in each
// profile so CheckBudgetProof can bind them to public-safe measured API/MCP
// artifacts.
//
// Load returns the committed, generated artifact embedded from
// data/catalog.generated.json. It is the runtime entry point for the API, MCP,
// and console surfaces, including the top-level authorization catalog and
// per-entry authorization requirements. The artifact is produced by
// cmd/capability-inventory and kept in lockstep with the specs by a drift test.
//
// The package also reconciles the surface inventory: a generated record of every
// platform surface across six categories (command binaries, collector families,
// reducer domains, API routes, MCP tools, and console pages). BuildSurfaceInventory
// merges the live surfaces enumerated from code, specs, and the source tree
// (LiveSurfaces, injected by the generator) with the editorial overlay
// (specs/surface-inventory.v1.yaml) into a deterministic SurfaceInventory plus
// Findings. Each surface carries a ReadinessLane (implemented, partial, gated,
// foundation_only, fixture_only, research_only, not_implemented, unsupported);
// only the implemented lane asserts production readiness and therefore requires
// linked promotion proof. Collector rows may also carry a CollectorContract that
// maps live fact kinds to projection/read consumers, proof gates, fixture refs,
// and deterministic/provider-gated/optional-semantic truth profile. A non-empty
// Findings slice means a collector is unclassified, an implemented collector
// links no proof, an overlay row is stale, a lane is invalid, or a live
// collector fact kind is missing from its contract. LoadSurfaceInventory returns
// the committed artifact embedded from data/surface-inventory.generated.json,
// and a drift test keeps it in lockstep with live code so no surface can appear
// or disappear silently.
package capabilitycatalog
