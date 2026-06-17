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
// intentional reconciliation gaps.
//
// Build merges the matrix and overlay with live code Signals (the MCP tool
// registry) and returns a Catalog plus reconciliation Findings. A non-empty
// Findings slice means a public surface lacks a catalog entry, a catalog entry
// claims a surface with no code evidence, or the overlay is stale. The package
// does not import the MCP or query packages; callers inject their registries
// through Signals so the catalog stays free of HTTP and graph dependencies.
//
// Build derives each entry's maturity from the matrix support statuses
// (general_availability, experimental, preview, not_implemented). The overlay
// may override maturity with the operational states the matrix cannot express
// (gated, degraded), and the entry records both the effective and the derived
// maturity so drift between them stays visible.
//
// Load returns the committed, generated artifact embedded from
// data/catalog.generated.json. It is the runtime entry point for the API, MCP,
// and console surfaces. The artifact is produced by cmd/capability-inventory and
// kept in lockstep with the specs by a drift test.
package capabilitycatalog
