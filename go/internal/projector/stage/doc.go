// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package stage holds the per-family projection stages, each of which reduces
// one scope generation's facts to that family's rows and reducer intents:
// files, parsed entities, relationships, and workloads.
//
// Every stage is a pure function from an envelope slice to a result value. A
// stage deduplicates within its own family, skips a fact it does not
// recognize rather than failing the projection, and produces no side effects —
// no writes, no queue, no telemetry. The projector runtime owns invocation
// order and everything downstream of the returned value.
package stage
