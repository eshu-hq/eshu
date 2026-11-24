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
