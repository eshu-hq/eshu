// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package parity is the arbiter-ordered differential conformance harness
// for the workflow extraction (#8875, ruling 6475436579).
//
// One scenario suite runs parameterized by Backend: the in-tree
// ControlStore on an isolated Postgres schema, and the satellite claim
// plane (exact flip image or a dark deployment) on its own isolated database
// through the in-tree ClaimPlaneClient. Both legs seed identical fixtures
// with deterministic IDs and one captured clock per scenario. The suite
// compares return values, error codes, and read-plane-observable state.
//
// Non-negotiables: isolated databases only (never shared tables), no live
// claim replay on a dark deployment (synthetic run and scope IDs plus cleanup), and
// no dual authority at any timestamp — the satellite leg only runs while
// the deployment under test is dark or a local container.
package parity
