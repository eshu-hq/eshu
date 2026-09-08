# AGENTS.md - repositoryartifacts

## Read first

1. `doc.go` for the public contract.
2. `README.md` for the ownership boundary and proof requirements.
3. Parent `../AGENTS.md` for query-wide invariants.

## Invariants

- Leaf package: standard library, `querycontract`, and content-parsing
  libraries only. Never import `repository`, `repositoryreadmodel`, or the
  query root.
- Artifact predicates feed access decisions (`filter...ForAccess` callers);
  keep the fail-closed behavior and bounded limits exactly as they are.

## Verification

Run focused `repositoryartifacts` tests, then `repository` and root `query`
suites. Run `scripts/verify-package-docs.sh` whenever this package changes.
