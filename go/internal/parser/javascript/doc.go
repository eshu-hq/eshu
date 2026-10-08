// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package javascript parses JavaScript, TypeScript, and TSX source into the
// parser payload consumed by the parent dispatcher.
//
// The package owns tree-sitter extraction, import and re-export rows, component
// evidence, TypeScript declarations, call metadata, structural child_process
// shell-command evidence, dead-code root evidence, tsconfig.json import
// resolution, and package.json public surface modeling.
// Receiver type metadata is limited to local syntax evidence such as
// constructor assignments, typed fields, typed parameters, and simple typed
// function returns.
// JavaScript and TypeScript dead-code helpers live in this package so root
// modeling stays close to import, export, CommonJS default-export class, Hapi,
// framework-route, Fastify route-object handler, constructor function-value,
// TypeScript public-surface, and nearest-package evidence.
// CommonJS class-method roots are limited to the exported class expression.
// Declaration public-surface walks are static, repository-bounded, and
// depth-capped. Package declaration targets ending in .d.ts are mapped back to
// authored TypeScript and JavaScript candidate sources before root checks.
// Shared helper aliases are kept local to the helpers that still need them.
// Callers provide a ParserFactory so runtime grammar caching stays in the
// parent package while this child package stays independent from internal/parser.
// Resolvers accept JSONC TypeScript config files, keep resolution inside the
// repository root, and return repository-relative source paths for
// resolved_source metadata. Parsed tsconfig.json and package.json content is
// memoized per resolved config file path (project/scope_cache.go) so every
// source file sharing the same nearest config reuses one read and one parse
// instead of repeating both per file.
//
// Cross-repository call keys (#7601, package_keys.go): an exported top-level
// function or class carries package_id (the nearest package.json name) and
// export_name; a default export is keyed only in the main or module entry
// file. A real call (function_call, constructor_call, jsx_component) bound to a
// bare package import carries package_export_symbol =
// "package:<source>#<imported name>", but only when a package.json between the
// file and the repository root declares that package in a dependency field
// (project.DeclaredDependencies). An npm alias dependency is keyed under its
// target ("alias": "npm:target@range" keys package:target#name), and an alias
// whose target does not parse stays unkeyed (#7613). An undeclared bare name
// (a bundler alias, a Node.js built-in) stays unkeyed, as do type references,
// type-only imports, subpaths, in-repo imports, deep chains, default-import
// members, and names the file declares again. A jsconfig.json alias resolves
// in-repo like a tsconfig.json one, so it sets resolved_source and stays
// unkeyed even when the name is also declared (#7613).
//
// Three leaf subpackages carry work that does not need the parse lifecycle,
// and none of them may import this package back (issue #6771):
// project resolves a file's tsconfig.json/package.json context from the
// repository layout and owns the stat-keyed config cache; syntax holds the
// AST-level extraction primitives (declared names, docstrings and method
// kinds, type parameters and references, implemented interfaces,
// member-expression decomposition, parameter counts, and the per-parse
// ParentLookup index); jsdataflow owns the CFG and reaching-definitions
// lowering behind Options.EmitDataflow.
//
// When Options.EmitDataflow is set, Parse also emits the opt-in value-flow
// buckets "dataflow_functions", "taint_findings", and "interproc_findings"
// (built by cfg_emit.go over the javascript/jsdataflow lowering and the shared
// internal/parser/dataflowemit renderer, labeled with the output language). The
// gate is off by default and the payload is byte-identical to before this
// feature when off. Shell-command evidence records only API and source location
// metadata; command text, arguments, and environment values are intentionally
// omitted.
//
// The Engine-level black-box regressions that used to live in
// internal/parser as engine_*_test.go now live here as external
// package javascript_test, matching the earlier Elixir relocation (#6335).
// They drive extraction through parser.DefaultEngine().ParsePath, which Go
// compiles separately from this package's own tests, so exercising the
// public engine contract does not give this package's production files a
// reverse dependency on internal/parser.
package javascript
