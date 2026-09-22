// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/cloud"
)

// Evidence is the slice of the operator status report that the collector
// readbacks derive from. It exists so this package never has to name the root
// status.Report: internal/status aggregates every family leaf into its report
// types, so a leaf that took a Report would close an import cycle.
//
// The root builds one of these per render and passes it to RuntimeStatuses,
// PromotionProofs and PresentCatalog. Every field is optional: a zero Evidence
// yields an empty readback rather than an error, which is what a status
// surface with no coordinator and no scan history must report.
type Evidence struct {
	// AsOf is the report's observation time, used as the staleness reference
	// when PromotionOptions does not supply its own.
	AsOf time.Time
	// Instances are the workflow-coordinator collector registrations. The root
	// passes nil when the report carries no coordinator snapshot, which the
	// readback treats the same as a coordinator with no instances.
	Instances []InstanceSummary
	// AWSScans is direct AWS cloud-scan evidence for collectors that report
	// outside the coordinator.
	AWSScans []cloud.AWSScanStatus
	// VulnerabilitySources is durable per-source checkpoint evidence, keyed by
	// collector instance.
	VulnerabilitySources []VulnerabilitySourceState
	// FactEvidence is aggregated fact-emission evidence per collector instance.
	FactEvidence []FactEvidence
}
