---
name: eshu-code-review
description: Produces the push/PR/merge review verdict on an Eshu diff — proof-tier selection, the five review passes, severity findings, merge-bar readiness. Not for closing GitHub threads (resolve-review-threads).
---

# Eshu Code Review

Review the final work product against its requirements and evidence. Author
confidence, local memory, and file type do not establish safety. Process wording
can authorize consequential mistakes; review it as carefully as executable code.
Load the project skills whose contracts match the diff, using root skill routing.

## Review Workflow

1. Bind the review to the intended base/head, branch, target, acceptance criteria,
   changed files, proof actually run, and open findings. Build the bounded
   [review packet](references/review-packet.md) for a separate reviewer or a
   self-review; never substitute chat history for it. Before any expensive
   proof, run `eshu-publish`'s `check-shape.sh` on the PR body when one exists.
   Re-measure each count, SHA, and file list that the body or changed docs claim
   at this head. Both checks take seconds. A defect found after a live proof
   wastes that proof.
2. Map the changed flow, owners, consumers, invariants, and failure boundaries.
   Use the full-picture checklist in [cold-review-probes.md](references/cold-review-probes.md).
   Explain which runtime, concurrency, rollback, and operator concerns apply;
   group excluded concerns with a concrete reason. Missing applicable context
   is a P1 proof failure, or P0 if it risks private data, truth, deadlock, or main.
3. Select exactly one [proof tier](references/proof-tiers.md). Explain why its
   actual evidence covers every in-scope claim. A weaker test cannot substitute
   for required backend, scaled, or full-corpus proof.
4. Perform all five [review passes](references/review-passes.md): scope,
   correctness, performance, reliability/security/workflow, and hostile read.
   Load [runtime-surfaces.md](references/runtime-surfaces.md) only for the
   runtime, graph, query, capability, or performance surfaces it covers.
5. Apply every relevant adversarial probe from
   [cold-review-probes.md](references/cold-review-probes.md), checking the
   production subject rather than just a helper. Check
   [failure-classes.md](references/failure-classes.md), generated-artifact and
   private-data/AI-attribution scans, and contradictions between passes.
6. For an existing PR, collect [live GitHub truth](references/github-truth.md),
   including review bodies, issue comments, inline threads, checks, and current
   head/base. Before first PR creation record `no PR exists yet`; collect live
   truth immediately after creation and re-review any new findings or drift.
   Classify every review comment by the code it cites and the body it carries,
   never by author identity: a bot finding and a human finding bind equally.
7. Record findings and readiness using [verdict.md](references/verdict.md) and
   the [merge bar](references/merge-bar.md). Keep every finding's identity,
   severity, confidence, disposition, evidence location, violated contract, and
   closing verification through subsequent rounds.

A separate reviewer works read-only until the verdict is delivered. Use a
separate context when delegation is available and authorized. If it is unavailable
or the user explicitly requests self-review, name that mode and limitation. An
external-review replacement has additional independence requirements in
`eshu-issue-driver`; author-side review does not satisfy that second review.

