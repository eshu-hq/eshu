# #5802 main-red: restore supplyChainRepositoryAnchorIsReplaceable

`origin/main` (`360a4cc41`) did not compile:

```
internal/reducer/supply_chain_impact_index.go:261:34: undefined: supplyChainRepositoryAnchorIsReplaceable
```

Every Go job — build, vet, lint, all reducer tests, the corpus gate — failed on
`main` and on every branch rebased onto it.

## Sequence (a semantic conflict git merged cleanly)

| commit | definitions of the function in that file |
| --- | --- |
| `0fad58eed` (#5779, PR #5782) | 1 — added, and used in the os-package anchor guard |
| `c78022471` (#5468, PR #5781) | 0 — landed with a copy of the file based on a revision predating #5779, silently deleting the definition; replaced that guard with its own `repoFromConsumption` check, so behavior held and nothing referenced the function any more |
| `360a4cc41` (#5780, PR #5783) | 0, plus a NEW call at line 261 guarding the SBOM RepositoryID overwrite — against a function that no longer existed |

Neither merge produced a textual conflict, and this repository enforces no
required status checks, so a non-compiling `main` landed.

## Fix

Restore the definition only. #5468's `repoFromConsumption` guard and #5780's new
call site are both left exactly as their authors intended, and #5780's call
resolves again.

## No-Regression Evidence

No-Regression Evidence: this restores a previously-present pure function and
changes no call site, no control flow, and no SQL, Cypher, queue, lease, or
worker path. The function is an O(1) `strings.TrimSpace` plus one
`strings.HasPrefix` on a single field, evaluated once per candidate finding
inside the existing `classifySupplyChainImpactPackage` loop.

- Baseline / after: not comparable by wall time, because `main` did not build at
  all before this change (the baseline is a compile failure, not a slower run).
  After: `cd go && go build ./...` clean; `go test ./internal/reducer -count=1`
  green in ~2.8s on the development machine.
- Backend / version: none — in-process reducer classification over already-loaded
  facts; it issues no Cypher and no Postgres query.
- Input shape / corpus: supply-chain-impact findings, B-7 golden corpus
  (`supply-chain-demo-db` fixture).
- Terminal row counts: proven by the live B-7 golden-corpus gate, not inferred
  from unit tests. This helper controls `findings[].repository_id`, which the
  B-12 snapshot pins exactly for `list_supply_chain_impact_findings`, so unit
  tests alone could not establish end-to-end terminal truth (codex review P1 on
  PR #5803 — a fair catch against the first draft of this note).

  `bash scripts/verify-golden-corpus-gate.sh` on this branch:

  ```
  [PASS] mcp:list_supply_chain_impact_findings: "findings" has 1 results;
         item fields [cve_id impact_status repository_id subject_digest] present;
         values [findings[].repository_id findings[].subject_digest]
  [PASS] fact_work_items_residual: residual=0 (dead_letter=0)   (x3 drains)
  summary: 492 pass, 0 required-fail, 1 advisory-warn
  === PASS: B-7 golden corpus gate green (elapsed 100s, budget ceiling 1800s) ===
  ```

  The reducer-to-MCP readback carries `repository_id`, so the snapshot needs no
  update: restoring the definition returns the projection to the truth the
  snapshot already pins. The single advisory-warn is `phase_maintenance_drains`
  timing variance on a shared development host, unrelated to this change.
- Why safe: the identical function body was on `main` at `0fad58eed`, and the
  reducer package — including
  `TestSupplyChainImpactHandlerPreservesConsumptionRepositoryOverImageIdentity`
  (#5779) and the #5780 SBOM-anchor test — is green with it restored.

## No-Observability-Change

No-Observability-Change: no metric, span, log, or status field is added or
changed. The `supply_chain_impact` reducer domain remains covered by the existing
`eshu_dp_reducer_executions_total`, `eshu_dp_reducer_run_duration_seconds`, and
`eshu_dp_reducer_input_invalid_facts_total` instruments (labeled
`domain=supply_chain_impact`). This change only makes the package compile again.

## Follow-up

A non-compiling `main` blocks every branch, and this class of break — two PRs
editing one function region from divergent bases — is invisible to a textual
merge. #5802 notes that making the whole-module `go build` an enforced check
would catch it at the PR boundary.
