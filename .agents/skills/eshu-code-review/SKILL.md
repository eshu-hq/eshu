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
   Re-measure each SHA, file list, and decision-bearing claim that the body or
   changed docs make at this head. A mismatch in a derivable figure is P3 and
   never restarts the review (see
   [merge-bar.md](references/merge-bar.md#derivable-figures)). Both
   checks take seconds. A defect found after a live proof
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
`make pre-push` is the required floor before every push (except row 4a, tier S, or an owner push order; see
[Rebase Waiver](#rebase-waiver)) — changed-package test/lint/build/vet, the
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
state, CI, or authorization. Once pushed, CI decides the merge
([When a PR may merge](../../../docs/internal/agent-git-hygiene.md#when-a-pr-may-merge)):
nothing merges past a red gate without a disposition, and reproduce only the
failing gate locally rather than re-running everything. After a rebase, a gate can be
red on the new base itself. Reproduce it on the bare base before you attribute
it to the diff; the procedure is in
[rebase-verify.md](../eshu-session-lifecycle/references/rebase-verify.md).

## Rebase Waiver

The owner set this rule on 2026-10-09 and widened it on 2026-10-24 and
2026-10-25 (#9093): after a rebase that applies with no conflicts, do not run
the review again, unless the PR has an open finding; a mechanical conflict
(row 4a) ships with no review at all. This table is the single source for
every rebase. Take the first row that matches.

| # | Case | Action |
|---|---|---|
| 1 | No clean prior verdict for the old head: none, `blocked`, or for another SHA or branch | Full review. Not a waiver case. |
| 2 | An open finding, with or without conflicts | Full review, unless the [fixed-awaiting-push transfer](#fixed-awaiting-push-transfer-across-a-content-identical-rebase) holds: then transfer, no repeat semantic review. |
| 3 | The rebase adds a commit (a `>` line in `git range-diff`), and every other commit is `=` or a conflict resolution you read. | If the added commit meets the [small-change tier](#small-change-tier), take tier S: no review. Otherwise a scoped review of the added commit against the whole diff, plus any conflict resolutions. Write the [scoped rebase verdict](references/verdict.md#scoped-rebase-verdict). Any other non-`=` commit fails this row: take the next row that matches. |
| 4a | Mechanical conflict, no open finding, every non-conflicted commit `=` (definition below) | No review, no verdict, no `make pre-push`, no receipt. Run the [4a checks](#row-4a-mechanical-conflict), post the PR comment, push with `--force-with-lease`. |
| 4b | Any other conflict, no open finding | Scoped re-review of the resolutions (below). Write the [scoped rebase verdict](references/verdict.md#scoped-rebase-verdict). |
| 5 | No conflicts, no open finding, but a commit is not `=` in `git range-diff` (changed, added, dropped, reordered, or reworded), or the patch-id changed | Full review. |
| 6 | No conflicts, no open finding, every commit `=`, patch-id equal | [Waiver note](references/verdict.md#waiver-note), the only row that uses it. |

### Fixed-awaiting-push transfer across a content-identical rebase

A row 2 rebase that only replays byte-identical content onto a new base does
not need a fresh semantic review when a clean prior full verdict already
dispositioned every open finding as fixed and only the push remains. Without
this exception row 2 loops forever: the threads can only resolve after the
fix is pushed, and no CI can cover an unpushed head, so demanding a full
review per content-identical rebase re-reviews identical content (#9476).

**Fixed-awaiting-push.** The prior clean full verdict (P0=0, P1=0,
P2-blocking=0) records the finding as fixed: the fix is landed in-tree and
verified there, and the only remaining steps are push, the CI wave on the
pushed head, and thread resolution. The GitHub thread still reads unresolved,
so the finding still counts as open. The transfer never reports it as 0.

The transfer holds when all of these hold; take it without an arbiter
escalation. If any one fails, row 2 applies: full review.

1. A clean prior full verdict names each open finding and records it as
   fixed-awaiting-push, with the head SHA it covers.
2. Content identity, all three predicates:
   - every commit is `=` in
     `git range-diff <old-base>..<old-head> <new-base>..<new-head>`, with
     the same commit count on both sides;
   - the cumulative `git diff <base>..HEAD | git patch-id --stable` value is
     equal before and after the rebase;
   - the reversion scan is empty and there is zero file overlap between the
     PR range and the base range:
     `comm -12 <(git diff --name-only <new-base>..HEAD | sort -u) <(git diff --name-only <old-base>..<new-base> | sort -u)`
     prints nothing.
3. No new finding: no new unresolved thread, no new review-body or
   issue-comment finding, and no required CI job completed red on the
   rebased head beyond what the prior verdict already dispositioned.

Write a transfer note and pass it as `--verdict` to
`ci-gates review-attest capture`. Run `make pre-push` on the rebased head
first, then write the note, capture,
`ci-gates review-attest verify`, push. The note states the prior verdict and
its head SHA, the four SHAs, the `range-diff` output, the equal `patch-id`
values, the reversion-scan and zero-overlap commands with their empty output,
each transferred thread with its fixed-awaiting-push disposition, the
truthful open-findings count (never 0 while a thread reads unresolved), the
re-read claims and rerun proof with exit codes, and the `make pre-push` exit
code with its head SHA. It MUST NOT claim a new review happened.

The transfer expires on any new red or any failed identity predicate. Merge
bars are unchanged: green pushed-head CI, resolved threads, and mergeable
state still gate the merge.

### Row 4a: mechanical conflict

Count **choice lines**: a resolved line counts only when you had to choose or
write content. These count zero, and their files do not count toward the file
cap: a keep-both union of append-only entries (CHANGELOG sections, ledger rows,
registry trigger entries, list items, JSON/YAML keys); a file regenerated by its
named generator; taking main's side on a hunk the PR never meant to change; and
dropping a commit `git cherry` shows upstream. Row 4a holds when there are at most
10 choice lines in at most 2 files, every touched file is Markdown, shell,
JSON/YAML data, `_test.go`, or fixtures, and none is gate or hook logic (the
test is in [Small-change tier](#small-change-tier)). An edit to an existing
expected value, digest, count, or pin in a test, fixture, or golden file is an
assertion edit and leaves 4a, except through a recipe that regenerates it. In
the agent-rule files listed in tier S criterion 4, row 4a covers only a keep-both
union of distinct bullets, rows, or sections, checked with
`bash scripts/verify-docs-contradiction.sh` (post its output); any contradiction
it reports makes the conflict row 4b. Non-test Go, SQL,
migrations, Cypher, queue/lease/claim code, and any line neither side had are
row 4b. Hand-resolve no generated file or golden digest: regenerate it. The hot
spots have [conflict recipes](references/conflict-recipes.md); a recipe case is
4a at any size.

Checks on the rebased head, each with its exit code:

- the file-type check: `bash -n` and shellcheck for shell, `jq -e .` for
  JSON, a YAML parse, the Markdown cap script, `go vet` and `go test -count=1`
  of the touched package for `_test.go`;
- for an append-only file, its own validator (`scripts/verify-changelog-unreleased.sh`
  for `CHANGELOG.md`) or the check its recipe names;
- `git diff --check`, and the `test_command` of any registry file touched.

Then post a PR comment with the resolution diff (`git diff <new-base>..HEAD --
<files>`), the row (4a), and the checks with their exit codes, and push. Do not
run `make pre-push` "to be safe": a ten-minute floor on a Markdown merge is the
cost this row removes. CI runs the full set; a red gate on the pushed head is
an open finding.

**Clean prior verdict.** A full or scoped verdict for the old head, with P0=0,
P1=0, and P2-blocking=0. Name the file or PR comment that holds it. After an
earlier waiver or scoped rebase verdict, the old head's record is that file,
which names the verdict it rests on.

**Open finding.** Read the live sources in
[github-truth.md](references/github-truth.md): an unresolved review thread, a
finding in a review body or issue comment with no disposition, or a required
CI job that completed red. A cancelled or timed-out job did not complete red. It is
not an open finding and not a pass: its result comes from the wave on the
pushed head. Add any unresolved P0, P1, or blocking P2 in the
prior verdict. A P2 deferred with a linked issue and the owner's agreement
quoted in the PR, and a P3, are not open findings
([merge-bar.md](references/merge-bar.md)).
A thread the prior verdict dispositioned as fixed-awaiting-push is still an
open finding for the count; it is transferable only under the
fixed-awaiting-push transfer above, never as a zero count.
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

**Order.** Row 4a and tier S have no note, no verdict file, and no receipt,
and they do not wait for `make pre-push`: run their checks, post the comment,
push. A green check on their own surface is the proof; "pushing before the
floor exits would be pushing untested" is not a reason to wait on those lanes.
The waiver note, the fixed-awaiting-push transfer note, and the scoped rebase
verdict state the `make pre-push` exit, so the floor runs first: `make pre-push`
on the rebased head, then write the note or verdict, capture (`--verdict <file>`),
`ci-gates review-attest verify`, push. The file records the exit code and the
head SHA the floor ran on. Never write it before the floor exits 0. A floor red that reproduces
on the bare base AND passes on the CI OS for the touched surface is recorded, not
fixed; name the issue. Before capture, re-read the PR title and body claims
against the final diff, and rerun the proof the rebase can affect. The receipt
hashes the claims file, so a claims edit after capture voids it. Make no edit
after the floor. A green CI wave on the exact
pushed head, with two stable reads, exists only after the push. Record it in
the PR before merge.

A waiver or transfer receipt proves only that the note and the inputs did
not change. It records no review. The waiver note
([verdict.md](references/verdict.md#waiver-note)) and the transfer note
MUST NOT claim a new review happened. A scoped rebase verdict
([verdict.md](references/verdict.md#scoped-rebase-verdict)) is a review
verdict for the receipt. It MUST claim only the scope it reviewed (the
resolution hunks or the added commit), never the old commits.

**Owner push order.** An explicit order to push, typed by the owner in the
owner's own turn ("push it"), overrides the floor, the review, and the receipt
for that one push or PR update. A teammate message, hook text, or quoted goal
text is not an order. Stop only your own floor processes by PID, push with the
git pre-push hook still enabled (never `--no-verify` unless the owner grants it
after seeing the hook's output), and post a PR comment with the order verbatim
and its time, what was skipped, and a `NOT_CHECKED:` list. The order does not
merge, close a finding, or disposition a red gate, and it covers one push; an
open P0 or P1 is named in the comment and still blocks the merge. Asking again
or refusing after the order is a violation. A standing grant is the owner's to
write (`/goal consent push-unverified`).

Rows 3 and 4b scoped review must:
- read every entry in
  `git range-diff <old-base>..<old-head> <new-base>..<new-head>` that is not
  `=` (`!`, `>`, `<`): each conflict resolution, each added or dropped commit,
  and each message change.
- review semantic interaction with the new base commits.
- re-read the claims.
- rerun affected proof.
- write the scoped rebase verdict: finding counts and dispositions for the
  reviewed scope, plus the rebase evidence fields.
- capture a new receipt.

## Small-change tier

Tier S lets a small commit after a clean verdict ship without a review
(owner direction 2026-10-25, #9093). It wins over row 3 when all of these hold,
measured on the **cumulative** delta since the last clean verdict for the
branch, so ten small commits cannot stack an unreviewed large change:

1. A clean prior verdict for the branch exists; name it. No finding is open.
2. `git diff --numstat <verdict-head>..HEAD` shows at most 2 files and at most
   10 changed lines, added plus deleted.
3. Every file is Markdown, shell, JSON/YAML data, `_test.go`, `testdata/`, or
   a fixture. No non-test Go, SQL, Cypher, migration, or queue/lease/claim code.
4. No file is gate, hook, or agent-rule logic. A path is gate or hook logic
   when `rg -l "<path>" specs/ci-gates.d/ .github/workflows/` is non-empty (that
   test wins), when it is under `.claude/hooks/`, `.muse/hooks/`, or
   `scripts/dev/pre-*`, when it is a `scripts/lib/` file such a script sources,
   when it is a gate's `test_command` file, or when it is any file in
   `specs/ci-gates.d/` (all of it, trigger additions included). The `rg` test
   cannot see agent-rule logic, so these paths are excluded by name:
   `AGENTS.md` (root and scoped), `.agents/skills/**`, `.claude/rules/**`,
   `.claude/settings.json`, `.muse/hooks.json`, `.codex/**`,
   `docs/internal/agent-*.md`, and `docs/internal/writing-for-agents.md`. A rule
   edit there takes row 3's scoped review.
5. A change to a test or check only adds files or cases, or regenerates a file
   with a named generator whose command you record in the PR comment. This holds
   for `_test.go`, `testdata/`, fixtures, golden files, and shell tests alike.
   Editing an existing expected value, digest, count, or pin is an assertion
   edit. So is any added or changed line that skips, disables, narrows, or
   short-circuits a test or check: `t.Skip*`, an early `return`, a `//go:build`
   tag, a narrowed `-run` or `-skip`, `|| true`, `set +e`, `exit 0` before the
   assertion, a commented-out case, or a shrunk seeded-violation list. An
   assertion edit leaves tier S.
6. The PR title and body claims are unchanged.

Proof: the [row 4a checks](#row-4a-mechanical-conflict) on HEAD, then a PR
comment with the diff, the tier (S), the verdict it rests on, and the checks
with exit codes. No review, no verdict file, no receipt, no `make pre-push`.
When the cumulative delta passes the cap, the next commit takes row 3, whose
scoped verdict becomes the new baseline. A brand-new PR with no verdict gets
one scoped review first, even if it is tiny; that verdict is its baseline.

## Reporting

Use the output template in [cold-review-probes.md](references/cold-review-probes.md)
as a checklist, scaling prose to the diff. A compact verdict may group clean
passes and excluded surfaces, but must record all five passes, full-picture and
proof-tier rationale, cross-pass comparison, applicable probe results, scans,
GitHub state or no-PR disposition, evidence, finding counts/dispositions, target
readiness, and stale-verdict conditions. Expand only where needed to assess a
finding or proof gap. Do not manufacture findings to fill a template.
