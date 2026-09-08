# AGENTS.md - serviceevidence

## Read first

1. `doc.go` for the public contract.
2. `README.md` for the ownership boundary and proof requirements.
3. Parent `../AGENTS.md` for query-wide invariants.

## Invariants

- Parsing only. No handler orchestration, graph queries, SQL, or
  family-specific response models.
- Import `querycontract` for the evidence types and the `SpecFileResolver`
  port, never the reverse. Never import the query root, graph drivers,
  Postgres adapters, or any handler family package: the root and
  `repository` both import this leaf, so a back-import cycles.
- `SpecFileResolver`'s contract is load-bearing: a read failure returns an
  error, a genuinely absent file returns empty with nil error (#5720 round
  10). Never collapse the two; a collapsed error silently produces a
  complete-looking spec with fewer servers, hostnames, and consumers.
- A referenced file that parses badly stays tolerated (best-effort content
  read), never an error.

## Verification

Run the root `query` and `repository` package service-evidence and narrative
suites, then whole-module build and vet. Run
`scripts/verify-package-docs.sh` whenever this package changes.

## Common changes

- Add a pure extractor only when both the service stayer and the repository
  narratives need it without importing each other.

## Failure modes

- A third-party import added to `querycontract` instead of here re-exposes
  every handler family to that runtime; parsing runtimes belong in this leaf.
- Swallowing a resolver error relabels a transient read failure as an absent
  file and shrinks the derived evidence silently.

## Anti-patterns

- Do not add handler orchestration, whole graph queries, SQL, or
  family-specific response models here.
