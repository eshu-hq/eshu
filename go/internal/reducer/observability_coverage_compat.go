// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/reducer/obscoverage"

// This file is the transitional compatibility surface for the observability
// coverage correlation and materialization family that moved to [obscoverage]
// (issue #6061). Reducer-root call sites keep their current spelling; each
// entry is deleted once its last caller has moved into a family subpackage.

// ObservabilityCoverageCorrelationHandler correlates observability coverage
// evidence into durable provenance-only decisions. See
// [obscoverage.ObservabilityCoverageCorrelationHandler].
type ObservabilityCoverageCorrelationHandler = obscoverage.ObservabilityCoverageCorrelationHandler

// ObservabilityCoverageCorrelationWriter persists reducer-owned observability
// coverage correlations. See
// [obscoverage.ObservabilityCoverageCorrelationWriter].
type ObservabilityCoverageCorrelationWriter = obscoverage.ObservabilityCoverageCorrelationWriter

// ObservabilityCoverageCorrelationWrite carries decisions for durable
// publication for one scope generation. See
// [obscoverage.ObservabilityCoverageCorrelationWrite].
type ObservabilityCoverageCorrelationWrite = obscoverage.ObservabilityCoverageCorrelationWrite

// ObservabilityCoverageCorrelationWriteResult summarizes durable coverage
// writes. See [obscoverage.ObservabilityCoverageCorrelationWriteResult].
type ObservabilityCoverageCorrelationWriteResult = obscoverage.ObservabilityCoverageCorrelationWriteResult

// ObservabilityCoverageMaterializationHandler projects exact observability
// coverage decisions into canonical COVERS graph edges. See
// [obscoverage.ObservabilityCoverageMaterializationHandler].
type ObservabilityCoverageMaterializationHandler = obscoverage.ObservabilityCoverageMaterializationHandler

// ObservabilityCoverageEdgeWriter persists and retracts canonical COVERS
// edges. See [obscoverage.ObservabilityCoverageEdgeWriter].
type ObservabilityCoverageEdgeWriter = obscoverage.ObservabilityCoverageEdgeWriter

// PostgresObservabilityCoverageCorrelationWriter stores reducer-owned
// observability coverage correlation decisions in the shared fact store. See
// [obscoverage.PostgresObservabilityCoverageCorrelationWriter].
type PostgresObservabilityCoverageCorrelationWriter = obscoverage.PostgresObservabilityCoverageCorrelationWriter

// ObservabilityCoverageEvidenceSource forwards to
// [obscoverage.ObservabilityCoverageEvidenceSource].
func ObservabilityCoverageEvidenceSource() string {
	return obscoverage.ObservabilityCoverageEvidenceSource()
}

// observabilityCoverageMaterializationDomainDefinition forwards to
// [obscoverage.MaterializationDomainDefinition].
func observabilityCoverageMaterializationDomainDefinition() DomainDefinition {
	return obscoverage.MaterializationDomainDefinition()
}

// ObservabilityCoverageNodesNotReadyFailureClass identifies an in-handler
// readiness-gate miss for observability coverage materialization. See
// [obscoverage.ObservabilityCoverageNodesNotReadyFailureClass].
const ObservabilityCoverageNodesNotReadyFailureClass = obscoverage.ObservabilityCoverageNodesNotReadyFailureClass
