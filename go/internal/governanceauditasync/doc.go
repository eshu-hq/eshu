// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package governanceauditasync provides a best-effort, non-blocking appender
// for hosted governance audit events that must not couple a caller's request
// latency to a durable-store round trip.
//
// AsyncAppender buffers governanceaudit.Event values in a bounded channel and
// flushes them to a durable Appender (the production sink is
// storage/postgres.GovernanceAuditStore) from a single background worker.
// Enqueue never blocks: a full buffer drops the event and increments a
// counter instead of applying backpressure to the caller. See the F-9
// (#5170) design addendum for the load-bearing latency measurements that
// justify this design over a synchronous append.
package governanceauditasync
