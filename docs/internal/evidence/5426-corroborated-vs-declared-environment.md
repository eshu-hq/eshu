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

On `main`, `deployed_image` was not promoted from a declared environment
directly. `supply_chain_impact_runtime.go` gated it on
`RuntimeReachability != "known_fixed"`, a non-empty `SubjectDigest`, and at least
one surviving deployment; the environment was only *appended* to
`finding.Environments`.

The over-promotion was therefore indirect, and it reached `deployed_image`
through which deployments counted as "surviving". Environment carries the
deployment link in exactly one place: the third branch of
`supplyChainDeploymentMatchesFinding`, which joins on `repositoryID` plus an
operational anchor and a non-empty environment, with no artifact identity at all.
A finding with a genuine digest could reach `deployed_image` through a deployment
that never referenced that digest — not because the environment promoted it, but
because that deployment counted toward the `len(deployments) > 0` gate.

Implementing the issue's literal text would have tightened all three branches,
breaking two legitimate artifact-identity-anchored paths.

## What shipped

The gate went on the **promotion**, not on the match.

My first attempt tightened the third branch of `supplyChainDeploymentMatchesFinding`
itself, so a declared-only deployment simply stopped matching. Separate-context
review caught that as an accuracy regression, and it was right. A match carries
four things, and only one of them was the problem:

1. the environment, appended to `finding.Environments`;
2. the `cicd_run_correlation` hop on `finding.EvidencePath`;
3. the correlation's fact ID on `finding.EvidenceFactIDs`;
4. `RuntimeReachability` promotion to `deployed_image`.

Refusing the match discarded all four. Item 2 is the load-bearing one:
`rowHasCIDeclaredDeploymentEvidence` (`internal/query/supply_chain_impact_result.go`)
reads exactly that hop to hold a row at
`deployment_truth_tier=provenance_ci_declared`, so dropping the match silently
downgraded it to `config_only` — for findings that never had the over-promotion
problem in the first place. It also made the issue's own goal unreachable: an
environment can only be *reported* as `declared` if it is recorded at all.

So `supplyChainDeploymentMatchesFinding` branch 3 is unchanged from `main`, and a
new `supplyChainDeploymentPromotesRuntimeReachability` decides promotion: a
matched deployment promotes when it is artifact-anchored (digest or image-ref
agreement with the finding) or when its environment carries `deploy_event`
corroboration. A declared-only branch-3 deployment keeps its environment, its
evidence hop, its fact ID, and its truth tier — it just no longer carries the
finding to `deployed_image` on its own.

One more correction came out of the second review round. `deploy_event`
corroborates the *environment*, not the *artifact*, and a correlation row
carries `artifact_digest` and `environment_evidence` together — so a
`deploy_event` row can name a digest that contradicts the finding's own subject.
Treating that as corroboration would let `deploy_event` act as a blanket
override, promoting a finding about image X through a deployment that explicitly
says image Y shipped. A contradicting digest is positive evidence of absence,
which outranks the environment signal, so it disqualifies the deployment
outright. Only the digest is decisive here: image references are mutable and
registry-prefixed, so two differing refs do not reliably denote two different
artifacts.

The `environment_evidence` value both rules read is published by #5425 on the
`reducer_ci_cd_run_correlation` payload, and that fact kind is already in this
reducer's load set — so reading it is not a
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

## What else moves when the promotion is withheld

`RuntimeReachability` is read by more than the field itself, so withholding the
promotion cascades. Four consequences, all deliberate: three on persisted
wire-visible fields, and one on query behavior, because one of those fields is
itself a query input.

### The reachability envelope stops saying "reachable"

`withSupplyChainReachability` derives `finding.Reachability` from
`RuntimeReachability`, and maps `image_sbom` / `image_os_package` /
`deployed_image` onto `state=reachable, source=runtime_or_sbom`. Measured on the
primary branch-3 fixture, HEAD versus `main`'s gate:

```
main gate:  runtime_reachability="deployed_image"
            reachability={state:"reachable", confidence:"partial", source:"runtime_or_sbom"}
HEAD gate:  runtime_reachability="package_api_missing_evidence"
            reachability={state:"missing_evidence", confidence:"unknown", source:"parser_js_ts"}
```

This is the deepest expression of what the issue asks for. On `main`, a
declared-only workflow environment was enough to label a finding
runtime-**reachable** — a truth field, not a triage score, and one that reaches
the SARIF export as `eshu.reachabilityState`. `state` is now driven by the
evidence the finding actually has. Pinned in
`...Branch3DeclaredOnlyDoesNotPromoteRuntimeReachability`, which asserts both
that the state is no longer `reachable` and that the source is no longer
`runtime_or_sbom`, and fails under `main`'s gate.

