# #5426 — mutation audit of the corroborated-vs-declared rules

Companion to
[#5426 — corroborated versus declared-only deployment environments](5426-corroborated-vs-declared-environment.md).
That document carries the design narrative, the shipped behavior, and the
verification runs. This one carries the mutation audit: which rule each mutant
deletes, which test catches it, and the one case where a green test was proved
not to be covering the rule it was credited with.

## Every rule is mutation-audited

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
| `normalize` trims before comparing | `TrimSpace` removed | `TestNormalizeSupplyChainEnvironmentEvidenceTrimsPadding` |
| caller requires a non-empty `SubjectDigest` | guard deleted | `...WithoutSubjectDigestDoesNotPromote` |
| image_sbom priority survives a declared-only deployment | gate reverted to `main`'s | `...DeclaredOnlyKeepsImageSBOMPriority` |
| declared-only stops labelling a finding reachable | gate reverted to `main`'s | `...Branch3DeclaredOnlyDoesNotPromote...` (documents the consequence; the promotion assertion in the same test is what the mutant hits first) |

Six of these rows exist only because the audit found them uncovered; the rest
confirmed guards that were already in place.

The collision rule had only a direct helper test, which does not prove the
production path calls the helper — swapping the call in
`applySupplyChainRuntimeContext` for naive last-write-wins passed every package.
Nothing exercised the persisted-payload decode at all, so a typo in the payload
key would have left the reducer writing evidence no caller could ever read: the
silent-inertness shape. The B-7 corpus now catches that one too — the
`list_supply_chain_impact_findings` shape reads `environment_evidence.prod` off
the live MCP response, so a payload-key typo drops the field and reds the gate
(see the companion
[golden-corpus record](5426-golden-corpus-coverage.md)) — but the unit test
stays, because it names the failure in milliseconds instead of in a
three-minute corpus run.
Inverting promotion from ANY-of to ALL-of also passed everything, which would
have silently dropped `deployed_image` from any finding with one corroborated
and one declared-only deployment — the exact multi-run shape the collision test
already models. And deleting the blank-environment skip passed too, which would
put a `""` key in `environment_evidence` while `uniqueSortedStrings` drops it
from `environments[]`, leaving the two collections disagreeing about their own
keys — contradicting the API reference this same change adds.

The last two rows came from the eighth review round, and one of them is a
cautionary case. A padded-value subtest had been added to pin `normalize`'s
`TrimSpace`, and the audit table credited it — but that subtest enters through
`supplyChainDeploymentContextFromEnvelope`, and `payloadStr` already trims
before `normalize` ever sees the value. The subtest therefore could not
distinguish a trimming `normalize` from a non-trimming one, and the mutant
survived while the table claimed it died. The rule is now pinned by a direct
call that bypasses `payloadStr`. Adding a test is not the same as covering a
rule; only the mutant settles it.

The `SubjectDigest` precondition is a pre-existing rule rather than a new one —
`main`'s `len(deployments) > 0` gate leaned on the same caller guard — but this
change is what writes the precondition down, so it pins it too. Without the
guard a finding with no artifact identity at all reaches `deployed_image`.

## The declared-only guard was silently defanged once

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
