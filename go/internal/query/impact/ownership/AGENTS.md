# AGENTS.md - impact/ownership

## Read first

1. `doc.go` for the contract, `README.md` for the ownership table and budget.
2. Parent `../AGENTS.md` and `../../AGENTS.md` for query-wide invariants.
3. `docs/internal/evidence/5167-impact-grant.md` before changing a statement
   or the budget.

## Invariants

- Deny by default. A class this package does not recognize is ungranted;
  never add a "unknown owner, admit" path.
- An empty grant makes no graph call; an unscoped caller makes none either.
- Only the page's deduplicated keys reach a statement, in `ChunkSize` chunks,
  capped by `CheckedKeyCap`. Keys past the cap are ungranted and the caller
  must report truncated.
- A row a statement returns for a key it was not asked about is ignored.
- Statement shape: one pattern, keyed node first, `WHERE n.<key> IN $uids`,
  returning `uid` and the owner's `repo_id`; the grant is applied in Go. Keep
  chunks small (per-chunk cost is quadratic in the key list). Never the
  uid-first two-MATCH form, never `UNWIND $uids ... {uid: u}`, never an inline
  grant disjunction, and never a grant-anchored `UNWIND $grant_ids` over an
  unindexed owner property (TerraformResource.repo_id has no index).
- A returned owner id that is empty or outside the grant admits nothing.
- Metric labels stay closed: route patterns, the `Reason*` constants, the
  three statement node labels, and `ok`/`error`.
- Do not import the query root, `impact`, or a graph driver.

## Verification

`go test ./internal/query/impact/... -count=1`, the root impact tests, and
the live NornicDB proofs named in the evidence doc
(`TestLiveImpactOwnershipStatementCost`, the two-tenant live test). A changed
statement or budget needs new live measurements and an evidence update.