### Priority moves, on two different populations

There is a second, deliberate delta, and it is worth stating because it is not
obvious from the change itself.

`supplyChainImpactPriorityContributions` gates two contributions —
`sbom_image_evidence` (+15) and `runtime_reachable` (+25) — on
`RuntimeReachability == "image_sbom"`, an **exact** string match. So on `main`,
promoting an SBOM-derived finding to `deployed_image` silently erased 40 points:
a *stronger* reachability tier cost the finding its priority. Holding it at
`image_sbom` gives them back.

Measured on the SBOM-only fixture — `sbomOnlyFindingFacts`, the one
`TestBuildSupplyChainImpactFindingsDeclaredOnlyKeepsImageSBOMPriority` uses, not
the branch-3 fixture measured in the section above — HEAD's gate versus `main`'s
`len(deployments) > 0` gate (applied via `go test -overlay`):

```
HEAD gate:  runtime_reachability="image_sbom"      priority_score=95  bucket="critical"
main gate:  runtime_reachability="deployed_image"  priority_score=55  bucket="medium"
```

The fixture's CVSS is deliberately low. At 9.1 both sides saturate at
100/critical and the delta is invisible — which is why it went unnoticed until
the final review round.

There is a second priority channel, and it applies to a **different**
population. `supplyChainImpactPriorityContributions` also switches on
`Reachability.State`/`.Source`, but none of those cases match
`source=runtime_or_sbom` — the value both `image_sbom` and `deployed_image` map
to. So for SBOM-derived findings this channel contributes zero on both sides,
and the 40-point delta above is entirely channel one. (15 + 25 = 40 = 95 - 55,
which is the arithmetic check that the second channel is not involved.)

Where the second channel does move is findings anchored by a code-reachability
source — govulncheck, the JS/TS parser, or SCIP — because for those the state
genuinely differs between the two gates. It is signed, not uniformly positive:

- a `not_called` govulncheck finding now takes the `reachability_not_called`
  penalty (**-20**) that `main`'s promotion to `deployed_image` had erased;
- a symbol-reachable govulncheck or JS/TS finding now takes
  `reachable_code_evidence` (**+20**), which the promotion had likewise erased.

Both directions are the same correction: `main` overwrote a finding's real
code-reachability state with a deployment claim, and the priority followed the
overwrite rather than the evidence.

`priority_score`, `priority_bucket`, and `priority_reason_codes` are persisted
and wire-visible, so this is a real contract change on a triage field, not an
internal detail. The direction is defensible: a declared-only deployment is
weaker evidence than the SBOM image anchor it was displacing, and the finding
now keeps the anchor it actually has. But it is a second delta, so it is
declared here and pinned by
`TestBuildSupplyChainImpactFindingsDeclaredOnlyKeepsImageSBOMPriority`, which
fails under `main`'s gate.

### Priority is a query input, so page membership moves too

`priority_score` and `priority_bucket` are not only reported, they are read back
by the findings route, so shifting them shifts what a query returns:

- `priority_bucket` and `min_priority_score` are documented filters, and either
  can be the sole anchor of a request. The fixture above moves `55/medium` to
  `95/critical`, so `?priority_bucket=medium` stops returning that row and
  `?priority_bucket=critical` starts returning it. That is page membership, not
  ordering.
- `sort=priority_score_desc|asc` pages with a keyset cursor on `priority_score`,
  so both the order and the page boundary move.
- Canonical de-duplication picks the winner with
  `ROW_NUMBER() OVER (PARTITION BY canonical_key ORDER BY priority_score DESC, ...)`,
  mirrored in the materialized winners store, so a large enough shift can change
  which duplicate fact is served as the canonical row.
- The aggregates route's `by_priority_bucket` counts move with the buckets.

No code change is involved — this is the existing read model doing what it
already does with a value this change moves. It is recorded because a reader
checking whether a filtered query still returns the same rows deserves to know
the answer is no.

## The payload field is additive-optional, not required

`environment_evidence` carries `omitempty` on
`sdk/go/factschema/reducerderived/v1`'s finding struct, so it is an
additive-*optional* field: a minor change under Contract System v1, needing no
conversion shim.

It was briefly required. The schema generator derives `required` from
reflection, so a tag without `omitempty` lands the field in the required set,
and `scripts/verify-factschema-diff.sh` correctly rejected that as
`added_required_field` — a compatibility break without a major bump.

