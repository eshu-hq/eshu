// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package graph holds the shared graph-read test doubles and Cypher-shape
// guards for internal/query and its handler-family subpackages.
//
// It is the graph half of testutil, nested under it for #6642. The
// parent's contract applies unchanged: every helper lives in an ordinary
// non-test file so other packages' tests can import it, nothing here is
// production behavior, and no non-test file under internal/query may import
// this package. internal/queryplan's callsite inventory enforces that last rule
// for this leaf the same way it does for the parent.
//
// A fake here must not call Run or RunSingle. The inventory walks this
// directory like any other, so such a call is an unregistered production query
// callsite and fails the gate. FakeGraphReader routes both methods through an
// unexported helper; FakeRepoGraphReader and FakeWorkloadGraphReader inline
// their dispatch in each method. What a new fake must not do is have one of
// the two methods call the other.
//
// FakeRepoGraphReader and FakeWorkloadGraphReader look alike but are separate
// types on purpose: only the repository fake falls back to a sole registered
// row for the narrow single-repository lookup. Merging them would hand every
// workload test that fallback.
//
// EvaluatingRepositoryGraph evaluates the repository-grant predicates a scoped
// read carries against seeded rows (GrantSeed), so a grant test sees what the
// predicate admits rather than what a fixture returns. The SqlBlastRadius
// helpers are the ordered cleanup and read-back probes for the blast-radius
// live fixtures.
//
// nornicdb_guards.go holds the two NornicDB v1.3.3 Cypher-shape guards (#6786):
// AssertCypherHasNoBrokenAndOr (X4, an AND/OR led by a newline or tab) and
// AssertCypherHasNoIgnoredLabelPredicate (X11, a label predicate in a clause
// position the backend ignores). Call them on the exact Cypher a test's graph
// double captured. TestProductionCypherHasNoIgnoredLabelPredicate also scans
// every production Cypher literal under go/internal and go/cmd for X11.
package graph
