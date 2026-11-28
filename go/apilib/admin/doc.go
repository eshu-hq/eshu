// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package admin is the proof-read family of the apilib extraction:
// the 7-method ReadStore port with row/filter types, the static tuning
// handler, the work-items query handler, and the per-family mounts
// (Refs eshu-hq/eshu-api#456).
//
// This package is the single definition site for the proof-read types:
// core aliases them (type X = apilibadmin.X), so signatures here are
// wire contracts on both sides. Stdlib only; see the parent AGENTS.md.
package admin
