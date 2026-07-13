// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package schedulereplay is the Layer 3 (ordering) schedule replay for the
// deterministic replay framework (design doc 4102, R-13 / #4122).
//
// It drives recorded projection work through the real reducer service loop using
// a deterministic in-memory work source (ScheduledWorkSource) that delivers
// intents in a scripted order — in-order, adversarial reverse, rotated, and
// duplicate delivery — in place of the production FOR UPDATE SKIP LOCKED Postgres
// claim path. Each delivery order drains into an in-memory canonical graph; the
// gate asserts the converged Canonical snapshot is byte-identical across every
// order, proving the projection's final graph truth is delivery-order
// independent (the offline, credential-free analog of the B-12 snapshot).
//
// Work items come from the committed offline-tier cassette through the real
// cassette -> offlinetier materialization seam, so the inputs are recorded facts,
// not synthetic toys. Items reference shared node keys (a child directory's edge
// points at its parent's node), so reordering exercises the #4019
// child-before-parent conflict-key class rather than independent inserts. A
// deliberately order-sensitive applier is used in tests to prove the gate detects
// ordering bugs.
//
// A second family of scenarios (workitem_projection.go,
// projection_ordering_scenario_test.go, C-14 #4367) proves the same
// order-invariance property for the two reducer projections that are
// shared-conflict-key: reducer_domain values written by more than one distinct
// projection_hook in specs/fact-kind-registry.v1.yaml
// (incident_repository_correlation, supply_chain_impact). Their work items come
// from dedicated committed cassettes under testdata/cassettes/replayschedule/
// through a cassette -> projection work-item seam (there is no offlinetier
// materializer for these domains), and Config.Domain schedules the intents
// under the real reducer.Domain constant so the scenario proves the actual
// projection's shared conflict key, not a placeholder domain.
//
// The package requires no Postgres and no graph backend, so the ordering gate
// runs in the default `go test` pass on every PR.
package schedulereplay
