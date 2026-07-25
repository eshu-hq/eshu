# #5779 supply-chain-impact consumption-anchor precedence: correctness + no-regression evidence

`TestSupplyChainImpactHandlerPreservesConsumptionRepositoryOverImageIdentity`
was red on `origin/main` (`e00bc2b37`): a finding with both a
`package_consumption` git anchor (`github.com/...` form) and an `os_package`
image-identity anchor surfaced the image-identity RepositoryID. The #5464
os_package git-source-anchor guard in
`go/internal/reducer/supply_chain_impact_index.go` skipped its override only when
RepositoryID already had the `repository:` prefix, so the prefix-less
consumption form was treated as replaceable and overwritten.

The fix replaces the prefix whitelist with `supplyChainRepositoryAnchorIsReplaceable`,
which treats an anchor as replaceable only when it is blank or a dead
`oci-registry://` path — preserving every git form (`repository:...` and
`github.com/...`) while keeping the #5464 rescue of a blank or OCI-clobbered
anchor.

## Behavior-change proof (failing-then-green)

Behavior change (a correctness fix), so the intended delta is proven, not
identity with the old wrong output:

- BEFORE: `TestSupplyChainImpactHandlerPreservesConsumptionRepositoryOverImageIdentity`
  fails — `RepositoryID = "github.com/example/image-anchor-app"`.
- AFTER: the same test passes — `RepositoryID = "github.com/example/consumption-anchor-app"`.
- `go test ./internal/reducer -count=1` is fully green: no other `SupplyChainImpact`
  test regresses (the blank→fill, OCI→replace, and `repository:`→preserve cases
  are unchanged; only the buggy `github.com/...`→preserve case flips).

## No-Regression Evidence

No-Regression Evidence: the change swaps one O(1) per-finding string test
(`strings.HasPrefix(id, "repository:")`) for another
(`strings.TrimSpace` + `strings.HasPrefix(id, "oci-registry://")`) inside the
existing `classifySupplyChainImpactPackage` loop. No new query, graph read,
allocation, batch, lease, or worker path is introduced; the classifier still
runs once per candidate finding.

- Baseline / after measurement: `go test ./internal/reducer -count=1` wall time
  unchanged within noise (~5–9s across runs on the development machine, single
  package); the touched function performs the same number of comparisons per
  finding.
- Backend / version: none — this is in-process reducer classification over
  already-loaded facts; it issues no Cypher and no Postgres query, so no
  NornicDB/Postgres backend behavior changes.
- Input shape / corpus: supply-chain-impact findings in the B-7 golden corpus
  (27-repo minimal corpus, `supply-chain-demo-db` fixture).
- Terminal row counts: the B-7 golden-corpus live gate is green with
  `list_supply_chain_impact_findings` returning **1** finding
  (`repository_id`, `subject_digest` unchanged) — identical to before the fix,
  so the corpus finding does not hit the corrected precedence path and the B-12
  snapshot needs no update.
- Why safe: pure precedence logic covered by a failing-then-green regression
  test plus a fully green package and a no-drift live golden gate.

## No-Observability-Change

No-Observability-Change: no metric, span, log, or status field is added or
changed. The `supply_chain_impact` reducer domain remains covered by the
existing `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds` instruments and the
`eshu_dp_reducer_input_invalid_facts_total` quarantine counter (labeled
`domain=supply_chain_impact`); this fix only changes which already-observed
RepositoryID value a finding carries, not how the projection is instrumented.

## Scope

Fixes #5779 only. Sibling #5780 (SBOM path clobbers a consumption anchor with a
dead OCI path) has no failing test and is left to its own PR; this change keeps
the OCI path replaceable, so it neither closes nor worsens #5780.
