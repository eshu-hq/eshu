//go:build ifadeterminismteeth

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// teethCloudResourceUpsertExtraSet appends two extra SET clauses to
// canonicalCloudResourceUpsertCypher, persisting row.ifa_teeth_seq and
// row.ifa_teeth_write_order (both stamped by go/internal/reducer's
// ifaTeethStampCloudResourceRow, the same build tag's counterpart) onto the
// committed CloudResource node as r.ifa_teeth_seq and
// r.ifa_teeth_write_order.
//
// This file compiles ONLY under `go build -tags ifadeterminismteeth`; every
// normal, CI, and production build instead links
// cloud_resource_node_writer_teeth_off.go, whose
// teethCloudResourceUpsertExtraSet is the empty string, so this
// deliberately non-idempotent write does not exist outside that one
// opt-in build.
//
// Why this is the acceptance clause's "deliberately non-idempotent write":
// row.ifa_teeth_seq is a process-global monotonic sequence number (see
// ifaTeethSequenceCounter) recording the relative order this reducer
// process built each CloudResource row. That order depends on (a) the
// commit interleaving `ifa drive -workers N` produced upstream in
// fact_work_items, and (b) this reducer's own worker pool
// (ESHU_REDUCER_WORKERS) racing to claim and process those rows. On the
// single-scope demo-org cassette alone this signal was measured INERT
// (concurrentreplay.Driver has exactly one work unit for any N, so the
// commit order never varies) — issue #4396 slice 6b's synthetic multi-scope
// cassette (go/internal/synth/gcp.GenerateMultiScope, driven in by
// scripts/verify-ifa-determinism.sh's `ifa synth-cassette` step) gives the
// driver K+1 genuinely independent work units, so this counter is now a real
// interleaving-sensitive signal rather than an inert one. row.ifa_teeth_
// write_order (wall-clock nanoseconds) stays wired unconditionally alongside
// it as the guaranteed-red floor: two independent pipeline runs over the
// identical cassette set are not guaranteed to assign the same
// CloudResource node the same value for either property, so
// scripts/verify-ifa-determinism.sh --teeth's N=1/N=2/N=4 cells diverge on
// at least one of them, changing `ifa graph-dump`'s canonical digest and
// failing the matrix — proving the matrix actually catches a non-idempotent
// write rather than passing vacuously. See
// scripts/verify-ifa-determinism.sh's own --teeth doc and issue #4396's
// acceptance clause.
const teethCloudResourceUpsertExtraSet = `,
    r.ifa_teeth_seq = row.ifa_teeth_seq,
    r.ifa_teeth_write_order = row.ifa_teeth_write_order`