Worth recording for the next person: **`make pre-pr` does not run
`verify-factschema-diff.sh`.** The full local promotion gate passed green with
the break in place; only CI's `factschema-diff` workflow would have caught it,
and merging would have turned that gate red on `main`. If a change touches
`sdk/go/factschema/schema/**`, run that verifier by hand — a green `make pre-pr`
says nothing about payload-contract compatibility.

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
    -run 'EnvironmentEvidence|Branch3|ContradictingDigest|RegardlessOf|DeployEventWins|WithoutSubjectDigest|ImageSBOMPriority'
--- PASS: TestRecordSupplyChainEnvironmentEvidenceSkipsBlankEnvironment
--- PASS: TestNormalizeSupplyChainEnvironmentEvidenceTrimsPadding
--- PASS: TestCICDRunCorrelationPayloadIncludesEnvironmentEvidence
--- PASS: TestExtractEC2InstanceNodeRowsDeterministicOrderRegardlessOfInput
--- PASS: TestBuildSupplyChainImpactFindingsWithoutSubjectDigestDoesNotPromote
--- PASS: TestBuildSupplyChainImpactFindingsImageRefBranchPromotesRegardlessOfEnvironmentEvidence
--- PASS: TestRecordSupplyChainEnvironmentEvidenceDeployEventWinsOverDeclared
--- PASS: TestBuildSupplyChainImpactFindingsBranch3DeployEventPromotesRuntimeReachability
--- PASS: TestBuildSupplyChainImpactFindingsDigestBranchPromotesRegardlessOfEnvironmentEvidence
--- PASS: TestBuildSupplyChainImpactFindingsBranch3DeployEventWithContradictingDigestDoesNotPromote
--- PASS: TestBuildSupplyChainImpactFindingsImageRefMatchWithContradictingDigestDoesNotPromote
--- PASS: TestBuildSupplyChainImpactFindingsBranch3DeclaredOnlyDoesNotPromoteRuntimeReachability
--- PASS: TestBuildSupplyChainImpactFindingsDeployEventWinsAcrossDeploymentsInEitherOrder
--- PASS: TestSupplyChainImpactTypedPayloadPersistsEnvironmentEvidence
--- PASS: TestSupplyChainDeploymentContextFromEnvelopeDecodesEnvironmentEvidence
--- PASS: TestBuildSupplyChainImpactFindingsDeclaredOnlyKeepsImageSBOMPriority
ok  	github.com/eshu-hq/eshu/go/internal/reducer	0.910s

$ cd go && go test ./internal/query -count=1 -v -run 'EnvironmentEvidence'
--- PASS: TestDecodeSupplyChainImpactFindingRowDecodesEnvironmentEvidence
--- PASS: TestDecodeSupplyChainImpactFindingRowToleratesAbsentEnvironmentEvidence
--- PASS: TestCICDListRunCorrelationsExposesEnvironmentEvidence
--- PASS: TestSupplyChainImpactFindingsOmitEnvironmentEvidenceWhenAbsent
--- PASS: TestSupplyChainImpactFindingsExposeEnvironmentEvidenceInResponseBody
ok  	github.com/eshu-hq/eshu/go/internal/query	0.949s
```

(`RegardlessOf` also matches an unrelated EC2-ordering test, and the reducer
filter deliberately keeps it rather than narrowing the regex to flatter the
output.)

The two `...PromotesRegardlessOfEnvironmentEvidence` tests are the load-bearing
ones. They assert the digest and image-ref branches still promote under
declared-only evidence, so the premise correction cannot be quietly undone by a
later change that "tightens" all three branches uniformly.

`...Branch3DeployEventWithContradictingDigestDoesNotPromote` is the guard for the
second-round correction: it proves `deploy_event` cannot override a deployment
that names a different digest, so the corroboration signal stays scoped to the
environment. Its sibling `...Branch3DeployEventPromotesRuntimeReachability` uses
a deployment with no digest at all, which is the case corroboration is actually
meant to rescue — the two together pin both sides of that boundary.

### Every rule is mutation-audited

Each rule this change introduces was audited by deleting or weakening exactly
that rule via `go test -overlay` and confirming a test goes red. The full
mutant/test table, the six rows that exist only because the audit found them
uncovered, and the case where the guard was silently defanged are in the
companion document
[#5426 — mutation audit of the corroborated-vs-declared rules](5426-mutation-audit.md).

Cross-package and contract gates:

```
$ cd go && go test ./internal/reducer ./internal/query ./internal/mcp ./cmd/reducer -count=1
ok — all packages