This skill runs at **Workhorse tier** through `review-eshu`, and at **Deep
tier** through `review-eshu-deep` for the final review of a difficult diff
(concurrency, locking, queues, schema, or hot-path Cypher and SQL); see
[default model bindings by tier](../../../docs/internal/agent-orchestration.md#default-model-bindings-by-tier).
An orchestrator dispatches it to a reviewer role rather than reviewing inline.
Every harness has both roles and each role carries its model binding, so this
skill never names a model and a subagent never changes its own.

## Promotion And Evidence Reuse

After focused proof, complete a preliminary full review before push. Do not
begin promotion with any P0, P1, or blocking P2 finding. Fix those, rerun
affected proof, and repeat the full review. Deferred P2 findings need the linked
issue and owner agreement required by the merge bar; P3 does not restart the loop.

Capture the clean verdict's exact inputs with `ci-gates review-attest capture`.
The order is: proof, clean preliminary review plus that capture, `make
pre-push`, `ci-gates review-attest verify`, then push. For a waiver note,
`make pre-push` runs before the capture ([Rebase Waiver](#rebase-waiver)).
`make pre-push` is the required floor before every push — changed-package test/lint/build/vet, the
file cap, registry-selected static gates, and docs-contradiction. For
queue/lease/claim code, schema DDL, hot-path Cypher or graph writes, reducer
projection/materialization, or a package move, the orchestrator also runs one
serialized `make pre-pr` (`make pre-pr-full` for a move) before that push —
recommended for that risk class, not required otherwise; subagents never run
it themselves. After preflight, `ci-gates review-attest verify` replaces a
second full semantic review only when the receipt matches. Any changed base,
commit, tree, worktree, submodule, PR claim, review packet, or verdict
invalidates it: repeat affected proof and full review, then capture a new
receipt. A rebase is the exception: the table in
[Rebase Waiver](#rebase-waiver) sets its review, including the waiver.
Do not edit between verified attestation and push. A review receipt
reuses semantic review; it does not waive independent review, current GitHub
state, CI, or authorization. CI's `required-gates-complete` (with
`go-core-complete` and `go-race-complete`) is the actual blocking authority
once pushed — nothing merges while it is red, and reproduce only the failing
gate locally rather than re-running everything. After a rebase, a gate can be
red on the new base itself. Reproduce it on the bare base before you attribute
it to the diff; the procedure is in
[rebase-verify.md](../eshu-session-lifecycle/references/rebase-verify.md).

## Rebase Waiver

The owner set this rule on 2026-10-09. After a rebase that applies with no
conflicts, do not run the review again, unless the PR has an open finding. The
waiver covers a rebase that changes only the base. This table is the single
source for every rebase. Take the first row that matches.

| # | Case | Action |
|---|---|---|
| 1 | No clean prior verdict for the old head: none, `blocked`, or for another SHA or branch | Full review. Not a waiver case. |
| 2 | An open finding, with or without conflicts | Full review. |
| 3 | The rebase adds a commit (a `>` line in `git range-diff`), and every other commit is `=` or a conflict resolution you read | Scoped review of the added commit against the whole diff, plus any conflict resolutions. One verdict holds the review and the waiver fields. Any other non-`=` commit fails this row: take the next row that matches. |
| 4 | Conflicts, no open finding | Scoped re-review of the resolutions (below). |
| 5 | No conflicts, no open finding, but a commit is not `=` in `git range-diff` (changed, added, dropped, reordered, or reworded), or the patch-id changed | Full review. |
| 6 | No conflicts, no open finding, every commit `=`, patch-id equal | Waiver note. |

**Clean prior verdict.** A full or scoped verdict for the old head, with P0=0,
P1=0, and P2-blocking=0. Name the file or PR comment that holds it. After an
earlier waiver, the old head's record is that waiver note, which names the
verdict it rests on.

**Open finding.** Read the live sources in
[github-truth.md](references/github-truth.md): an unresolved review thread, a
finding in a review body or issue comment with no disposition, or a required
CI job that completed red. A cancelled or timed-out job did not complete red. It is
not an open finding and not a pass: its result comes from the wave on the
pushed head. Add any unresolved P0, P1, or blocking P2 in the
prior verdict. A P2 deferred with a linked issue and the owner's agreement
quoted in the PR, and a P3, are not open findings
([merge-bar.md](references/merge-bar.md)).
Before a PR exists, only the prior verdict's findings count. Record
`no PR exists yet`.

**Content.** Both conditions must hold, and the commit messages are part of
the content:

1. `git range-diff <old-base>..<old-head> <new-base>..<new-head>` prints every
   commit as `=`. The count is the same. The rebase does not add, drop,
   reorder, or reword a commit. It compares each commit's message and diff, so
   a dedent, a squash, a reworded message, or a closing keyword shows as `!`. A
   base edit within three lines of a hunk changes the context and also shows
   as `!`.
2. `git diff <base>..HEAD | git patch-id --stable` is equal for the old base
   and head and for the new ones. This is a second test on the cumulative
   diff. It is blind to whitespace and to messages, so it never replaces step 1.
   With an added commit, step 1 alone covers the old commits.

**Reversion scan.** `git diff <new-base>..HEAD` shows no hunk that reverts or
drops a line the new base added. Read the files both sides touched. List them
with `comm -12 <(git diff --name-only <new-base>..HEAD | sort -u)
<(git diff --name-only <old-base>..<new-base> | sort -u)`. This scan is
textual. A semantic collision with no shared hunk shows only in the merged-tree
vet and tests of `make pre-push`, and in CI.

**Order.** The waiver note states the `make pre-push` exit, so the floor runs
first: `make pre-push` on the rebased head, then write the note, capture
(`--verdict <note>`), `ci-gates review-attest verify`, push. The note records
the exit code and the head SHA the floor ran on. Never write the note before
the floor exits 0. Before capture, re-read the PR title and body claims
against the final diff, and rerun the proof the rebase can affect. The receipt
hashes the claims file, so a claims edit after capture voids it. Make no edit
after the floor. A green CI wave on the exact
pushed head, with two stable reads, exists only after the push. Record it in
the PR before merge.

A waiver receipt proves only that the note and the inputs did not change. It
records no review. The note ([verdict.md](references/verdict.md#waiver-note))
MUST NOT claim a new review happened.

Row 4 scoped re-review must:
- read every entry in
  `git range-diff <old-base>..<old-head> <new-base>..<new-head>` that is not
  `=` (`!`, `>`, `<`): each conflict resolution, each added or dropped commit,
  and each message change.
- review semantic interaction with the new base commits.
- re-read the claims.
- rerun affected proof.
- capture a new receipt.

## Reporting

Use the output template in [cold-review-probes.md](references/cold-review-probes.md)
as a checklist, scaling prose to the diff. A compact verdict may group clean
passes and excluded surfaces, but must record all five passes, full-picture and
proof-tier rationale, cross-pass comparison, applicable probe results, scans,
GitHub state or no-PR disposition, evidence, finding counts/dispositions, target
readiness, and stale-verdict conditions. Expand only where needed to assess a
finding or proof gap. Do not manufacture findings to fill a template.
