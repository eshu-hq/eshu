//go:build ifadeterminismteeth

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// teethCloudResourceUpsertExtraSet appends exactly one extra SET clause to
// canonicalCloudResourceUpsertCypher, persisting row.ifa_teeth_write_order
// (stamped by go/internal/reducer's ifaTeethStampCloudResourceRow, the
// same build tag's counterpart) onto the committed CloudResource node as
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
// row.ifa_teeth_write_order is a per-process monotonic sequence number (see
// ifaTeethStampCloudResourceRow) recording the relative order this reducer
// process built each CloudResource row. That order depends on (a) the
// commit interleaving `ifa drive -workers N` produced upstream in
// fact_work_items, and (b) this reducer's own worker pool
// (ESHU_REDUCER_WORKERS) racing to claim and process those rows. Two
// independent pipeline runs over the identical demo-org cassette are not
// guaranteed to assign the same CloudResource node the same
// ifa_teeth_write_order value, so scripts/verify-ifa-determinism.sh
// --teeth's N=1/N=2/N=4 cells diverge on this one property, changing
// `ifa graph-dump`'s canonical digest and failing the matrix — proving the
// matrix actually catches a non-idempotent write rather than passing
// vacuously. See scripts/verify-ifa-determinism.sh's own --teeth doc and
// issue #4396's acceptance clause.
const teethCloudResourceUpsertExtraSet = `,
    r.ifa_teeth_write_order = row.ifa_teeth_write_order`
