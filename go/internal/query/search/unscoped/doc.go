// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package unscoped answers file-content and entity-content substring searches
// that carry no repository filter, inside a work budget (#7730).
//
// A substring search over every indexed repository has no cost bound in
// PostgreSQL: the planner prices the ILIKE predicate from a histogram of at
// most 1 KB values, so one ANALYZE can flip the same pattern between a 9 ms
// ordered walk and a 3 s trigram bitmap, and a common token's recheck cost is
// the byte mass of its candidate set. This package bounds the work instead of
// trusting the plan. One call is one REPEATABLE READ READ ONLY transaction
// whose first statement is the substring-index readiness check, followed by
// these phases against contiguous, non-overlapping key ranges. Each shape
// has its own figures, stated at the default budget as content_files /
// content_entities:
//
//  1. a probe of 200 / 1,000 rows in key order (repo_id, relative_path for
//     files; repo_id, relative_path, start_line, entity_id for entities),
//     shaped as an ordered primary-key walk that never uses the trigram index,
//     with the remaining budget as its statement timeout;
//  2. continuation steps of 500 / 5,000 rows, capped at 0.55 / 0.20 x the
//     budget, each with the room left in that cap as its statement timeout;
//  3. a trigram tail inside a savepoint with index scans disabled and a
//     statement timeout of min(remaining budget, 0.50 / 0.80 x budget),
//     skipped when less than the tail floor (375 ms) is left, and skipped
//     when the pattern has no three-character alphanumeric run, because
//     pg_trgm then extracts no trigram and the tail could only run as an
//     unbounded sequential scan; the partial reason states that limit
//     (pattern_has_no_trigram, #8002);
//  4. continuation steps with whatever budget remains.
//
// The file figures come from the #7730 measurements; the entity figures are
// triple A of the #7009 S1 shim on content_entities.
//
// The walk is one engine over a table shape. The engine (walk.go) owns the
// phases, the savepoints, the timeouts, the cursor rule and the partial
// result, and names no table. A shape supplies the statement text, the key
// columns its cursor binds, the row scanner, the span identity, and its
// window sizes and phase figures. There are two shapes: content_files
// (files.go), which Searcher.Search walks, and content_entities
// (entities.go), which Searcher.SearchEntities walks.
//
// A window shorter than its size proves the key space is exhausted, so the
// answer is complete. A statement the server cancels advances nothing and is
// rolled back to its savepoint. When the budget ends first, both routes return the
// rows found so far as a querycontract.SearchPartial with a resume cursor and
// the measured elapsed time and overrun; it never returns an error for a
// spent budget and never returns a short page that looks complete.
//
// Every figure scales linearly with the budget (EnvBudgetMS, default 2000 ms).
// The package depends on db.ReadTransaction, not on database/sql, and reads
// the clock through Searcher.Now so the budget decisions are testable.
package unscoped
