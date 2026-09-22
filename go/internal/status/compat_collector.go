// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// This file is the status root's compatibility surface for the collector
// family that moved to [collector] (issue #6775). It carries no behavior
// change: every alias names the same type, every constant the same value, and
// every forwarder calls straight through. The packages importing
// internal/status keep compiling unchanged, and each entry is deleted once its
// last caller has moved to the leaf; see the importer-migration child issue. A
// later collector move adds a stanza here and never creates a second compat
// file for this family.
//
// Stanzas:
//   - collector runtime readback (collector_runtime_status.go)
//   - collector promotion proof and catalog
//   - collector backpressure, fact evidence, generation dead letters
//   - vulnerability source state
//   - coordinator collector instance summary

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// collectorEvidence projects the report down to the slice the collector
// readbacks consume. It exists because the leaf must not name Report: the root
// aggregates every family into Report, so a leaf taking one would be an import
// cycle. A nil Coordinator yields nil Instances, which the readback treats the
// same as a coordinator with no registrations -- the pre-nest behavior.
func collectorEvidence(report Report) collector.Evidence {
	evidence := collector.Evidence{
		AsOf:                 report.AsOf,
		AWSScans:             report.AWSCloudScans,
		VulnerabilitySources: report.VulnerabilitySources,
		FactEvidence:         report.CollectorFactEvidence,
	}
	if report.Coordinator != nil {
		evidence.Instances = report.Coordinator.CollectorInstances
	}
	return evidence
}

// Stanza: collector runtime readback.

// CollectorRuntimeStatus is the unified operator view of one collector runtime
// identity.
//
// Deprecated: use [collector.RuntimeStatus].
type CollectorRuntimeStatus = collector.RuntimeStatus

// CollectorRuntimeStatuses derives the collector runtime readback from the
// status report.
//
// Deprecated: use [collector.RuntimeStatuses] with [collector.Evidence].
func CollectorRuntimeStatuses(report Report) []collector.RuntimeStatus {
	return collector.RuntimeStatuses(collectorEvidence(report))
}

// Collector runtime status categories.
//
// Deprecated: use the [collector] constants.
const (
	CollectorRuntimeCoordinatorManaged = collector.RuntimeCoordinatorManaged
	CollectorRuntimeDirectMode         = collector.RuntimeDirectMode
	CollectorRuntimeProfileGated       = collector.RuntimeProfileGated
	CollectorRuntimeDisabled           = collector.RuntimeDisabled
	CollectorRuntimeUnregistered       = collector.RuntimeUnregistered
)

// Stanza: collector promotion proof and catalog.

// CollectorPromotionProof is one collector family's promotion evidence.
//
// Deprecated: use [collector.PromotionProof].
type CollectorPromotionProof = collector.PromotionProof

// CollectorPromotionOptions controls promotion-proof derivation.
//
// Deprecated: use [collector.PromotionOptions].
type CollectorPromotionOptions = collector.PromotionOptions

// CollectorCatalogEntry describes one catalogued collector family.
//
// Deprecated: use [collector.CatalogEntry].
type CollectorCatalogEntry = collector.CatalogEntry

// CollectorPromotionProofs derives the per-collector promotion proof report.
//
// Deprecated: use [collector.PromotionProofs] with [collector.Evidence].
func CollectorPromotionProofs(report Report, opts collector.PromotionOptions) []collector.PromotionProof {
	return collector.PromotionProofs(collectorEvidence(report), opts)
}

// DefaultCollectorCatalog returns the full collector fleet catalog.
//
// Deprecated: use [collector.DefaultCatalog].
func DefaultCollectorCatalog() []collector.CatalogEntry { return collector.DefaultCatalog() }

// KnownCollectorKinds lists every catalogued collector kind.
//
// Deprecated: use [collector.KnownKinds].
func KnownCollectorKinds() []string { return collector.KnownKinds() }

// DefaultCollectorPromotionStaleAfter is the default promotion staleness bound.
//
// Deprecated: use [collector.DefaultPromotionStaleAfter].
const DefaultCollectorPromotionStaleAfter time.Duration = collector.DefaultPromotionStaleAfter

// Collector promotion states.
//
// Deprecated: use the [collector] constants.
const (
	CollectorPromotionImplemented      = collector.PromotionImplemented
	CollectorPromotionPartial          = collector.PromotionPartial
	CollectorPromotionFailed           = collector.PromotionFailed
	CollectorPromotionStale            = collector.PromotionStale
	CollectorPromotionGated            = collector.PromotionGated
	CollectorPromotionDisabled         = collector.PromotionDisabled
	CollectorPromotionPermissionHidden = collector.PromotionPermissionHidden
	CollectorPromotionUnsupported      = collector.PromotionUnsupported
)

// Collector reducer-readback states.
//
// Deprecated: use the [collector] constants.
const (
	CollectorReadbackAvailable   = collector.ReadbackAvailable
	CollectorReadbackPending     = collector.ReadbackPending
	CollectorReadbackUnavailable = collector.ReadbackUnavailable
)

// Stanza: collector backpressure, fact evidence, generation dead letters.

// CollectorBackpressureSnapshot reports one collector's queue backpressure.
//
// Deprecated: use [collector.BackpressureSnapshot].
type CollectorBackpressureSnapshot = collector.BackpressureSnapshot

// CollectorFactEvidence aggregates one collector instance's fact emission.
//
// Deprecated: use [collector.FactEvidence].
type CollectorFactEvidence = collector.FactEvidence

// CollectorGenerationDeadLetterSnapshot reports pre-queue commit failures.
//
// Deprecated: use [collector.GenerationDeadLetterSnapshot].
type CollectorGenerationDeadLetterSnapshot = collector.GenerationDeadLetterSnapshot

// Stanza: vulnerability source state.

// VulnerabilitySourceState is one durable vulnerability source checkpoint.
//
// Deprecated: use [collector.VulnerabilitySourceState].
type VulnerabilitySourceState = collector.VulnerabilitySourceState

// Stanza: coordinator collector instance summary.

// CollectorInstanceSummary is one configured collector runtime instance.
//
// Deprecated: use [collector.InstanceSummary].
type CollectorInstanceSummary = collector.InstanceSummary
