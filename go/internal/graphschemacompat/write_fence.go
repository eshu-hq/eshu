// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphschemacompat

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// DefaultWriteFenceInterval is how long a WriteFence reuses its last decision
// before reading the marker again. It bounds two things at once: how long an
// already-running writer can keep writing after the applied schema stops
// admitting it, and how often each fenced writer re-reads one indexed Postgres
// row. Thirty seconds is short next to a rolling upgrade's pod-replacement
// window and long next to a canonical write batch.
const DefaultWriteFenceInterval = 30 * time.Second

// WriteFence re-checks the applied graph schema marker while a writer is
// already running, instead of only when it starts.
//
// RequireCompatible runs once, at process startup, before the service loop.
// That admits or refuses a writer that is asking to start, and nothing after
// it looks at the marker again -- so a writer that was admitted keeps writing
// across a schema change recorded underneath it. During a Helm upgrade the
// schema-bootstrap Job is a pre-upgrade hook, so it records the new marker
// while the previous generation of ingester, projector, and reducer pods is
// still serving. The same gap is open in the other direction on a rollback,
// where an older bootstrap re-records an older marker under pods from the newer
// release.
//
// A fence closes that on the write path: a canonical write asks the fence
// first, and a writer the applied schema no longer admits fails instead of
// writing.
//
// How far that reaches is narrower than "canonical writers", and the next
// identity cutover has to check rather than assume. Only CanonicalNodeWriter
// takes a fence (CanonicalNodeWriter.WithSchemaWriteFence), and only cmd/ingester
// and cmd/projector wire one; cmd/bootstrap-index deliberately does not, being a
// one-shot seeder. Every writer cmd/reducer builds runs unfenced -- EdgeWriter
// (built twice), SemanticEntityWriter, SecretsIAMGraphWriter, the OrphanSweepStore,
// every field of cmd/reducer's canonicalGraphWriters struct, which is wider than
// the cloud/Kubernetes/IAM writers it is often described as, and the two
// reducer-native materializers WorkloadMaterializer and
// InfrastructurePlatformMaterializer, which live in internal/reducer rather than
// internal/storage/cypher and so are missed by any inventory search keyed on the
// writer package. AGENTS.md carries the constructor-level inventory and the
// command that regenerates it. A marker recorded under a running reducer stops
// none of them.
//
// Deployment ordering is not a substitute. It gates when a writer may start,
// not whether one already running stops: the Helm schema Job is a pre-upgrade
// hook, so it records the marker before the chart rolls the resolution-engine
// Deployment, whose pods keep serving at their configured replica count until
// the rolling update replaces them, and Compose's depends_on gate is likewise a
// start condition. Stopping those writers is the manual procedure in
// docs/public/deployment/service-runtimes-bootstrap.md.
//
// The #6102 Module cutover is unaffected, because no unfenced writer keys a
// Module node on name: the canonical `MERGE (m:Module {name, lang})` belongs to
// CanonicalNodeWriter, the reducer's only Module upsert MERGEs on uid, and the
// orphan sweep already keys Module on (name, lang). Other labels are less
// lucky. Unfenced reducer writers MERGE nine labels on a key that is not uid --
// Repository, EvidenceArtifact, CloudAction, Workload, WorkloadInstance,
// Platform, and Endpoint on id, CodeownerTeam on ref, and Environment on name --
// and a cutover on any of those lands in the gap, with those pods checked at
// startup and writing through the whole rollout. Repository is the sharpest:
// the fenced CanonicalNodeWriter MERGEs it on the same id key the unfenced
// EdgeWriter does, so that cutover is fenced on one side of a rollout and not
// the other. The last four come from the two materializers named above.
//
// Two gaps stay open regardless. A writer from a release that predates the fence
// contains no call to it, and an unfenced writer has none to make. Only stopping
// those pods before bootstrap records the marker closes either.
//
// Decisions are cached for interval, so the cost is one indexed Postgres read
// per fence per interval rather than one per write. The mutex is held across
// that read on purpose: concurrent writers coalesce onto the single in-flight
// check rather than each issuing their own.
type WriteFence struct {
	db       postgres.Queryer
	backend  graph.SchemaBackend
	interval time.Duration
	now      func() time.Time

	mu        sync.Mutex
	checkedAt time.Time
	refusal   error
}

// NewWriteFence returns a fence over backend's marker. An interval at or below
// zero falls back to DefaultWriteFenceInterval.
func NewWriteFence(db postgres.Queryer, backend graph.SchemaBackend, interval time.Duration) *WriteFence {
	if interval <= 0 {
		interval = DefaultWriteFenceInterval
	}
	return &WriteFence{db: db, backend: backend, interval: interval}
}

// NewWriteFenceForRuntime builds a fence for the backend selected by
// ESHU_GRAPH_BACKEND. It returns a nil fence, and no error, for the runtime
// profiles that have no graph schema marker to check at all -- the same
// profiles RequireCompatibleForRuntime skips. A nil fence admits every write,
// so callers can wire the result unconditionally.
func NewWriteFenceForRuntime(db postgres.Queryer, getenv func(string) string) (*WriteFence, error) {
	if graphCompatibilityDisabled(getenv) {
		return nil, nil
	}
	runtimeBackend, err := runtimecfg.LoadGraphBackend(getenv)
	if err != nil {
		return nil, err
	}
	backend, err := schemaBackendForRuntime(runtimeBackend)
	if err != nil {
		return nil, err
	}
	return NewWriteFence(db, backend, DefaultWriteFenceInterval), nil
}

// Check reports whether this writer may still write. It returns nil when the
// applied schema admits the writer, and the ErrIncompatible error from
// RequireCompatible when it does not.
//
// A marker that cannot be read -- Postgres unreachable, the row gone -- is not
// a refusal. The startup check already admitted this writer, so an unreadable
// marker holds the previous decision rather than turning one database blip into
// a simultaneous graph-write outage across every fenced writer. Only a marker that is
// readable and says no stops a write.
func (f *WriteFence) Check(ctx context.Context) error {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	now := f.clock()
	if !f.checkedAt.IsZero() && now.Sub(f.checkedAt) < f.interval {
		return f.refusal
	}

	_, err := RequireCompatible(ctx, f.db, f.backend)
	// checkedAt advances even when the read failed, so an unreachable Postgres
	// costs one query per interval rather than one per write.
	f.checkedAt = now
	switch {
	case err == nil:
		f.refusal = nil
	case errors.Is(err, ErrIncompatible):
		f.refusal = err
	}
	return f.refusal
}

func (f *WriteFence) clock() time.Time {
	if f.now != nil {
		return f.now()
	}
	return time.Now()
}
