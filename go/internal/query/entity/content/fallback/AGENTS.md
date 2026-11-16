# AGENTS.md - content/fallback

## Read first

1. `doc.go` for the public contract.
2. `README.md` for the ownership boundary.
3. Parent `../../AGENTS.md` for entity-family invariants.

## Invariants

- This package must not import the query root or the parent `entity`
  package. The root (`handler.go`, `entity_alias.go`, wiring) imports this
  leaf, so a root import here cycles.
- Import `querycontract`, `querycontract/kubernetes`, `impact/deployment`,
  `repositoryartifacts`, `parser`, and `ghactionsref`, never the reverse.
- The extraction classifiers stay pure reads over
  `querycontract.ContentRelationshipReader`: no `*sql.DB`, no graph driver,
  no new exported surface beyond `Builder`, the two k8s SELECTS
  classifiers, and the `ContentRelationshipLimit` ceiling. The last three
  exist only so the root's staying regression tests bind the same
  implementation the leaf ships — never a copy.
- Keep every file under 500 lines; split a classifier into its own file
  before it approaches the limit.
- Behavior changes need a failing regression first; a pure move proves
  green-before/green-after on the fallback suites.

## Verification

Run the leaf tests plus the root `query` fallback suites
(`entity_content_*_fallback_test.go`,
`code_relationships_content_fallback_test.go`), whole-module build, and
`scripts/verify-package-docs.sh` when this package changes.
