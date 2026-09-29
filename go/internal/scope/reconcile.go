// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scope

import "time"

// FullReconcileState summarizes one scope's full (non-delta) generations for
// the periodic reconciliation sweep that re-observes a scope to retract drift
// the delta path could not (#7288).
//
// The sweep needs two separate facts. The reconcile obligation is measured on
// the last full generation that actually reached the graph (activated), so a
// full generation that was superseded while still pending does not satisfy it.
// The throttle is measured on the latest full attempt of any status, so a full
// generation still in flight, or one that recently failed, holds the sweep off
// instead of stacking another full snapshot of the same commit.
type FullReconcileState struct {
	// HasProjectedFull reports whether any full generation was activated.
	HasProjectedFull bool
	// LastProjectedFullAt is the ingest time of the newest activated full
	// generation. It is zero when HasProjectedFull is false.
	LastProjectedFullAt time.Time
	// HasLatestFull reports whether the scope has any full generation.
	HasLatestFull bool
	// LatestFullAt is the ingest time of the newest full generation of any
	// status. It is zero when HasLatestFull is false.
	LatestFullAt time.Time
	// LatestFullStatus is the lifecycle status of that newest full generation.
	LatestFullStatus GenerationStatus
	// LatestFullProjected reports whether that newest full generation was
	// activated. A superseded generation with no activation never projected.
	LatestFullProjected bool
}

// UncoveredProjectionWriter is a generation that started writing the canonical
// graph and never activated, and that no later activated full generation has
// covered (#7389). Its overlay may still be in the graph, so the git collector
// forces the scope's next sync to a full snapshot instead of a delta.
type UncoveredProjectionWriter struct {
	// GenerationID identifies the generation.
	GenerationID string
	// Status is superseded or failed.
	Status GenerationStatus
	// FailureClass is the projector work row's failure_class, or empty.
	FailureClass string
	// ProjectionWriteStartedAt is its latest projection write start.
	ProjectionWriteStartedAt time.Time
}
