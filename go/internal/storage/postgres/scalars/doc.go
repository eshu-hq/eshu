// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package scalars holds the shared null/blank value shaping every Postgres
// store family uses: blank-string rejection before touching the database,
// zero/nil time mapping to nullable database values, seconds-to-duration
// conversion, JSON payload map widening, and string-set normalization.
//
// The helpers live here instead of the db contract leaf so db stays
// interfaces-only per go/internal/storage/postgres/db/AGENTS.md, and instead
// of any one store family so the tenant leaf (which must not import the
// parent postgres package) shares one implementation with the families still
// in the root. Standard library only; no SQL text, no I/O.
package scalars
