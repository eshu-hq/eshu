// Package ruby parses Ruby source evidence for the parent parser engine using a
// tree-sitter AST.
//
// Parse reads one Ruby source file through shared.ReadSource, parses it with the
// tree-sitter-ruby grammar, and emits the parser payload buckets for modules,
// classes, methods, imports, module inclusions, variables, method calls, block
// end lines, and bounded dead-code root metadata. Modules, classes, singleton
// classes, method definitions, imports, inclusions, and variable assignments are
// recovered from AST nodes; block `end_line` values come from node end
// positions. Method calls are recovered by a byte-level line scan whose context
// (enclosing module, class, or method) is resolved from the AST scope index, so
// the call set stays byte-identical to the prior regex implementation. Gemfile
// and Gemfile.lock inputs use a Bundler-specific path that emits RubyGems
// dependency rows, exact lockfile versions, group scope, and lockfile-proven
// dependency chains while skipping dynamic Ruby. PreScan returns declaration
// names from the same payload path. ParseWithParser and PreScanWithParser let
// the parent engine reuse a caller-owned runtime parser without importing parser
// dispatcher internals. The package keeps constants in the existing variable
// bucket and treats unmodeled framework DSL chains as bounded call evidence, not
// framework-root truth. The package is deterministic and depends only on shared
// parser helpers and the tree-sitter runtime.
package ruby
