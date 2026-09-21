// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cleanrooms maps AWS Clean Rooms collaboration, configured-table, and
// membership metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for collaborations, configured
// tables, and memberships plus relationships for the configured-table-to-Glue-
// table backing source and the membership-in-collaboration association.
// Analysis-rule SQL, protected-query bodies and results, allowed-column names,
// and member secrets stay outside this package contract: the scanner is
// metadata-only.
package cleanrooms
