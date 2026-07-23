# Ruby Parser

## Purpose

This package owns the Ruby parser adapter used by the parent parser engine. It
parses Ruby source with the tree-sitter-ruby grammar and extracts module and
class declarations, method signatures, require/load imports, module inclusions,
local and instance variables, bounded method-call evidence, parser-backed
dead-code root metadata, exact literal Rails/Sinatra route entries, and Bundler
dependency evidence from `Gemfile` and `Gemfile.lock`. All Ruby source evidence
(modules, classes, singleton classes, methods, imports, inclusions, variables,
block end lines, method calls, and route entries)
comes from the tree-sitter AST. Only the Bundler `Gemfile`/`Gemfile.lock`
manifest path still uses line-oriented scanning, which is appropriate for those
non-Ruby-grammar manifest formats.

## Ownership boundary

The package is responsible for Ruby source scanning, Bundler manifest/lockfile
scanning, and payload bucket population. The parent parser package still owns
registry dispatch, engine orchestration, repo path handling, and parse
telemetry.

## Exported surface

The godoc contract is in doc.go. Current exports are Parse, ParseWithParser,
PreScan, and PreScanWithParser. The `WithParser` variants accept a caller-owned
tree-sitter parser so the parent engine can reuse cached language handles.

## Dependencies

This package imports the Go standard library, internal/parser/shared, the
go-tree-sitter runtime, and the tree-sitter-ruby grammar binding. It must not
import the parent internal/parser package.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

Ruby block structure comes from the AST: module, class, singleton-class, and
method nodes provide `line_number` and `end_line` (the line of the matching
`end`) so downstream containment checks can attach receiverless helper calls to
the enclosing Ruby method before reducer materialization. Class visibility is
tracked only for literal `public`, `private`, and `protected` statements so
public Rails controller actions can be separated from private helpers.
Method-call rows are recovered from tree-sitter `call` nodes during the AST
walk. The dotted full name is composed from the receiver and method nodes:
chained calls such as `original.bind(self).call` emit both `original.bind` and
`original.bind.call` because each link is its own `call` node, and argument
lists are excluded from the composed name. Receiverless command calls
(`log_api_key_restore api_key`), parenthesized calls, and bare lowercase
identifiers on the right side of an assignment (`x = build_scopes`) are all
recorded. Rows are deduplicated by full name and source line so repeated calls
on different lines remain visible. Enclosing context is read from the live AST
scope stack rather than a line index. PreScan sorts names after collecting them
from the parsed function, class, and module buckets.

Constants are represented in the legacy `variables` bucket with class or module
context instead of a separate constants bucket. Predicate, bang, and writer
method suffixes are preserved for qualified calls. Rails controller actions,
literal Rails callback symbols, `method_missing` / `respond_to_missing?`,
literal `method` / `send` / `public_send` symbol targets, and script guard calls
are marked as derived dead-code roots. Script guards (`if __FILE__ == $0`) are
detected on the AST: the grammar parses the guard cleanly as an `if` node with a
`binary` `==` condition, so the receiverless calls and bare identifiers in its
body are read from the tree rather than from a line scan. Other Rails-style DSL
chains are captured as bounded call evidence only. `def self.name` and
`class << self` are covered, while `def ClassName.name` is not part of the
current contract.

Literal Rails routes inside `Rails.application.routes.draw` emit
`framework_semantics.rails.route_entries` only when the HTTP method, path, and
`to: "controller#action"` target are source literals. Literal Sinatra routes
emit `framework_semantics.sinatra.route_entries` only when a Sinatra import or
`Sinatra::Base` subclass is present and the route block is a named
`&method(:handler)` reference. Dynamic paths, dynamic targets, namespaced Rails
controller strings, anonymous Sinatra route blocks, generated route files, and
autoload/eager-load behavior remain outside this parser's exactness boundary;
the reducer may project `HANDLES_ROUTE` only after the emitted handler resolves
to exactly one indexed function.

A `resources`/`resource` Rails routing DSL macro, and an explicit Rails `to:`
target that does not parse into a clean unqualified controller#action (for
example a namespaced `"admin/posts#show"`), are each detected without being
expanded: their presence anywhere inside `Rails.application.routes.draw`
stamps `framework_semantics.rails.has_unmodeled_routes = true` for that file.
This is the #5494 route-liveness ambiguity signal: the reducer's repo-wide
verdict builder treats its presence as proof the repo's exact route surface
cannot be trusted as complete, and keeps every controller action rather than
downgrading one that a macro or a namespaced route actually covers.

## Performance and observability evidence

Performance Evidence: This change moves Ruby method-call and script-guard
extraction from a per-line byte scanner onto the existing single-pass
tree-sitter AST walk that already produced every other Ruby bucket. The walk
visits each `call` node once and reads context from the in-memory scope stack,
removing the second per-line pass over the source that the deleted scanner
performed, so the parser does strictly less per-file work on the same AST.

No-Regression Evidence: The full parser suite plus the Ruby semantics and
dead-code root tests pass unchanged (`go test ./internal/parser/... ./internal/mcp
./internal/query ./internal/reducer -count=1`), confirming the AST call set
matches the prior call set for the `function_calls`, `imports`,
`module_inclusions`, and `dead_code_root_kinds` buckets. Ruby parsing is an
ingester-side, per-file CPU step with no graph write, queue, or lease behavior,
so there is no concurrency or backend contention surface to measure.

No-Observability-Change: This package emits no telemetry; parse timing and spans
remain owned by the parent parser engine, and this change does not add, remove,
or alter any metric, span, log, or status field.

Bundler parsing is intentionally static. Literal `gem` calls in `Gemfile`
emit `variables` rows with `config_kind=dependency`,
`package_manager=rubygems`, version requirement text, group scope, and
git/path source metadata. Dynamic gem names or dynamic version expressions are
skipped instead of resolved. `Gemfile.lock` rows emit exact versions from
`specs:` sections and set `dependency_path`, `dependency_depth`, and
`direct_dependency` only when the lockfile `DEPENDENCIES` section plus
indented `specs:` edges prove the direct or transitive chain. Git and path
Bundler sources preserve `source_type`, `source_path`, and
`source_ambiguous=true` so reducers do not treat forked or local gems as
public RubyGems registry consumption.

## Related docs

- docs/public/languages/support-maturity.md
