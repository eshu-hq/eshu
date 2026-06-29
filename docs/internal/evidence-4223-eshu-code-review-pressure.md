# Eshu Code Review Skill Pressure Evidence

This note records the process-documentation TDD pressure cases for the Eshu
code-review skill. It is evidence for the skill authoring workflow, not a
runtime contract.

## RED Baseline

Baseline inspected: `.agents/skills/eshu-issue-driver/SKILL.md` at
`origin/main` commit `96d25701f`.

The existing issue-driver review gate requires a severity-tagged review, but it
embeds many concerns in one checklist. Under pressure, an agent can satisfy the
text with one flattened review paragraph and still miss Eshu-specific proof
gaps:

| Scenario | Baseline failure |
| --- | --- |
| Dead-code replay pressure | Review can say "golden gate passed" without naming the exact dead-code cassette/golden assertions that would fail on missing `live_by_consumer`, `unknown`, or evidence items. |
| Graph write/retract timeout pressure | Review can accept unit/static truth and miss the need for backend-required or scaled replay to expose graph-write timeout budgets. |
| Reducer/materialization/search-index long-pole pressure | Review can cite replay truth while missing queue depth, p95/p99 latency, and pprof attribution requirements. |
| Parser regression pressure | Review can accept collector cassettes even though cassettes bypass the collection/parse stage where the regression lives. |
| Bootstrap restart DDL pressure | Review can pass without fault-injection or live restart proof because ordinary replay never restarts into active backend work. |
| NornicDB backend-version pressure | Review can rely on current local NornicDB source while Eshu still pins an older backend image, silently skipping backend-version and hot-path proof. |
| Generated-inventory pressure | Review can miss a generated artifact advertising a nonexistent command until bot review catches it. |
| Golden false-green pressure | Review can weaken B-12 candidate-item assertions to bucket counts and still claim golden coverage. |

Missing enforced artifacts in the baseline:

- a required `Proof tier decision`;
- all required pass notes, including scope/diff integrity and hostile-read pass;
- a cross-pass contradiction check;
- confidence labels and explicit finding disposition;
- exact NornicDB fast-path flags and fallback expectations;
- a tracked follow-on handoff when stronger proof belongs after the PR.

## GREEN Target

The new `eshu-code-review` skill must force a reviewer to produce all of the
following before push, PR create/update, or merge-readiness:

- one selected proof tier with rationale;
- required passes for scope/diff integrity, correctness/truth,
  performance/storage/query shape, reliability/concurrency/security/workflow
  hygiene, and hostile-read loopholes;
- severity-tagged findings with file/line evidence, confidence, disposition,
  and no silent drops;
- NornicDB/Cypher review that compares the pinned Eshu backend against current
  NornicDB docs/source and names expected fast paths or fallbacks;
- a follow-on validation section that would route backend-version
  gaps instead of assuming current backend behavior.
- an adversarial wording check that blocks in-scope proof deferral, stale-diff
  review sequences, false-green proof, and author-intent assumptions.

## REFACTOR Checks

After implementation, re-run the pressure scenarios by reading the completed
skill against each row above. The skill passes only if an agent following its
template cannot claim ready while omitting the proof tier, required pass notes,
hostile-read loophole check, NornicDB fast-path evidence, generated-artifact
scan, golden/cassette sufficiency check, private-data scan, or
tracked follow-on decision.

Result: `.agents/skills/eshu-code-review/SKILL.md` requires each artifact above.
The output template forces the proof tier, scope/diff integrity,
correctness/truth, performance/storage/query shape,
reliability/concurrency/security/workflow hygiene, hostile read, cross-pass
comparison, severity/confidence/disposition, Eshu failure-class check, NornicDB
pinned/current source check, expected fast-path/fallback evidence,
generated-artifact scan, private-data scan, verification evidence, and
tracked follow-on routing before a reviewer can mark the diff ready.

Follow-up pressure found during PR review: a sympathetic self-review can carry
author intent into the verdict and miss wording that lets future agents defer
in-scope proof. The skill now forces rejection-mode review, a Pass 4 hostile
read, explicit hostile-read classes, and hard blocks for missing in-scope proof.

Second follow-up pressure found during PR review: CI-wait rebases can change the
final head after the review gate already ran. The issue-driver now requires
affected gates and `eshu-code-review` to rerun on that new head before pushing
or continuing toward merge.

Third follow-up pressure found during PR review: `origin/main` can advance
cleanly without a merge conflict and still change the reviewed base/head. The
issue-driver now reruns affected gates and `eshu-code-review` whenever the base
or head changes for any reason during CI wait.

Fourth follow-up pressure found during PR review: a DONE checklist that only
names P0 and P1 can let an unresolved P2 slip through even when the main review
gate blocks that state. The issue-driver now requires P2 to be zero or every P2
to be fixed inline or linked to a tracked repository issue before closure.

Fifth follow-up pressure found during C-6 review: the compact PR view can omit
inline review-thread bodies, so a self-review can claim comments are handled
while missing an unresolved P1. The code-review skill now requires the
review-thread GraphQL/API result before any comments-addressed verdict, and it
requires a disposition for outdated threads that named a real bug.

Sixth follow-up pressure found during C-6 CI: a workflow path can be green
locally while the Actions job fails because token, permission, or event-context
setup differs. The skill now requires every workflow-only fix to include a local
workflow-contract mirror or an explicit Actions-only proof explanation, plus a
final `gh pr checks --json` snapshot after the last push.

Seventh follow-up pressure found during C-6 replay coverage: capability and
product-claim gates need named false-green attacks, not only generic hostile
read questions. The skill now lists blank proof kind/ref, unknown capability
ids, stale anchors/counts, proof-signal/catalog drift, live issue-state drift,
and authored-scenario-without-green-sibling-gate as required checks.

Eighth follow-up pressure found while comparing the skill to a dedicated
code-reviewer prompt: the Eshu-specific checks were strong, but a reviewer could
still receive an unbounded chat handoff with no immutable base/head, no no-PR
disposition, and no read-only boundary. The skill now requires a concrete review
packet, forbids reviewer-side mutation before the verdict, records proof already
established as evidence instead of praise, and forces a final readiness/staleness
decision.
