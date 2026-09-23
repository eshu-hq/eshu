// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package code holds the dead-code contract types the code family shares.
//
// A dead-code answer is assembled by several packages that cannot import each
// other: the content readers in package query, the scan and cross-repo filter
// in query/codequery/deadcode, the exposure path in query/impact, and the
// shared test double in query/querytestutil. Each of them needs the same
// shapes. Declaring them once here is what keeps a single definition; a second
// literal elsewhere would compile and then drift silently, changing either
// what the scan checks or what the API advertises with nothing failing.
//
// Two questions decide whether a symbol is dead, and both are represented
// here. DeadCodeIncomingEdge answers "does anything reach it",
// and the cross-repo consumer types answer "does anything outside the
// caller's grant reach it". The second is the half a grant-bound page cannot
// see, and losing it would mark a live symbol dead, so
// CrossRepoDeadCodeConsumerReads carries the page bound and the probe grant
// separately rather than letting one stand in for the other.
//
// The package depends on nothing in its parent and on no other Eshu package:
// its only import is the standard library's strings. The parent does not
// depend on it either, which is what let it move out of querycontract
// unchanged.
//
// Identifiers keep their DeadCode prefix rather than dropping it to match the
// package name. The prefix is not a stutter here: "dead code" is one term,
// and this package is the contract surface for the whole code family, so the
// root code_seam.go surface that joins it later (code topics, hardcoded
// secrets, symbol search) needs the dead-code subset to stay marked.
package code
