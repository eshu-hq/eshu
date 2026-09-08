# AGENTS.md - repositoryreadmodel

## Read first

1. `doc.go` for the public contract.
2. `README.md` for the ownership boundary and proof requirements.
3. Parent `../AGENTS.md` for query-wide invariants.

## Invariants

- Leaf package: standard library, `net/http`, and `querycontract` only.
  Never import `repository`, `repositoryartifacts`, or the query root.
- Page bounds and truncation semantics are stable wire behavior; changing a
  limit, default, or truncation flag is a contract change, not a refactor.

## Verification

Run focused `repositoryreadmodel` tests, then `repository` and root `query`
suites. Run `scripts/verify-package-docs.sh` whenever this package changes.
