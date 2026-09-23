// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"sync"
)

// WriteCounters carries one executed write statement's Bolt summary
// counters: graph objects the statement created, deleted, or relabeled.
// Unlike RecordReconciliationDriftRetractions (reconciliation_drift_metrics.go),
// which publishes retract deletes to OTEL, these counters travel to the
// differential capture recorder (issue #6783), not to telemetry. Backends
// that do not report a counter leave it zero.
type WriteCounters struct {
	NodesCreated         int64
	NodesDeleted         int64
	RelationshipsCreated int64
	RelationshipsDeleted int64
	PropertiesSet        int64
	LabelsAdded          int64
	LabelsRemoved        int64
}

// Add returns the counter-wise sum: retry and drain-loop re-executions of
// one statement report every attempt, so the recorder join sums rather than
// dropping surplus entries.
func (c WriteCounters) Add(other WriteCounters) WriteCounters {
	return WriteCounters{
		NodesCreated:         c.NodesCreated + other.NodesCreated,
		NodesDeleted:         c.NodesDeleted + other.NodesDeleted,
		RelationshipsCreated: c.RelationshipsCreated + other.RelationshipsCreated,
		RelationshipsDeleted: c.RelationshipsDeleted + other.RelationshipsDeleted,
		PropertiesSet:        c.PropertiesSet + other.PropertiesSet,
		LabelsAdded:          c.LabelsAdded + other.LabelsAdded,
		LabelsRemoved:        c.LabelsRemoved + other.LabelsRemoved,
	}
}

// WriteCountEntry is one Bolt-reported write execution: the statement text
// and bound parameters that identify it plus its summary counters.
type WriteCountEntry struct {
	Cypher     string
	Parameters map[string]any
	Counters   WriteCounters
}

// WriteCountsCollector gathers write-count entries in arrival order. It is
// safe for concurrent use: entity-phase concurrent chunks share one call's
// collector through the context. Entries snapshots copy, so joining never
// observes a torn list.
type WriteCountsCollector struct {
	mutex   sync.Mutex
	entries []WriteCountEntry
}

// NewWriteCountsCollector returns an empty collector.
func NewWriteCountsCollector() *WriteCountsCollector {
	return &WriteCountsCollector{}
}

// Add appends one entry.
func (c *WriteCountsCollector) Add(entry WriteCountEntry) {
	if c == nil {
		return
	}
	c.mutex.Lock()
	c.entries = append(c.entries, entry)
	c.mutex.Unlock()
}

// Entries returns a copy of the entries in arrival order.
func (c *WriteCountsCollector) Entries() []WriteCountEntry {
	if c == nil {
		return nil
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	out := make([]WriteCountEntry, len(c.entries))
	copy(out, c.entries)
	return out
}

// writeCountsContextKey carries one recorder call's collector. An unexported
// struct key keeps unrelated context values from colliding with it.
type writeCountsContextKey struct{}

// WithWriteCountsCollector stashes the collector for one recorder call in
// the context. Bolt seams below read it back; every other layer passes the
// context through untouched.
func WithWriteCountsCollector(ctx context.Context, collector *WriteCountsCollector) context.Context {
	return context.WithValue(ctx, writeCountsContextKey{}, collector)
}

// WriteCountsCollectorFromContext returns the stashed collector, or nil
// when capture is off or the call predates it.
func WriteCountsCollectorFromContext(ctx context.Context) *WriteCountsCollector {
	collector, _ := ctx.Value(writeCountsContextKey{}).(*WriteCountsCollector)
	return collector
}

// ReportWriteCounts appends one entry to the call's collector. It is a
// no-op without a stashed collector, so production Bolt seams call it
// unconditionally: uncaptured runs pay one context lookup per statement.
func ReportWriteCounts(ctx context.Context, cypher string, params map[string]any, counters WriteCounters) {
	WriteCountsCollectorFromContext(ctx).Add(WriteCountEntry{Cypher: cypher, Parameters: params, Counters: counters})
}

// WriteSummaryCounters is the Bolt summary-counter surface
// WriteCountersFromSummary reads. It matches the driver's Counters shape
// for the graph-object counters, so seams pass summary.Counters() directly;
// the test fake implements it without a driver.
type WriteSummaryCounters interface {
	NodesCreated() int
	NodesDeleted() int
	RelationshipsCreated() int
	RelationshipsDeleted() int
	PropertiesSet() int
	LabelsAdded() int
	LabelsRemoved() int
}

// WriteCountersFromSummary converts one Bolt summary's graph-object
// counters to WriteCounters. Schema counters (indexes, constraints) stay
// out: production writers never change schema, so they carry no statement
// truth.
func WriteCountersFromSummary(counters WriteSummaryCounters) WriteCounters {
	return WriteCounters{
		NodesCreated:         int64(counters.NodesCreated()),
		NodesDeleted:         int64(counters.NodesDeleted()),
		RelationshipsCreated: int64(counters.RelationshipsCreated()),
		RelationshipsDeleted: int64(counters.RelationshipsDeleted()),
		PropertiesSet:        int64(counters.PropertiesSet()),
		LabelsAdded:          int64(counters.LabelsAdded()),
		LabelsRemoved:        int64(counters.LabelsRemoved()),
	}
}
