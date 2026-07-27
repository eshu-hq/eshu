# #5426 — corroborated versus declared-only deployment environments

The issue asked to stop promoting `RuntimeReachability="deployed_image"` from a
CI-declared free-text environment alone, corroborating instead against
kubernetes-live, terraform state, or cloud tag evidence.

It could not be implemented as written. This records why, and what shipped
instead.

## Two of the three named sources cannot work

**terraform state.** `terraform_state_tag_observation.v1.schema.json` carries
`resource_address`, `tag_key_hash`, and `tag_source`. There is no tag *value*
anywhere in the payload, so it cannot supply an environment name at any layer,
in any design.

**cloud tags.** A reducer-side cloud-fact join was already attempted and
reverted. #5452 (PR #5790, merged `83e09aaf8`) records it in its own commit
message: the reducer-side join was *"silently inert: cloud facts never reach the
supply-chain reducer's scope"*. The surviving mechanism is the bounded
query-time probe in `go/internal/query/supply_chain_impact_cloud_runtime_probe.go`.

**kubernetes-live.** Reachable only at query time for the same reason.
`supplyChainImpactFactKinds()` documents cross-scope additions as "the
tempting-but-wrong fix" in its own comment, which is exactly what #5452 proved
empirically.

An environment-*name* join is also weak evidence independent of reachability:
"this repository deploys to prod, and a prod Environment node exists" does not
place *this artifact* in prod. The honest kubernetes corroboration is a
digest-anchored `RUNS_IMAGE` probe mirroring #5452, filed as **#5834**.

## The premise was also partly wrong

`deployed_image` is not promoted from a declared environment.
`supply_chain_impact_runtime.go` gates it on `RuntimeReachability != "known_fixed"`,
a non-empty `SubjectDigest`, and at least one surviving deployment. The
environment is only *appended* to `finding.Environments`.

Environment carries the deployment link in exactly one place: the third branch of
`supplyChainDeploymentMatchesFinding`, which joins on `repositoryID` plus an
operational anchor and a non-empty environment, with no artifact identity at all.
That branch is the real over-promotion — a finding with a genuine digest can
reach `deployed_image` through a deployment that never referenced that digest.

Implementing the issue's literal text would have tightened all three branches,
breaking two legitimate artifact-identity-anchored paths.

## What shipped

The third branch additionally requires `environment_evidence == "deploy_event"`.

That value is published by #5425 on the `reducer_ci_cd_run_correlation` payload,
and that fact kind is already in this reducer's load set — so reading it is not a
cross-domain join and cannot go inert the way #5452's attempt did. No fact kind
was added to `supplyChainImpactFactKinds()`, no graph port was added to the
handler.

Findings now carry per-environment evidence state alongside the unchanged
`Environments` list, so a consumer can distinguish a corroborated environment
from a declared one instead of seeing them blended. `deploy_event` wins a
collision with `declared`, and a correlation row written before #5425 maps to
`declared` rather than inventing corroboration it cannot support.

This is deliberately **not** a new `RuntimeReachability` value — that axis is
artifact reachability (`image_sbom`, `image_os_package`, `deployed_image`,
`known_fixed`), while corroboration is per-environment. It is also not
`truth.TierDeclaredRef`, which #5393 owns for DEPLOYS_REF evidence.

## The alias-contract dependency was already satisfied

The issue says to coordinate with the spine epic's environment-alias contract.
That contract is landed (`docs/public/reference/environment-alias-contract.md`),
`canonicalEnvironmentName` is now a one-line delegate to `environment.Canonical`
with no second alias table, and the CI correlation path already canonicalizes
both declared and deploy-event environments. Coordination here meant conforming
to it, not waiting on it.

## Verification

Behavior change, so the proof is the intended delta.

```
$ cd go && go test ./internal/reducer -count=1 -v \
    -run 'EnvironmentEvidence|Branch3'
--- PASS: TestSupplyChainDeploymentContextFromEnvelopeDecodesEnvironmentEvidence
--- PASS: TestBuildSupplyChainImpactFindingsBranch3DeclaredOnlyDoesNotPromoteRuntimeReachability
--- PASS: TestBuildSupplyChainImpactFindingsBranch3DeployEventPromotesRuntimeReachability
--- PASS: TestBuildSupplyChainImpactFindingsDigestBranchPromotesRegardlessOfEnvironmentEvidence
--- PASS: TestBuildSupplyChainImpactFindingsImageRefBranchPromotesRegardlessOfEnvironmentEvidence
--- PASS: TestSupplyChainImpactTypedPayloadPersistsEnvironmentEvidence
--- PASS: TestRecordSupplyChainEnvironmentEvidenceDeployEventWinsOverDeclared
ok  	github.com/eshu-hq/eshu/go/internal/reducer
```

