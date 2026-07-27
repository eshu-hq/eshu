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
    -run 'EnvironmentEvidence|Branch3|ContradictingDigest|RegardlessOf|DeployEventWins'
--- PASS: TestCICDRunCorrelationPayloadIncludesEnvironmentEvidence
--- PASS: TestRecordSupplyChainEnvironmentEvidenceSkipsBlankEnvironment
--- PASS: TestRecordSupplyChainEnvironmentEvidenceDeployEventWinsOverDeclared
--- PASS: TestBuildSupplyChainImpactFindingsImageRefMatchWithContradictingDigestDoesNotPromote
--- PASS: TestBuildSupplyChainImpactFindingsBranch3DeployEventPromotesRuntimeReachability
--- PASS: TestBuildSupplyChainImpactFindingsBranch3DeclaredOnlyDoesNotPromoteRuntimeReachability
--- PASS: TestBuildSupplyChainImpactFindingsBranch3DeployEventWithContradictingDigestDoesNotPromote
--- PASS: TestBuildSupplyChainImpactFindingsDigestBranchPromotesRegardlessOfEnvironmentEvidence
--- PASS: TestSupplyChainDeploymentContextFromEnvelopeDecodesEnvironmentEvidence
--- PASS: TestBuildSupplyChainImpactFindingsImageRefBranchPromotesRegardlessOfEnvironmentEvidence
--- PASS: TestBuildSupplyChainImpactFindingsDeployEventWinsAcrossDeploymentsInEitherOrder
--- PASS: TestSupplyChainImpactTypedPayloadPersistsEnvironmentEvidence
--- PASS: TestExtractEC2InstanceNodeRowsDeterministicOrderRegardlessOfInput
ok  	github.com/eshu-hq/eshu/go/internal/reducer	0.894s

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

After the defanging below, each rule this change introduces was audited by
deleting or weakening exactly that rule via `go test -overlay` and confirming a
test goes red. A rule no mutant can kill is not guarded, however many tests
mention it.

The audit was run three times, and each pass found rules the previous pass had
missed — including two, the ANY-of quantifier and the blank-environment skip,
that no test touched even after the table below was first written. Both were
reachable with ordinary production data. The table is the current state, not a
claim that the technique is exhaustive.

| Rule | Mutant | Caught by |
|---|---|---|
| `environmentEvidence == deploy_event` return | `return false` | `...Branch3DeployEventPromotesRuntimeReachability` |
| contradicting-digest early return | block deleted | `...Branch3DeployEventWithContradictingDigest...`, `...ImageRefMatchWithContradictingDigest...` |
| digest-equality check | block deleted | `...DigestBranchPromotesRegardlessOfEnvironmentEvidence` |
| image-ref check | block deleted | `...ImageRefBranchPromotesRegardlessOfEnvironmentEvidence` |
| branch 3 of `supplyChainDeploymentMatchesFinding` | round-1 condition restored | `...Branch3DeclaredOnlyDoesNotPromote...` + the two pre-existing operational-anchor tests |
| `recordSupplyChainEnvironmentEvidence` collision rule | call replaced with last-write-wins | `...DeployEventWinsAcrossDeploymentsInEitherOrder` |
| `normalizeSupplyChainEnvironmentEvidence` exact match | any non-empty maps to `deploy_event` | `...DecodesEnvironmentEvidence` + 3 promotion tests |
| payload decode of `environment_evidence` | key typo | `TestDecodeSupplyChainImpactFindingRowDecodesEnvironmentEvidence` |
| promotion is ANY-of, not ALL-of | `ANY` inverted to `ALL` | `...DeployEventWinsAcrossDeploymentsInEitherOrder` |
| blank-environment skip in the recorder | guard deleted | `TestRecordSupplyChainEnvironmentEvidenceSkipsBlankEnvironment` |
| `normalize` trims before comparing | `TrimSpace` removed | `...DecodesEnvironmentEvidence/padded_deploy_event` |

The last four rows exist because the audit found them uncovered.

