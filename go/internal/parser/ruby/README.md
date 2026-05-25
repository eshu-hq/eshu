# Ruby Parser

## Purpose

This package owns the line-oriented Ruby parser adapter used by the parent
parser engine. It extracts module and class declarations, method signatures,
require/load imports, module inclusions, local and instance variables, bounded
method-call evidence, parser-backed dead-code root metadata, and Bundler
dependency evidence from `Gemfile` and `Gemfile.lock`.

## Ownership boundary

The package is responsible for Ruby source scanning, Bundler manifest/lockfile
scanning, and payload bucket population. The parent parser package still owns
registry dispatch, engine orchestration, repo path handling, and parse
telemetry.

## Exported surface

The godoc contract is in doc.go. Current exports are Parse and PreScan.

## Dependencies

This package imports the Go standard library and internal/parser/shared. It
must not import the parent internal/parser package.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

Ruby block tracking is line-oriented and uses `end` to close the current
module, class, singleton-class, or method context. Closed blocks update
`end_line` metadata so downstream containment checks can attach receiverless
helper calls to the enclosing Ruby method before reducer materialization. Class
visibility is tracked only for literal `public`, `private`, and `protected`
lines so public Rails controller actions can be separated from private helpers.
Method-call rows are deduplicated by full name and source line so repeated calls
on different lines remain visible. PreScan sorts names after collecting them
from the parsed function, class, and module buckets.

Constants are represented in the legacy `variables` bucket with class or module
context instead of a separate constants bucket. Predicate, bang, and writer
method suffixes are preserved for qualified calls. Rails controller actions,
literal Rails callback symbols, `method_missing` / `respond_to_missing?`,
literal `method` / `send` / `public_send` symbol targets, and script guard calls
are marked as derived dead-code roots. Other Rails-style DSL chains are captured
as bounded call evidence only. `def self.name` and `class << self` are covered,
while `def ClassName.name` is not part of the current contract.

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
