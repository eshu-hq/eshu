// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifadeterminismteeth

package reducer

import (
	"strconv"
	"sync/atomic"
	"time"
)

// ifaTeethWriteOrderKey is the wall-clock row/property key
// ifaTeethStampCloudResourceRow stamps. It exists ONLY under the
// ifadeterminismteeth build tag (see scripts/verify-ifa-determinism.sh
// --teeth and issue #4396's acceptance clause: "a deliberately
// non-idempotent write is caught"). No normal, CI, or production build ever
// compiles this file — gcp_resource_materialization_teeth_off.go (tag:
// !ifadeterminismteeth) gives ifaTeethStampCloudResourceRow the same
// signature as a pure no-op for every other build, and
// cloud_resource_node_writer_teeth_off.go (same tag, cypher package) drops
// both this key and ifaTeethSequenceKey from the CloudResource upsert's SET
// clause entirely, so stray keys in the row parameter map have no effect
// there either.
//
// This is the ONE writer (CloudResourceNodeWriter.WriteCloudResourceNodes,
// the sole canonical writer for gcp_cloud_resource-derived nodes) the
// determinism matrix's --teeth mode exercises, now stamping TWO properties.
// See cloud_resource_node_writer_teeth.go
// (go/internal/storage/cypher) for the Cypher-side half of this fault.
const ifaTeethWriteOrderKey = "ifa_teeth_write_order"

// ifaTeethSequenceKey is the process-global monotonic sequence-number
// row/property key ifaTeethStampCloudResourceRow stamps alongside
// ifaTeethWriteOrderKey (issue #4396 slice 6b). See ifaTeethSequenceCounter's
// doc for why this counter was reintroduced after an earlier version proved
// it inert on the single-scope demo-org cassette.
const ifaTeethSequenceKey = "ifa_teeth_seq"

// ifaTeethSequenceCounter is a process-global monotonic sequence number,
// incremented once per CloudResource row this reducer process builds. It
// records this reducer process's own relative processing order across every
// gcp_cloud_resource fact it handles in this run.
//
// History: an earlier version of this fault used ONLY this counter (no
// wall-clock companion), on the theory that `ifa drive -workers N` produces a
// different commit interleaving into fact_work_items for N=1 vs. N=2 vs.
// N=4, which downstream reducer processing would then reflect. Measured
// against the demo-org GCP cassette
// (testdata/cassettes/gcpcloud/supply-chain-demo.json) that theory was
// FALSE: the cassette has exactly one scope and one generation, so
// concurrentreplay.Driver has exactly one work unit for ANY worker count to
// drain — N never changes the commit order because there is only ever one
// commit. All three N=1/N=2/N=4 cells produced the identical canonical
// digest (48f30267f1c0773d137d14c64ae008e7fe0a5a39db481f524ac07d8ddcb09310)
// with the counter-based fault alone, proving it inert for that fixture.
//
// Issue #4396 slice 6b fixes the ROOT problem the counter's inertness
// exposed — not the counter itself: scripts/verify-ifa-determinism.sh --teeth
// now also drives a synthetic multi-scope cassette
// (go/internal/synth/gcp.GenerateMultiScope via `ifa synth-cassette`) with K
// disjoint GCP project scopes into the same cell, alongside the unmodified
// demo-org cassette. With K+1 genuinely independent work units,
// concurrentreplay.Driver's commit interleaving into fact_work_items DOES
// vary with -workers N, and this reducer's own worker pool
// (ESHU_REDUCER_WORKERS) races to claim and process those rows in an order
// that also varies run to run — so this counter is no longer inert: it is
// re-added as the fault's more diagnostic signal, sensitive specifically to
// -workers N interleaving (not merely to being a fresh process), while
// ifaTeethWriteOrderKey's wall-clock nanoseconds remain the fault's
// guaranteed-red FLOOR: even if some future fixture change made this counter
// inert again, two independent process invocations reading
// time.Now().UnixNano() are vanishingly unlikely to collide, so --teeth can
// never flake green. See go/internal/reducer/README.md's matching section
// for the fuller writeup of both properties' roles.
var ifaTeethSequenceCounter atomic.Int64

// ifaTeethStampCloudResourceRow stamps row with two build-tag-gated debug
// properties for every CloudResource row this reducer process builds:
// ifaTeethSequenceKey (this process's monotonic per-row sequence number,
// interleaving-sensitive on a multi-work-unit fixture) and
// ifaTeethWriteOrderKey (wall-clock nanoseconds, the guaranteed-red floor
// that stays sensitive to "a fresh process ran" even if the sequence number
// were ever inert again).
func ifaTeethStampCloudResourceRow(row map[string]any) {
	row[ifaTeethSequenceKey] = strconv.FormatInt(ifaTeethSequenceCounter.Add(1), 10)
	row[ifaTeethWriteOrderKey] = strconv.FormatInt(time.Now().UnixNano(), 10)
}
