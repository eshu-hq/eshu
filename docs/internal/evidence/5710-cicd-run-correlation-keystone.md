# #5710 ci_cd_run_correlation intent-builder keystone — theory-proof and live evidence

`ci_cd_run_correlation` was registered, had a wired handler, and had cross-scope
dependency metadata declared (#5709), but no builder in
`go/internal/projector/scope_generation_intents.go` ever emitted
`Domain=ci_cd_run_correlation`. It was unreachable outside unit tests and Ifá
replay: `list_ci_cd_run_correlations` always returned zero in the golden gate and
in any live deploy.

## Theory-proof: the classify logic supports a real join

Before touching production code, a throwaway test (not committed) fed the real
`testdata/cassettes/cicdrun/supply-chain-demo.json` and
`testdata/cassettes/ociregistry/supply-chain-demo.json` payload shapes through
the production decode/classify functions directly, no Docker/Postgres/timing
involved:

```
BuildContainerImageIdentityDecisionsWithQuarantine([ci.run, ci.artifact, oci_registry.image_manifest])
  -> outcome=exact_digest reason="artifact digest matched one registry digest observation"
     canonical_writes=1 image_ref="ghcr.io/eshu-hq/supply-chain-demo@sha256:abcdef...ab"

buildCICDRunCorrelationDecisionsWithQuarantine([ci.run, ci.artifact, <that identity fact>])
  -> outcome=exact reason="artifact digest matches one container image identity row"
     canonical_writes=1
```

This proved the join CAN resolve given the cassette's real digest data, isolating
the golden-gate's "derived" result to a runtime/data characteristic rather than a
missing-evidence gap.

## Live evidence: what actually happens in the full 27-repo corpus

A live `--keep` run of `scripts/verify-golden-corpus-gate.sh`, queried directly
against its Postgres mid-run, gave a fuller picture than the isolated
theory-proof:

1. **The correlation fires.** Even during the very first (non-reopened) drain
   pass, before any maintenance reopen ran, a `reducer_ci_cd_run_correlation`
   fact already existed:
   ```
   fact_id  | reducer_ci_cd_run_correlation:9136bb3a...
   scope_id | ci_cd_run:github_actions:eshu-hq:supply-chain-demo
   outcome  | derived
   digest   | sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab
   cw       | 0
   ```
   `cicdRunCorrelationDomainDefinition` writes a durable fact for **every**
   outcome (exact/derived/ambiguous/unresolved/rejected), not only canonical
   ones, so `minimum_results:1` on `list_ci_cd_run_correlations` is deterministic
   from the correlation's first execution — no reopen dependency for that floor.

2. **`exact` is not truthfully achievable for this shared digest.** The digest
   `sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab`
   is deliberately shared by the `kuberneteslive`/`ociregistry`/`cicdrun`
   cassette trio for the RUNS_IMAGE=3 story, but it is *also* reused as a
   generic placeholder container-image digest by several unrelated fixtures.
   Querying active `reducer_container_image_identity` rows for that digest:
   ```sql
   SELECT scope_id, payload->>'repository_id' AS repository_id
   FROM fact_records
   WHERE fact_kind='reducer_container_image_identity'
     AND payload->>'digest' = 'sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab';
   ```
   returned 12 rows: `aws:...:ecs`, `aws:...:lambda`,
   `ci_cd_run:github_actions:eshu-hq:supply-chain-demo`,
   `git-repository-scope:repository:r_08586f9c` (kustomize-deployable-overlay),
   `r_217415d9` (deployable-config), `r_314b35c7` (kubernetes_comprehensive),
   `r_b11b6e25` (supply-chain-demo-db), `r_d14bf326` (php_comprehensive),
   `r_d5318f0f` (api-svc), `oci_registry:...ecr...`, `oci_registry:ghcr.io:...`,
   `sbom_attestation:...` — every one carrying
   `repository_id = "oci-registry://ghcr.io/eshu-hq/supply-chain-demo"` (the OCI
   registry's own repository namespace), never the CI run's canonical
   `repository:r_69256c06`. `ci_cd_run_correlation.go`'s
   `cicdImageMatchesForRepository` repo-narrowing therefore never matches
   anything and always falls back to the full unfiltered digest set — 12
   entries — which classifies as `ambiguous`, never `exact`. Filed as
   [#5766](https://github.com/eshu-hq/eshu/issues/5766).

3. **Repointing the cassette to a private digest was considered and reverted.**
   `sha256:abcdef...ab` is independently load-bearing for two other already-passing
   assertions in `testdata/golden/e2e-20repo-snapshot.json`: `BUILT_FROM` (the
   edge_count assertion, keyed on `container_image_identity`'s own exact_digest
   decision for this digest) and `list_container_image_identities` (#5423,
   filtered by `source_repository_id=repository:r_69256c06` against a row this
   same digest produces). Changing the digest to make `ci_cd_run_correlation`
   uniquely resolvable would have silently regressed both. `sha256:abcdef...ab`
   stays untouched everywhere.

4. **A separate, unrelated latent bug**: the CI-scope's own
   `container_image_identity` work item (triggered by `ci.artifact`) dead-letters:
   ```
   status: dead_letter
   failure_class: projection_bug
   failure_message: write container image built_from provenance edges:
     Neo4jError: Neo.ClientError.Statement.SyntaxError
     (UNWIND MERGE chain relationship update failed: not found)
   ```
   It still durably writes its `reducer_container_image_identity` fact before
   hitting this failure (a partial-success/partial-failure split), so it does
   not block the digest join by itself — 11 other scopes' rows already cover the
   digest — but it is a real bug. Filed as
   [#5767](https://github.com/eshu-hq/eshu/issues/5767).

## Conclusion

- Keep the intent builder (`go/internal/projector/ci_cd_run_correlation_intents.go`)
  and the maintenance-pass reopen addition
  (`go/cmd/bootstrap-index/bootstrap_pipeline.go`) — both proven working: the
  correlation fires and the reopen list still includes it for future
  convergence, matching the existing `container_image_identity` /
  `kubernetes_correlation_materialization` pattern.
- `list_ci_cd_run_correlations`'s golden assertion is `minimum_results:1` with
  no outcome pin — the row's existence is deterministic; its specific outcome
  value (derived vs. ambiguous, depending on how many of the unrelated
  fixtures' identity rows have committed when the reopened intent runs) is not,
  and pinning either would assert an untruth for at least some runs.
- No phase-split / readiness-gated reopen was added: `minimum_results:1` does
  not depend on reopen ordering at all (a decision fact is written on the very
  first execution, regardless of outcome), so that additional complexity was
  out of scope for making this assertion deterministic.

## Evidence markers

No-Regression Evidence: this is a behavior fix (the `ci_cd_run_correlation`
reducer domain was never enqueued in production and now is), not a rewrite of a
proven-correct hot path. The intent builder adds one bounded intent per scope
generation that already carries CI/CD facts, mirroring the sibling
`scope_generation_intents.go` builders; it enqueues no work for scopes without
`ci.*` facts. The bootstrap maintenance-pass reopen gains exactly one more
domain (`ci_cd_run_correlation`) in the same `ReopenSucceededReducerWorkItems`
call that already replays `container_image_identity` and
`kubernetes_correlation_materialization`, and the replay is idempotent
(scope-keyed stable fact key). Backend: the golden-corpus gate on NornicDB
`nornicdb-cpu-bge:v1.1.11` + Postgres 16 over the fixed 20-repo corpus. Before:
`list_ci_cd_run_correlations` returned 0 (the domain never ran) — the vacuous
`minimum_results:0` placeholder. After: it returns 1 deterministically
(`[PASS] mcp:list_ci_cd_run_correlations: "correlations" has 1 results`,
`492 pass, 0 required-fail`, `PASS: B-7 golden corpus gate green` in 99s), with
per-phase timings unchanged from the pre-#5710 baseline (first_drain and
maintenance_drains within noise; the one extra reopened domain adds a single
idempotent replay item). No hot-path Cypher was rewritten; the correlation
handler itself is unchanged.

No-Observability-Change: no metric, span, or log is added or removed. The new
intent builder's enqueue is covered by the existing
`eshu_dp_reducer_intents_enqueued_total` / `eshu_dp_projector_run_duration_seconds`,
and the reducer execution that consumes the intent by
`eshu_dp_reducer_executions_total` / `eshu_dp_reducer_run_duration_seconds`; the
maintenance reopen is covered by the existing bootstrap correlation-reopen phase
timing. The telemetry-coverage doc row for the new stage file is added in the
same change with a No-Observability-Change marker.
