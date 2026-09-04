// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package securityalert owns the security-alert reconciliation family:
// comparing provider-reported repository security alerts (GitHub Dependabot
// and equivalent collectors, decoded through the typed
// security_alert.repository_alert seam) against Eshu-owned dependency
// consumption and supply-chain-impact evidence, and publishing a durable
// per-alert comparison verdict without itself promoting an alert into impact
// truth.
//
// It covers the reconciliation decision builders (BuildSecurityAlertReconciliations,
// BuildSecurityAlertReconciliationsWithQuarantine), the reducer handler
// (SecurityAlertReconciliationHandler), the domain definition
// (SecurityAlertReconciliationDomainDefinition), and the Postgres writer
// (PostgresSecurityAlertReconciliationWriter). SecurityAlertReconciliationDecision
// is the family's canonical output row; SecurityAlertReconciliationStatus
// names the possible comparison outcomes (matched, unmatched, stale,
// dismissed, fixed, provider_only, unsupported, ambiguous).
//
// # The manifest-consumption injection seam
//
// One piece of the family's pre-move behavior does not live here:
// reconciling a provider alert against repository manifest/lockfile
// dependency evidence. That logic (extractSecurityAlertManifestConsumptions,
// security_alert_manifest_dependency_match.go in the reducer root) depends on
// extractPackageManifestDependencies and packageConsumptionKeys --
// package-identity decode and normalization logic owned by the reducer
// root's still-in-root package-consumption-correlation family (issue #6061).
// A family subpackage may never import the reducer root, so this package
// exposes ManifestConsumptionExtractor, a function type both
// BuildSecurityAlertReconciliations/WithQuarantine and
// SecurityAlertReconciliationHandler (its ExtractManifestConsumptions field)
// accept as an injected dependency. The reducer root wires its own
// unexported bridge function into every construction site
// (defaults_additive_domains_supply_chain.go,
// supply_chain_impact_security_alert.go), and the reducer root's own test
// files exercise the real manifest-matching behavior end to end
// (security_alert_reconciliation_lockfile_test.go,
// security_alert_scoped_npm_test.go) because this package cannot build a
// working extractor on its own. Passing a nil extractor is valid and simply
// skips the manifest-consumption half of the evidence set -- every other test
// in this package uses that path.
//
// The package never imports the parent reducer package. Everything else it
// needs from the reducer's shared vocabulary comes from leaf packages
// instead: contract for the domain, intent, and result types; factload for
// fact loading; factdecode for quarantine handling and its
// QuarantinedFact/InputInvalidSubSignals/RecordQuarantinedFacts; factwrite for
// the batched fact-row writer; payloadcore for payload accessors and string
// helpers; and schemadecode for the sdk/go/factschema
// security_alert.repository_alert decode seam. activeRepositoryFactLoader and
// activePackageManifestDependencyFactLoader are declared locally rather than
// imported (security_alert_reconciliation_handler.go): the reducer root's own
// copies (package_source_correlation_handler.go) are shared by several
// families that have not moved out of root yet, and Go interfaces are
// satisfied structurally, so the same concrete FactLoader implementation root
// wires into other families' handlers also satisfies these local
// declarations without duplicating any logic -- the same pattern
// internal/reducer/codetaint's graph_ports.go established. A handful of
// small, pure, reducer-root-owned helpers this package's own logic touches
// (package-name-from-purl/package-ID parsing, the dependency-scope and
// exact-version-match fallbacks, the evidence-kind default) are copied
// locally rather than imported for the same reason; each copy names its root
// source and the reasoning in a doc comment at its definition.
//
// The reducer root keeps its own manifest-consumption bridge
// (security_alert_manifest_dependency_match.go) and re-exports nothing else:
// every other reducer-root or module caller now names this package's
// exported symbols directly: cmd/reducer names
// PostgresSecurityAlertReconciliationWriter, internal/storage/postgres names
// SecurityAlertReconciliationFactFilter, and internal/replay/costcounting's
// cost test names SecurityAlertReconciliationDecision along with Matched,
// Write, and the writer. Nothing under internal/query imports this package.
// So the import direction stays one-way -- root and its remaining
// supply_chain_impact family depend on securityalert, never the reverse.
//
// Telemetry: the handler records quarantined
// security_alert.repository_alert facts (a payload missing its required
// repository_id) through factdecode.RecordQuarantinedFacts, which feeds the
// shared eshu_dp_reducer_input_invalid_facts_total counter, and reports
// SubSignals["input_invalid_facts"] on its Result. The package registers no
// domain-specific instrument of its own; its writes are covered by the same
// eshu_dp_reducer_executions_total / eshu_dp_reducer_run_duration_seconds
// pair every reducer handler execution emits.
package securityalert