The two `...PromotesRegardlessOfEnvironmentEvidence` tests are the load-bearing
ones. They assert the digest and image-ref branches still promote under
declared-only evidence, so the premise correction cannot be quietly undone by a
later change that "tightens" all three branches uniformly.

Two pre-existing tests in `supply_chain_impact_operational_anchor_test.go` now
supply `deploy_event`. They exercise operational-anchor attachment and
provenance-only rejection — behaviour *downstream* of the branch-3 match — so
without it they would have stopped testing their own subject and started testing
the new gate.

Cross-package and contract gates:

```
$ cd go && go test ./internal/reducer ./internal/query ./internal/mcp ./cmd/reducer -count=1
ok — all packages

$ bash scripts/verify-payload-usage-manifest.sh          ok
$ bash scripts/verify-openapi.sh                          252 routes, 252 OpenAPI entries
$ cd go && go run ./cmd/fact-kind-registry -check         generated artifacts are current
$ cd sdk/go/factschema && go test ./... -count=1          ok
```

### Golden corpus, and why it cannot assert this

The gate is green on this branch at `bdaa24977`:

```
$ COMPOSE_PROJECT_NAME=env5426gate2 bash scripts/verify-golden-corpus-gate.sh
summary: 506 pass, 0 required-fail, 2 advisory-warn
=== PASS: B-7 golden corpus gate green (elapsed 158s, budget ceiling 1800s) ===
```

Both advisory warns are phase timing under a deliberately raised
`GATE_COLLECTOR_SETTLE_SECONDS=75`, not assertion failures.

I tried to add a real floor for this change rather than settle for a green run
that asserts nothing about it:

```json
"findings[].environment_evidence.prod": "deploy_event"
```

**It failed, and the failure is the useful result:**

```
[FAIL] mcp:list_supply_chain_impact_findings: required JSON value
  "findings[].environment_evidence.prod" failed:
  path segment "environment_evidence" resolved no values
```

The assertion was reverted, because the cause is a pre-existing corpus
limitation rather than a defect in this change. The corpus's only impact
finding is an OS-package finding, and `SupplyChainImpactResult.RuntimeContext`
already documents that for those, the baked `workload_ids` / `service_ids` /
`environments` fields "stay empty ... until #5747 makes the filters agree".
Every runtime value the snapshot asserts sits under `runtime_context`, which
#5746 resolves at read time from `repository_id` — a path that never calls
`matchingSupplyChainDeployments`.

That empty map is provably the cause and not a dropped field. The decode maps
it (`supply_chain_impact_findings_decode.go:69`),
`normalizeSupplyChainEnvironmentEvidence` never returns empty (an absent key
maps to `declared`), and `recordSupplyChainEnvironmentEvidence` skips only when
the *environment* is empty. So an empty evidence map implies an empty baked
`Environments` list: there is no matched deployment on this finding, and
therefore nothing to corroborate.

The producer half is already pinned in the snapshot by #5425 and passes here:

```
[PASS] GET /api/v0/ci-cd/run-correlations?environment=prod&...:
  values [correlations[].environment correlations[].environment_evidence]
```

So the corpus proves the correlation carries the evidence; the unit tests prove
the reducer consumes it. Nothing in the committed corpus exercises the join
between them, and I would rather say that plainly than leave a green gate
implying coverage this change does not have.

This surfaced a real gap worth tracking on its own: environment corroboration
rides only on the baked fields, so it never reaches the read-time-resolved
`runtime_context` that OS-package findings actually serve. Filed as **#5835**.

No-Observability-Change: no metrics, spans, or status fields are added or
altered. The branch-3 tightening removes a match rather than adding a failure
path, and a rejected deployment is already reflected in the finding's existing
missing-evidence reporting.

## Known limitation

Corroboration here means "a provider Deployments API event was observed at this
run's commit", not "this artifact was observed running in that environment". The
stronger claim requires the runtime probe in #5834. A finding whose only
deployment evidence is a declared workflow environment now reports that
environment as `declared` rather than being silently blended with corroborated
ones — which is the accuracy improvement this issue can honestly deliver.
