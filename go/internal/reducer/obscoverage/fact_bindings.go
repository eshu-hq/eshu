// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package obscoverage

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// AWSCoverageMaterializationBinding declares this package's verified reducer consumption of
// aws kinds (issue #6629). Build-time metadata only.
var AWSCoverageMaterializationBinding = contract.Bind(contract.Binding{
	ID: "obscoverage/obscoverage_coverage_materialization_handler", FamilyID: "aws",
	ReducerDir: "go/internal/reducer/obscoverage", Symbol: "ObservabilityCoverageMaterializationHandler",
	Kinds: []string{"aws_relationship", "aws_resource"},
})

// ObservabilityCoverageCorrelationBinding declares this package's verified reducer consumption of
// observability kinds (issue #6629). Build-time metadata only.
var ObservabilityCoverageCorrelationBinding = contract.Bind(contract.Binding{
	ID: "obscoverage/obscoverage_coverage_correlation_handler", FamilyID: "observability",
	ReducerDir: "go/internal/reducer/obscoverage", Symbol: "ObservabilityCoverageCorrelationHandler",
	Kinds: []string{"observability.applied_resource", "observability.applied_sync_state", "observability.coverage_warning", "observability.declared_alert_rule", "observability.declared_dashboard", "observability.declared_datasource", "observability.declared_folder", "observability.declared_log_route", "observability.declared_metric_route", "observability.declared_metric_rule", "observability.declared_scrape_config", "observability.declared_trace_route", "observability.observed_dashboard", "observability.observed_log_signal", "observability.observed_rule", "observability.observed_target", "observability.observed_trace_signal", "observability.source_instance"},
})
