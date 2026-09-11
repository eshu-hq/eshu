# AGENTS.md - service/evidence

## Read first

1. `doc.go` for the public contract.
2. `README.md` for the ownership boundary, move evidence, and proof
   requirements.
3. Parent `../AGENTS.md` for query-wide invariants.

## Scope

`go/internal/query/service/evidence/` (package `evidence`): `parse.go`
holds every exported symbol.

- `SliceValue`, `MapValue`, `StringValue` — read one field out of a
  loosely-parsed YAML/JSON spec document without panicking on shape.
  Callers: the rest of this file's own extractors.
- `OpenAPIRefFilePath` — resolves a `$ref` against its carrying spec file's
  directory. Callers: `service/service_evidence_types.go`
  (`buildSpecFileResolver`).
- `ExtractDocsRoutes`, `LooksLikeDocsRoute` — sorted, de-duplicated
  docs-like route references quoted in content. Callers:
  `service/service_docs_routes.go` (`extractDocsRoutes` wrapper),
  `repository/repository_narrative_enrichment.go`.
- `ExtractAPISpecEvidence`, `ExtractAPISpecEvidenceWithoutRefs` — summarize
  one candidate API spec file, with or without `$ref` resolution. Callers:
  `service/service_evidence.go` (`extractAPISpecEvidence` wrapper),
  `repository/repository_narrative_enrichment.go`.

## Invariants

- Parsing only. No handler orchestration, graph queries, SQL, or
  family-specific response models.
- Import `querycontract` for the evidence types and the `SpecFileResolver`
  port, never the reverse. Never import the query root, graph drivers,
  Postgres adapters, or any handler family package, including this leaf's
  own parent package `service`: `service` and `repository` import this leaf
  directly, and the query root reaches it through `service`, so a
  back-import cycles.
- `SpecFileResolver`'s contract is load-bearing: a read failure returns an
  error, a genuinely absent file returns empty with nil error (#5720 round
  10). Never collapse the two; a collapsed error silently produces a
  complete-looking spec with fewer servers, hostnames, and consumers.
- A referenced file that parses badly stays tolerated (best-effort content
  read), never an error.
- No package-name stutter in exported identifiers or file names
  (`docs/internal/naming.md` rules 2 and 4): the accessors are `SliceValue`/
  `MapValue`/`StringValue`, not `Service*Value`; the file is `parse.go`, not
  `service_evidence_parse.go` or `evidence_parse.go`.

## Verification

Run the root `query`, `service`, and `repository` package service-evidence
and narrative suites, then whole-module build and vet. Run
`scripts/verify-package-docs.sh` whenever this package changes.

## Common changes

- Add a pure extractor only when the service family and the repository
  narratives both need it without importing each other.

## Failure modes

- A third-party import added to `querycontract` instead of here re-exposes
  every handler family to that runtime; parsing runtimes belong in this leaf.
- Swallowing a resolver error relabels a transient read failure as an absent
  file and shrinks the derived evidence silently.

## Anti-patterns

- Do not add handler orchestration, whole graph queries, SQL, or
  family-specific response models here.
- Do not re-glue the package name into a file or export prefix
  (`service_evidence_*`, `Evidence*` duplicating the package name) when
  moving or extending this leaf.
