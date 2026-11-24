# live-9401: the orders go-alias scope as the Postgres fact store held it

`scope-facts.json` is the eleven non-reducer facts of the synthetic scope
`vulnerability_intelligence:orders-api:go-alias` (generation
`cassette-vulnintel-orders-go-alias-gen1`), read from the fact store of a local
Neo4j golden-corpus run on `origin/main` at `def495f429`. In that run the gate's
query-truth check for CVE-2026-9401 read `module_only` instead of
`analysis_failed`.

The scope held the Go scan facts (module evidence, call reachability and the
`go_reachability` warning) beside the advisories, a shape production never has:
the scanner writes them into a `go-reachability://repository:<id>` scope, and a
pass reads them only through the cross-scope Go stage. The tests split the file
by kind, keep the advisories in the pass's own scope and serve the Go scan facts
through that stage, so they pin the production shape. Only the columns the
reducer reads are kept. Every name is synthetic (`acme`, `orders-api`,
`legacy-lib`).

## What this fixture proves, and what it does not

The tests split this file the way production stores it and prove the join: with
the Go scan facts served by the cross-scope Go stage, the three Go verdicts come
out (9229 symbol reachable, 9301 not called, 9401 analysis_failed with the scoped
`toolchain_unsupported` gap). They also pin that the Go stage's repository filter
is derived from the pass's own package-consumption evidence.

They do not prove that the live corpus pass resolves `repository:r_ea78e8bb`
once the Go rows leave the vulnerability-intelligence scope: the live fact store
and the corpus drain order are not modeled here. That claim rests on the plain
Golden Corpus Gate run on this change, whose 9229, 9301 and 9401 rows go in the
PR body.
