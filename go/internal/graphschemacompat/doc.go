// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package graphschemacompat records graph schema markers and checks whether a
// runtime may write to the active graph schema.
//
// The package reads the Postgres graph_schema_applications marker written by
// schema bootstrap and compares the latest backend fingerprint with the
// fingerprint compiled into the current writer. It also writes that marker
// after strict graph DDL or adoption succeeds. It does not inspect the graph
// backend at runtime, so steady-state pods avoid repeated SHOW CONSTRAINTS or
// SHOW INDEXES scans on large retained graph stores.
//
// RequireCompatible answers for a writer that is starting. WriteFence carries
// the same decision onto the write path of a writer that is already running,
// for the case a schema application is recorded underneath it -- a Helm upgrade
// records the new marker from a pre-upgrade hook while the previous pods still
// serve. It caches its decision, so the cost is one indexed marker read per
// interval rather than one per write.
//
// A fence only reaches a writer that asks it. Today that is CanonicalNodeWriter
// in cmd/ingester and cmd/projector; every writer cmd/reducer builds runs
// unfenced, and so does the one cmd/bootstrap-index builds. Deployment ordering
// does not close that gap. It decides when a writer may start, not whether one
// already running stops: under the Helm pre-upgrade hook the schema Job records
// the marker while the outgoing pods still serve at their configured replicas.
// Stopping those writers is a manual step. See WriteFence for the coverage
// list, and AGENTS.md for the reducer writer inventory and the node identities
// it keys on.
package graphschemacompat
