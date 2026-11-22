// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package audit checks that a query read uses only the stores its
// capability declared in the capability matrix (the answering_stores field,
// #10880 acceptance criterion 1).
//
// Two checks share one classifier. A Recorder is a db.ReadStore that captures
// the exact SQL and bind arguments a production store issues, so a probe can
// drive a store through each gate and grant branch and Classify the result into
// store tokens; Undeclared compares them to the declaration. Census walks the
// non-test Go files under a directory and classifies every SQL string literal
// per package, so a package cannot read an undeclared store by not having a
// probe. The tests live in this package and read the real matrix.
//
// Query text is built at init from package variables and helper calls, and a
// fact kind often arrives only as a bind argument, so the recorded statement is
// the primary evidence and the source census is the floor. A fact_records read
// whose kind cannot be resolved is reported as unresolved rather than dropped.
package audit
