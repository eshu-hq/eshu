# PHP Parser

## Purpose

This package owns the tree-sitter PHP parser adapter used by the parent parser
engine. It extracts namespace metadata, imports, classes, interfaces, traits,
functions, variables, call rows, receiver inference, anonymous classes, and
trait-use adaptation evidence by walking the tree-sitter AST.

## Ownership boundary

The package is responsible for PHP source parsing and payload bucket
population. The parent parser package still owns registry dispatch, engine
orchestration, repo path handling, tree-sitter runtime/grammar caching, and
parse telemetry. `Engine.parsePHP` obtains a `php` parser from the shared
runtime and passes it to `Parse`.
The parser also emits exact `framework_semantics.symfony.route_entries` for
method-level attributes resolved to Symfony `Route` whose path and HTTP methods
are literal, and bounded `dead_code_root_kinds` for PHP entrypoints,
constructors, known magic methods, same-file interface and trait methods,
route-backed controller actions, literal route handlers, Symfony route
attributes, and WordPress hook callbacks.

## Exported surface

The godoc contract is in doc.go. Current exports are `Parse` and `PreScan`,
both of which take a `*tree_sitter.Parser` configured for the PHP grammar.

## Dependencies

This package imports the Go standard library, `internal/parser/shared`, and
`github.com/tree-sitter/go-tree-sitter`. The grammar is
`github.com/tree-sitter/tree-sitter-php` (LanguagePHP), wired into the parent
runtime loader. This package must not import the parent `internal/parser`
package.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

PHP parsing is AST-driven. Declarations resolve `name`, `base_clause`,
`class_interface_clause`, and trait `use_declaration` nodes; properties and
parameters resolve declared type nodes (`named_type`, `optional_type`,
`union_type`, `primitive_type`); calls resolve `member_call_expression`,
`nullsafe_member_call_expression`, `scoped_call_expression`,
`object_creation_expression`, and `function_call_expression`. Receiver chains
are reconstructed from receiver node source text, with nullsafe `?->`
normalized to `->` and the final `->method` rendered as `.method` in
`full_name`. Call rows are deduplicated by full name and source line so repeated
calls on different lines remain visible. PreScan sorts names after collecting
them from the parsed function, class, trait, and interface buckets. Type
inference resolves receivers through a two-pass walk so declarations later in
the file still inform earlier call sites. Symfony route entries stay bounded to
source-proven method attributes whose imported, aliased, or fully qualified name
resolves to Symfony `Route` with literal path and method arguments; dynamic
attributes, Composer/autoload breadth, reflection, and dynamic dispatch remain
query-layer exactness blockers.

## Related docs

- docs/public/languages/support-maturity.md
