// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main wires the Confluence documentation collector binary.
//
// The binary reads bounded Confluence documentation evidence through
// read-only credentials, emits source-neutral documentation facts through
// collector.Service, and commits those facts through the shared Postgres
// ingestion store. Commit failures before projector work exists are recorded
// through the shared collector generation dead-letter sink.
package main
