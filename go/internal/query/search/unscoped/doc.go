// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package unscoped answers file-content substring searches that carry no
// repository filter, inside a work budget (#7730).
//
// A substring search over every indexed repository has no cost bound in
// PostgreSQL: the planner prices the ILIKE predicate from a histogram of at
// most 1 KB values, so one ANALYZE can flip the same pattern between a 9 ms
// ordered walk and a 3 s trigram bitmap, and a common token's recheck cost is
// the byte mass of its candidate set. This package bounds the work instead of
// trusting the plan. One call is one REPEATABLE READ READ ONLY transaction
// whose first statement is the substring-index readiness check, followed by
// these phases against contiguous, non-overlapping key ranges:
//
//  1. a probe of 200 rows in repo_id, relative_path order, shaped as an
//     ordered primary-key walk that never uses the trigram index;
//  2. continuation steps of 500 rows, capped at 0.15 x the budget;
//  3. a trigram tail inside a savepoint with index scans disabled and a
//     statement timeout of min(remaining budget, 0.5 x budget);
//  4. continuation steps with whatever budget remains.
//
// A window shorter than its size proves the key space is exhausted, so the
// answer is complete. A statement the server cancels advances nothing and is
// rolled back to its savepoint. When the budget ends first, Search returns the
// rows found so far as a querycontract.SearchPartial with a resume cursor and
// the measured elapsed time and overrun; it never returns an error for a
// spent budget and never returns a short page that looks complete.
//
// Every figure scales linearly with the budget (EnvBudgetMS, default 800 ms).
// The package depends on db.ReadTransaction, not on database/sql, and reads
// the clock through Searcher.Now so the budget decisions are testable.
package unscoped