The collision rule had only a direct helper test, which does not prove the
production path calls the helper — swapping the call in
`applySupplyChainRuntimeContext` for naive last-write-wins passed every package.
Nothing exercised the persisted-payload decode at all, so a typo in the payload
key would have left the reducer writing evidence no caller could ever read: the
silent-inertness shape, which the corpus cannot catch here either (#5836).
Inverting promotion from ANY-of to ALL-of also passed everything, which would
have silently dropped `deployed_image` from any finding with one corroborated
and one declared-only deployment — the exact multi-run shape the collision test
already models. And deleting the blank-environment skip passed too, which would
put a `""` key in `environment_evidence` while `uniqueSortedStrings` drops it
from `environments[]`, leaving the two collections disagreeing about their own
keys — contradicting the API reference this same change adds.

### The declared-only guard was silently defanged once

Worth recording, because the failure mode is invisible in a green test run.

`...Branch3DeclaredOnlyDoesNotPromoteRuntimeReachability` was originally written
with a fixture carrying a non-matching digest. That was harmless until the
contradicting-digest rule landed and moved a digest check to the *front* of the
promotion predicate. From then on the test short-circuited on the digest and
never reached the declared-vs-`deploy_event` rule it exists to prove — while
still passing.

Review caught it by mutation: deleting #5426's entire corroboration rule left
the whole `internal/reducer` package green. The fixture now carries no artifact
identity at all, so branches 1 and 2 are unsatisfiable and the contradiction
check cannot fire, leaving the corroboration rule as the only thing that can
decide the outcome. Re-running the same mutation now fails exactly where it
should:

```
$ go test ./internal/reducer -overlay=<promotion rule replaced with `return true`> \
    -run 'Branch3|RegardlessOfEnvironmentEvidence|ContradictingDigest'
--- FAIL: TestBuildSupplyChainImpactFindingsBranch3DeclaredOnlyDoesNotPromoteRuntimeReachability
    RuntimeReachability = "deployed_image", want declared-only branch-3 evidence
    to withhold deployed_image
```

The general lesson: when a predicate gains an early-exit branch, every existing
test whose fixture can trigger that branch stops testing whatever came after it,
and nothing goes red to say so.

`...Branch3DeclaredOnlyDoesNotPromoteRuntimeReachability` asserts both halves of
the corrected design: the finding is not promoted, *and* it still carries the
environment (labelled `declared`) and the `cicd_run_correlation` evidence hop.
That second assertion is what fails if anyone re-implements this by gating the
match again.

**No pre-existing behavior test needed editing.** The first attempt required two
edits in `supply_chain_impact_operational_anchor_test.go` to keep those tests
reaching their own subject; both are reverted, and they now pass unmodified
against `main`'s versions.  That is the cleanest available proof that the collateral
damage is gone: the tests that had to be bent to accommodate the wrong design
need no accommodation from the right one.

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

### Golden corpus, and why it cannot assert this

The gate is green on this branch at `e3eab3d6f`:

```
$ COMPOSE_PROJECT_NAME=env5426gate7 bash scripts/verify-golden-corpus-gate.sh
summary: 506 pass, 0 required-fail, 2 advisory-warn
=== PASS: B-7 golden corpus gate green (elapsed 156s, budget ceiling 1800s) ===
```

Both advisory warns are phase timing under a deliberately raised
`GATE_COLLECTOR_SETTLE_SECONDS=75`, not assertion failures. (Without that
override the run trips a known collector-settle flake — `only 17 credentialed
collector source(s) landed facts; want >= 18` — which is #5831, not this
change.)

Every asserted snapshot value is expected to be unchanged from `main` here, and
is: this change gates only the promotion, so every deployment that matched
before still matches, and the corpus's one impact finding matches no branch-3
deployment either way. The persisted payload is byte-identical too: the field is optional and the
corpus finding's evidence map is empty, so `omitempty` drops the key entirely.

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

Two follow-ups came out of this, and they are separate surfaces:

- **#5835** — environment corroboration rides only on the baked fields, so it
  never reaches the read-time-resolved `runtime_context` that OS-package
  findings actually serve.
- **#5836** — the corpus grows a branch-3 fixture so the promotion gate is
  actually asserted end to end. Until that lands, the gate would stay green if
  someone re-tightened all three match branches uniformly; only the unit tests
  catch that today.

No-Regression Evidence: the touched hot path is
`applySupplyChainRuntimeContext` in `go/internal/reducer/supply_chain_impact_runtime.go`,
which gains one `map[string]string` per finding that records an environment and
one O(matched deployments) pass over a slice the same function already iterates
twice. No query, index, Cypher, or SQL shape changes, and no new I/O. Covered by
`cd go && go test ./internal/reducer ./internal/query ./internal/mcp ./cmd/reducer -count=1`
and the B-7 golden-corpus run cited above, whose phase timings stayed inside
their baselines apart from the two advisory warns explained there.

No-Observability-Change: no metrics, spans, or status fields are added or
altered, and no new failure path is introduced. The gate withholds a promotion
rather than rejecting a deployment: the deployment still matches, so it keeps
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
