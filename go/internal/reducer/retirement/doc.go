// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package retirement holds the Deliverable 2 retirement plan tables
// (#8659): which reducer domain retracts what when a repository retires,
// which Postgres tables each purge step owns, and which partition-lease
// domains the 2b' barrier wait covers.
//
// The tables are closed: the tests fail when a domain, table, or lease
// writer is added without a classification, so new writers cannot slip
// past the retirement path silently. Reasons cite the source check behind
// each row; a row whose reason cannot name one is a guess and does not
// belong here.
package retirement
