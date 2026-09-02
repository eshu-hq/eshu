// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package obscoverage correlates AWS-native observability objects (CloudWatch
// alarms and dashboards, X-Ray sampling rules) and Grafana-stack declared/
// applied/observed metadata against the monitored CloudResource nodes they
// cover, and publishes those decisions as durable reducer facts plus, for the
// exact-match subset, canonical COVERS graph edges.
//
// The family owns two handlers over one shared classifier pipeline (issue
// #391): ObservabilityCoverageCorrelationHandler builds a provenance-only
// decision (and gap finding) per candidate via BuildObservabilityCoverageDecisions,
// covering both the AWS-native alarm/dashboard/X-Ray join
// (observability_coverage_correlation_index.go,
// observability_coverage_correlation_classify.go) and the Grafana-stack
// declared/applied/observed metadata evidence
// (observability_coverage_metadata.go); ObservabilityCoverageMaterializationHandler
// (issue #391 PR3) re-runs the same classifier and projects only the
// exact-coverage decisions that resolved a target CloudResource uid into
// canonical COVERS edges, gated on the #805 PR1 canonical-nodes-committed
// readiness phase so edges never resolve against a generation whose nodes
// have not yet committed.
//
// A coverage decision measures whether a CloudResource is watched, not
// whether what it is watched by is healthy: no metric value, alert state, or
// dashboard body is ever read as graph truth, only the identity link between
// the observability object and its target. See
// docs/internal/design/391-observability-coverage-correlation.md.
//
// # Package boundary
//
// Imports point strictly downward: this package reaches [reducercontract],
// [factdecode], [factload], [factwrite], [gpphase], [payloadcore],
// [schemadecode], internal/facts, internal/telemetry, and internal/truth, and
// never the parent internal/reducer package. The reducer root keeps
// compatibility aliases in observability_coverage_compat.go so its own
// callers compile unchanged; that direction is root importing this family,
// never the reverse. See AGENTS.md in this directory before adding an import.
//
// # Observability
//
// ObservabilityCoverageCorrelationHandler.Handle emits
// eshu_dp_observability_coverage_correlations_total (labeled by domain,
// outcome, and coverage_signal) once per non-empty outcome after classifying
// a batch. ObservabilityCoverageMaterializationHandler.Handle wraps its work
// in the reducer.observability_coverage_materialization span and emits
// eshu_dp_observability_coverage_edges_total (labeled by coverage_signal and
// resolution_mode) for each materialized exact-coverage edge; provenance-only
// coverage never produces an edge and is counted by
// ObservabilityCoverageCorrelations instead. Both handlers route decode
// failures through the shared eshu_dp_reducer_input_invalid_facts_total
// counter, and every execution stays covered by
// eshu_dp_reducer_executions_total and eshu_dp_reducer_run_duration_seconds.
package obscoverage
