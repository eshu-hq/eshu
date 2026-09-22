// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package prescan provides the cheap, pre-parse Go evidence collection the
// collector's package-level prescan pass runs before the real per-file
// parse: symbol names for the import map (PreScan), and the per-file
// interface/method-call evidence a package-level pass aggregates across
// every file in a package before feeding it back into per-file parse
// options (PreScanFileEvidence and the individual evidence functions in
// package_interface.go).
//
// PreScan collects function, struct, and interface names directly from the
// tree-sitter tree without running dead-code root evidence or other
// Parse-only payload work; a package-level import-map prescan runs once per
// file before the real parse, so it must stay cheap (issue #161).
//
// PreScanFileEvidence reads, parses, and walks a file once to populate every
// evidence type internal/parser's PreScanGoPackageSemanticRoots loop needs:
// imported interface parameter contracts (both all-function and
// exported-only), imported direct method call roots, local interface method
// returns, local interface methods, generic constraint interface names, and
// method declaration keys. It replaces seven separate read+parse+walk
// passes with one. The individual evidence functions in package_interface.go
// (ImportedInterfaceParamMethods, ExportedInterfaceParamMethods,
// ImportedDirectMethodCallRoots, ImportedDirectMethodCallRootsWithInterfaceReturns,
// LocalInterfaceImportedMethodReturns, LocalInterfaceMethods,
// GenericConstraintInterfaceNames, MethodDeclarationKeys) remain as the
// single-evidence public API PreScanFileEvidence's per-file walk mirrors.
//
// This package never imports internal/parser/golang — that back-edge is the
// import cycle issue #6774 removes. Anything it needs from the old flat
// package lives in internal/parser/golang/symbols. The golang root package
// (internal/parser/golang) keeps thin compatibility forwarders
// (compat_prescan.go) for the symbols internal/parser/go_language.go and
// internal/parser/go_package_interface_prescan.go still call, so this move
// requires no change to internal/parser/*.go.
package prescan
