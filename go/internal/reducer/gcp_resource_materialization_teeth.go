//go:build ifadeterminismteeth

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strconv"
	"time"
)

// ifaTeethWriteOrderKey is the single row/property key
// ifaTeethStampCloudResourceRow stamps. It exists ONLY under the
// ifadeterminismteeth build tag (see scripts/verify-ifa-determinism.sh
// --teeth and issue #4396's acceptance clause: "a deliberately
// non-idempotent write is caught"). No normal, CI, or production build ever
// compiles this file — gcp_resource_materialization_teeth_off.go (tag:
// !ifadeterminismteeth) gives ifaTeethStampCloudResourceRow the same
// signature as a pure no-op for every other build, and
// cloud_resource_node_writer_teeth_off.go (same tag, cypher package) drops
// this key from the CloudResource upsert's SET clause entirely, so a stray
// key in the row parameter map has no effect there either.
//
// This is the ONE writer (CloudResourceNodeWriter.WriteCloudResourceNodes,
// the sole canonical writer for gcp_cloud_resource-derived nodes) / ONE
// property (ifa_teeth_write_order) the determinism matrix's --teeth mode
// exercises. See cloud_resource_node_writer_teeth.go
// (go/internal/storage/cypher) for the Cypher-side half of this fault.
const ifaTeethWriteOrderKey = "ifa_teeth_write_order"

// ifaTeethStampCloudResourceRow stamps row with the current wall-clock time
// in nanoseconds under the ifa_teeth_write_order key, unconditionally, for
// every CloudResource row this reducer process builds.
//
// Why wall-clock nanos and not a per-process monotonic sequence number: an
// earlier version of this fault used a process-global atomic counter tied
// to this reducer's own processing order, on the theory that
// `ifa drive -workers N` produces a different commit interleaving into
// fact_work_items for N=1 vs. N=2 vs. N=4, which downstream reducer
// processing would then reflect. Measured against the demo-org GCP
// cassette (testdata/cassettes/gcpcloud/supply-chain-demo.json) that theory
// was FALSE: the cassette has exactly one scope and one generation, so
// `concurrentreplay.Driver` has exactly one work unit for ANY worker count
// to drain — N never changes the commit order because there is only ever
// one commit. All three N=1/N=2/N=4 cells produced the identical canonical
// digest (48f30267f1c0773d137d14c64ae008e7fe0a5a39db481f524ac07d8ddcb09310)
// with the counter-based fault, proving it inert for this fixture rather
// than proving anything about the matrix.
//
// Wall-clock nanoseconds sidestep that: each of the three cells is an
// independent, freshly-built reducer process reading the identical facts,
// and a fresh `time.Now().UnixNano()` value is vanishingly unlikely to
// repeat across separate process invocations, so the stamped property
// differs between cells regardless of whether drive-side commit order also
// differed. This is still the acceptance clause's real, build-tag-gated,
// non-idempotent write reached by the actual runtime pipeline — not a
// cassette-mutation stand-in — but the honest caveat is that the
// divergence it proves is "the matrix catches a non-idempotent write,
// full stop," not specifically "a write that is sensitive to the
// `-workers N` interleaving" for this single-generation fixture. See
// go/internal/reducer/README.md's matching section for the fuller writeup.
func ifaTeethStampCloudResourceRow(row map[string]any) {
	row[ifaTeethWriteOrderKey] = strconv.FormatInt(time.Now().UnixNano(), 10)
}
