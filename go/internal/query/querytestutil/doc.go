// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package querytestutil holds test helpers shared by internal/query and its
// handler-family subpackages.
//
// The helpers live in ordinary (non-_test.go) files on purpose. A symbol
// declared in a _test.go file is not part of the importable package, so no
// export, alias, or forwarder can reach it from another package's tests. That
// constraint is what forces this package to exist: as internal/query splits
// into handler-family subpackages (#6060), each family's tests need the same
// helpers, and they cannot reach root's _test.go declarations at all.
//
// This package is intended for tests only. No production file imports it today,
// and while that holds the linker drops it from production binaries — a
// consequence of the invariant, not a guarantee independent of it. A production
// import would pull testing into a shipped binary and should be treated as a
// defect rather than documented as behavior. internal/queryplan enforces that
// direction: a non-test file under internal/query importing this package fails
// the production query-callsite inventory.
//
// A fixture whose signature names a handler-family type cannot live here: this
// package must not import a family, and a family's in-package tests import this
// package, so that direction cycles. The consuming package declares its own
// double for those. See AGENTS.md.
//
// A fake here must not call Run or RunSingle. That inventory walks this
// directory like any other, so such a call is an unregistered production query
// callsite and fails the gate. There is more than one way to satisfy that:
// FakeGraphReader routes both methods through an unexported helper, while
// FakeRepoGraphReader and FakeWorkloadGraphReader inline their dispatch in each
// method. Either is fine. What a new fake must not do is have one of the two
// methods call the other, or ask for an exemption (#6060, epic #6053).
// callsite and fails the gate. FakeGraphReader satisfies both methods by
// routing each through an unexported helper instead of having one call the
// other; give a new fake the same shape rather than asking for an exemption
// (#6060, epic #6053).
//
// Fakes here may depend on the leaf packages whose types they stand in for --
// FakeGovernanceAuditAppender on internal/governanceaudit and
// FakeScopedTokenResolver on queryauth. Root internal/query and the handler
// families are off limits: root imports the families for its compatibility
// aliases, so importing either from here is an import cycle in the test binary
// of any package whose tests import this one. This package still compiles on
// its own, which is why the rule is worth stating rather than left to the
// compiler.
package querytestutil
