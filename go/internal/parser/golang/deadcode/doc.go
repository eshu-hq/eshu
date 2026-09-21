// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package deadcode derives dead-code root evidence for a parsed Go file: the
// signal the golang parser attaches to each function, method, interface, and
// struct as "dead_code_root_kinds" so a reachability query downstream can
// tell a genuinely unreferenced declaration from one reached only through
// evidence a syntactic call-graph walk cannot see on its own.
//
// Evidence has two sources. Registration evidence (registrations.go) matches
// explicit framework wiring: a net/http ServeMux/HandleFunc/Handle
// registration and a cobra Command's Run/RunE field, whether assigned
// directly or through a struct literal. RootKinds (roots.go) additionally
// recognizes root-worthy function/method signatures on sight — an
// http.Handler shape, a cobra RunE shape ([]string in, error out), or a
// controller-runtime Reconcile method — and folds in same-package direct
// method call roots (a call whose receiver type this package's
// options.GoDirectMethodCallRoots recorded, resolved during the reducer's
// prior pass over the package). Evidence composes both sources plus the
// semantic evidence from the sibling internal/parser/golang/deadcode/semantic
// package: interface satisfaction, function/method value references, generic
// constraint methods, and dependency-injection callback arguments.
//
// This package never imports internal/parser/golang — that back-edge is the
// import cycle issue #6774 removes. Anything it needs from the old flat
// package lives in internal/parser/golang/symbols; the golang parser's
// Parse (language.go) is deadcode's only caller, via Evidence and RootKinds.
package deadcode