$ bash scripts/verify-factschema-diff.sh                  no breaking changes
$ bash scripts/verify-payload-usage-manifest.sh          ok
$ bash scripts/verify-openapi.sh                          252 routes, 252 OpenAPI entries
$ cd go && go run ./cmd/fact-kind-registry -check         generated artifacts are current
$ cd sdk/go/factschema && go test ./... -count=1          ok
```

### Golden corpus

The B-7 gate is green on this branch and now asserts this change end to end. The
`list_supply_chain_impact_findings` MCP shape pins
`findings[].environments[] = prod` and
`findings[].environment_evidence.prod = deploy_event` on the CVE-2026-00010
finding, with both fields required on the result item — and both are `omitempty`,
so an empty evidence map reds the gate.

Getting there took two production fixes, not a cassette change. The corpus's
correlation was being rejected as provenance-only because the persisted
`reducer_container_image_identity` row for its digest had lost the CI run's
build provenance, and the impact finding was never replayed after the
correlation later resolved. Both were measured against a live gate database.
The gate output, the before/after rows, the failing-then-green tests, and what
the corpus still does not assert are in the companion document
[#5426 — what the golden corpus asserts about environment_evidence](5426-golden-corpus-coverage.md).

No-Regression Evidence: the touched hot path is
`applySupplyChainRuntimeContext` in `go/internal/reducer/supply_chain_impact_runtime.go`,
which gains one `map[string]string` per finding that records an environment and
one O(matched deployments) pass over a slice the same function already iterates
twice. No query, index, Cypher, or SQL shape changes, and no new I/O. Covered by
`cd go && go test ./internal/reducer ./internal/query ./internal/mcp ./cmd/reducer -count=1`
and the B-7 golden-corpus runs recorded in the companion above, whose phase
timings stayed inside their baselines apart from the advisory warns explained
there.

No-Regression Evidence (`containerImageBuiltFromRows`): conferring build
provenance on the CI-run decision makes two decisions resolve the same
`(digest, repository)` pair, so the row builder now dedupes them. Measured on
`BenchmarkContainerImageBuiltFromRows` (N=5000 distinct decisions, the
dedup's worst case since there is nothing to remove), median of 6:

| variant | ns/op | allocs/op | B/op |
| --- | --- | --- | --- |
| no dedup (`main`) | 970,245 | 25,001 | 1,960,963 |
| concatenated key | 1,338,353 | 30,018 | 2,643,396 |
| struct key (shipped) | 1,250,194 | 25,018 | 2,354,498 |

**+28.9% over `main`**, allocation *count* flat but bytes **+20.1%** — the map's
own storage. Quoting only allocs/op would have understated the trade by 393 KB/op. The struct key recovered only 6.6% —
the rest is the map itself, and it ships. `testdata/benchmarks/reducer-handler-budgets.txt`
carries an absolute ceiling for this benchmark with 1.50x headroom over its
baseline; projecting this ratio leaves roughly 1.16x, so the next
`refresh-reducer-handler-budgets.sh` run on the enforcement runner should expect
a tighter margin. The gate is advisory today (`REDUCER_PERF_ENFORCE=false`).

The trade is deliberate rather than free: a duplicate row is idempotent at the
writer (`MERGE` on `(start, end, type)`) but still costs a MERGE round, so the
payload reduction is not purely cosmetic. It is recorded here because a
budgeted handler losing a third of its headroom should not be discovered by
whoever flips that gate to enforcing.

Observability Evidence: no metric name, span, or status field is added, and no
new failure path is introduced. One metric's bounded outcome and counted unit
do shift:
`eshu_dp_provenance_edges_total{outcome="submitted"}` samples distinct rows
accepted by the successful writer call rather than one row per (decision x
build-provenance repository) pair. The value differs only for the duplicate
case this deduplication collapses. Rows from a failed writer call emit no point;
retract errors, empty projections, and unwired writers emit none. A missing
endpoint remains a submitted writer no-op, and a successful retry can count the
same identity again, so this event counter does not claim unique durable edges.
The gate withholds a promotion rather than rejecting a deployment: the
deployment still matches, so it keeps
contributing its environment, evidence hop, and fact ID, and no rejection
reason or dead-letter is involved. An operator reading a finding sees the
environment present and labelled `declared` — which is a strictly clearer
signal than the previous behavior, where a declared-only environment was
indistinguishable from a corroborated one.

## Known limitation

Corroboration here means "a provider Deployments API event was observed at this
run's commit", not "this artifact was observed running in that environment". The
stronger claim requires the runtime probe in #5834. A finding whose only
deployment evidence is a declared workflow environment now reports that
environment as `declared` rather than being silently blended with corroborated
ones — which is the accuracy improvement this issue can honestly deliver.
