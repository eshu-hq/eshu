// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codetopicparallel runs bounded code-topic content probes under one
// exported PostgreSQL snapshot and assembles their ordered page in PostgreSQL.
//
// Callers supply repository and language predicates before the per-term LIMIT.
// The package preserves nullable probe columns in JSON and delegates text
// grouping, distinct-term scoring, cap detection, and page ordering to SQL.
// Any probe or assembly error cancels sibling reads and rolls back all sessions.
package codetopicparallel
