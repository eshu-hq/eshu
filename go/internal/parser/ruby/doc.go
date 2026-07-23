// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ruby parses Ruby source evidence for the parent parser engine using a
// tree-sitter AST.
//
// Parse reads one Ruby source file through shared.ReadSource, parses it with the
// tree-sitter-ruby grammar, and emits the parser payload buckets for modules,
// classes, methods, imports, module inclusions, variables, method calls, block
// end lines, exact literal Rails/Sinatra route entries, and bounded dead-code
// root metadata. Modules, classes, singleton classes, method definitions,
// imports, inclusions, route entries, and variable assignments are recovered
// from AST nodes; block `end_line` values come from node end
// positions. Method calls are recovered from tree-sitter `call` nodes during the
// same AST walk: the dotted full name is composed from the call's receiver and
// method nodes (recursing through chained call receivers), and the enclosing
// module, class, or method context is taken from the live scope stack. Bare
// lowercase identifiers on the right side of an assignment are also recorded as
// receiverless calls. Gemfile
// and Gemfile.lock inputs use a Bundler-specific path that emits RubyGems
// dependency rows, exact lockfile versions, group scope, and lockfile-proven
// dependency chains while skipping dynamic Ruby. PreScan returns declaration
// names from the same payload path. ParseWithParser and PreScanWithParser let
// the parent engine reuse a caller-owned runtime parser without importing parser
// dispatcher internals. The package keeps constants in the existing variable
// bucket and treats unmodeled framework DSL chains as bounded call evidence, not
// framework-root truth. The package is deterministic and depends only on shared
// parser helpers and the tree-sitter runtime. Route entries are exact-only:
// Rails requires a literal `to: "controller#action"` route target inside
// `Rails.application.routes.draw`, and Sinatra requires a named
// `&method(:handler)` block on a literal route. The package also detects
// (without expanding) Rails routes it cannot resolve exactly -- a
// `resources`/`resource` DSL macro, or an explicit `to:` target that does not
// parse into a clean unqualified controller#action -- and stamps
// framework_semantics.rails.has_unmodeled_routes so a downstream consumer
// (the #5494 reducer route-liveness join) can treat the repo's route surface
// as ambiguous rather than silently reading a macro-covered action as
// unrouted.
package ruby
