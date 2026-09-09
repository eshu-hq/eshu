// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package sql holds the incident-context Postgres query text (Issue #6060,
// lane B S2): the shared active-generation fact-row projection and every
// bounded list query the incident/store/ reads issue. The text moved here
// verbatim from the query root's incident_context_*_sql.go files; only the
// constant names lost their incident_ prefix. The package imports nothing:
// it is pure query text the store package composes with its filters.
package sql
