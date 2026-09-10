# AGENTS.md — codequery/search

Bonded leaf of the code family.

## Ownership

- `names.go` owns global entity-name search. The grantless-resolves-
  empty rule is structural: a grantless caller must never reach the
  searcher.
- `enrich.go` owns result enrichment and the metadata merge. The nil
  absent-metadata contract is structural (see README).
- `codequery/global_name_search.go` and
  `codequery/result_enrichment.go` own the thin methods, the seam
  wrapper, and the grandfathered-support shims. Do not move the
  methods here.

## Rules for agents

- Never import `codequery` or root `query` from this package.
- New files need a useful Go doc comment; no placeholders.
