# #10655 repository ambiguity 409: no-regression evidence

Scope: branch `drive/10655-repo-ambiguity-structured`. A repository
selector matching more than one repository answers 409 with a stable
`ambiguous` code and a `candidates[{repo_id}]` body instead of a bare
400 with the ids buried in a sentence. The gate flagged
`go/internal/query/selector/selector.go` as a hot file by path; this
note records why the change cannot regress runtime cost.

No-Regression Evidence (error-branch-only response reshape): the
resolution queries are untouched (the same `MatchRepositories` call
and the same `MATCH (r:Repository)` Cypher run before and after), and
only the ambiguity branch (2+ in-memory matches) formats differently.
`AmbiguousCandidates` dedups and sorts the already-materialized match
ids (one map plus one sort, O(n log n) in match count, no I/O, lock,
retry, or enqueue); the old branch joined the same ids into a
sentence, so the response size class is unchanged. The success
(0/1 match), 404, and 500 paths keep their order and bodies, pinned
by `server_failure_test`. Baseline at merge-base `0148ce2b3a`
(`go test ./internal/query/selector/ ./internal/query/repository/
./internal/query/contentread/ ./internal/query/supply/chain/
-count=1`): selector 0.668s, repository 0.842s, contentread 0.444s,
supply/chain 1.460s (go1.27.3, darwin/arm64 dev harness, no
production backend). After measurement at HEAD `0227376dd4a`, same
command: selector 0.263s, repository 0.743s, contentread 0.474s,
supply/chain 1.435s, all green. Input shape: ambiguous selectors
with 2+ matches, including grant-filtered and empty/dup match
lists. Queue/row counts: not applicable, the branch runs after
resolution materializes matches and touches no rows. Change is safe
because no hot-path statement (query, lock, claim, batch size) is
added, removed, or reordered; only the error-branch bytes change.

Observability Evidence: the change adds no spans, metrics, or logs
(`WriteAmbiguousError` has no logger; per #7626 each caller owns its
span). The operator-visible delta is the HTTP status 400 to 409 with
the stable `error.code=ambiguous`, documented with a wire example in
`docs/public/reference/http-api/selector-and-read-failures.md`.
