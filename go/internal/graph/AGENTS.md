# internal/graph Agent Instructions

These rules are mandatory for this package. Root `AGENTS.md` still owns the
repo-wide proof, performance, concurrency, and skill-routing rules.

## Read First

1. `README.md` and `doc.go`.
2. `writer.go`, `entity.go`, `batch.go`, and `mutations.go`.
3. `schema.go`, `schema_statements.go`, `schema_execution.go`, and
   `schema_labels.go` before changing schema or backend dialect behavior.
4. `go/internal/storage/cypher/README.md` before changing graph-writer
   contracts or backend handoff.

## Local Rules

- Keep this package standard-library-only. `CypherStatement` and
  `CypherExecutor` live here to avoid an import cycle with
  `internal/storage/cypher`.
- Validate every dynamic label and property key with the local Cypher
  identifier helpers before building generated Cypher.
- Keep entity and relationship batch rows homogeneous per call. Relationship
  batches read source label, target label, and relationship type from the first
  row.
- Keep schema dialect branching inside schema helpers. Do not branch on Neo4j
  or NornicDB from entity, batch, or mutation builders.
- Keep `Module` as an index, not a uniqueness constraint.
- Preserve both file identities: `File.path` for canonical merge behavior and
  repo-scoped `File.uid` for shared projection endpoints.
- Keep OCI and package-registry schema truth identity-first. Digest or `uid`
  is identity; mutable tag text and publication hints are evidence, not schema
  ownership truth.
- For NornicDB, keep composite-constraint suppression and supported `uid`
  constraints aligned with `schema.go`.
- Schema bootstrap must stay observable: backend, phase, statement ordinal,
  duration, bounded summary, and failure class must remain available.

## Change Gates

- Add or change schema labels, constraints, or indexes only with hot-path
  Cypher evidence and observability evidence in a tracked repo file when the
  change affects runtime graph writes.
- Add merge or mutation helpers with tests first in the matching package test
  file.
- Run at least `go test ./internal/graph -count=1`; include
  `go test ./internal/storage/cypher -count=1` when a writer contract or schema
  handoff changes.

## Do Not Change Without Owner Review

- Backend-neutral writer contracts.
- Schema identity for `File`, `Module`, OCI image labels, package labels, or
  source-local records.
- NornicDB schema dialect behavior.
