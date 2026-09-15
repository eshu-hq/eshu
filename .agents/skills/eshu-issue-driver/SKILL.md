---
name: eshu-issue-driver
description: Drives an assigned eshu-hq/eshu issue or epic through implementation, review, promotion, and a verified merged/closed outcome — worktree per leaf, PR monitoring, to closure.
---

# Eshu Issue Driver

Use for assigned issues in `eshu-hq/eshu`. A request to edit skills or open a PR
alone does not authorize driving issues through merge and closure. Follow the
user's requested endpoint and existing session grants; skill invocation never
adds authorization for external actions or peer-owned work.

## Establish The Work Set

Require issue numbers or URLs from the request; do not invent them. Read each
issue's title, body, labels, and state. For epics (tracking labels, child task
lists, or sub-task sections), recursively enumerate children and deduplicate
leaves, stopping cycles. Restate each leaf's problem, acceptance criteria, and
affected flow, then present a numbered execution order.

Read-only exploration may proceed within the authorized scope. Continue to
implementation when scope and acceptance criteria are clear and already
authorized; ask only for unresolved decisions or work outside that scope.
Do not introduce a second plan approval after the user has authorized the work.
Check open PRs and recent commits for the same root cause before claiming a leaf.
Respect existing ownership; do not take over an active peer PR without assignment.

Create a worktree per leaf and verify its path before editing. Load the minimum
project skills covering its touched surfaces using root skill routing. Preserve
TDD for code, package ownership, graph/query truth, performance and concurrency
proof, private-data boundaries, and the prohibition on shared-worktree stashes.
Delegate independent work when available and authorized; executors run focused
proof and only the orchestrator runs the promotion gate.

## Implement And Promote Each PR

1. Implement and commit the authorized change with focused verification from
   `docs/public/reference/local-testing.md`. Runtime changes also require
   performance/no-regression and operator evidence. Use the root rules for
   proof order and selected gates.
2. Verify authentication before push/PR actions. If `gh` is unavailable, an
   equivalent installed GitHub connector may be used; identify that fallback
   in the evidence. Fetch `origin`, rebase on fresh `origin/main`, rerun proof
   affected by the rebase, and inspect the diff for unrelated reversions.
   For Go changes, run `cd go && go vet ./...` to catch test compilation drift.
3. Run the complete preliminary `eshu-code-review` on the rebased diff. Use
   separate-context review when available and authorized; otherwise explicitly
   identify self-review and its limitation. Fix P0/P1/blocking P2 findings,
   rerun affected proof, and repeat review until those counts are zero.
   Use that skill's finding schema, proof tiers, and merge bar as the single
   source of review policy.
4. Capture the clean review's inputs with `ci-gates review-attest capture`.
   Run `make pre-push` before every push — the required floor (changed-package
   test/lint/build/vet, file cap, registry-selected static gates,
   docs-contradiction). For queue/lease/claim code, schema DDL, hot-path
   Cypher or graph writes, reducer projection/materialization, or a package
   move, the orchestrator should also run one serialized `make pre-pr`
   (`make pre-pr-full` for a move) before that push — recommended for that
   risk class, not required otherwise. Keep the shared machine quiet for
   either live lane: coordinate ownership across worktrees/clones and inspect
   the live-gate lock and running gate process; absence of a `make pre-pr`
   process alone does not prove the machine is free. Never kill another
   session's gate.
5. Verify the receipt with `ci-gates review-attest verify` after preflight.
   Matching inputs reuse the preliminary semantic review. A changed base,
   commit, tree, worktree, submodule, PR claim, review packet, or verdict
   requires affected proof and a new full review/receipt before promotion.
   On preflight failure, diagnose and fix it, rerun affected proof, and obtain
   a clean preliminary review before another attempt. Deferred P2 and cosmetic
   P3 findings do not restart this loop. Once pushed, CI's
   `required-gates-complete` (with `go-core-complete` and `go-race-complete`)
   is the actual blocking authority; on a red check, reproduce only that gate
   locally.
6. With authorization already established for the act, push the reviewed head
   and verify the remote SHA equals local HEAD before PR creation/update.
   Use `--force-with-lease` for an authorized rebase of an already-pushed branch.
   No edits may occur between verified attestation and push. Make the PR title,
   description, and affected docs match the final change and its proof.
7. Collect current GitHub checks, mergeability, review bodies, issue comments,
   and review threads immediately after publication. During an authorized
   merge/closure drive, follow [monitoring.md](references/monitoring.md) for
   stable check reads, cancelled gates, findings, and unavailable reviewers.
   Every requested reviewer must have a disposition. If none produces a review
   after retries, obtain the additional independent review specified there;
   the author-side verdict cannot be relabelled as that replacement.
8. Merge only when that endpoint and act are authorized, checks are stably
   green, findings and threads are dispositioned, current base/head evidence
   remains valid, and independent review requirements are met. Confirm local
   HEAD equals the PR head, execute the merge, and verify `MERGED` through the
   GitHub API. Existing session authorization applies without repeated consent.

For long-running `/goal` drives, load [sustained-drives.md](references/sustained-drives.md)
for goal composition and bounded waiter/liveness mechanics. The skill does not
create a goal or schedule future work by itself.

## Defects Found During The Drive

Fix related defects inline when they fit the authorized change and its proof.
Use `eshu-code-review`'s merge bar to classify blocking and deferred findings.
A defect needing an owner design decision, unrelated projected-truth changes,
or unavailable infrastructure may need separate work. Obtain owner agreement
before creating a follow-up issue unless that act and scope are already granted;
link it to the originating issue and epic. Do not expand the drive silently.

## Completion Evidence

At the requested endpoint, report commands and results once with durable evidence
links; do not repaste an unchanged checklist every turn. A merged/closed drive
is complete only when:

- GitHub directly reports every assigned leaf and epic `CLOSED` and its PRs
  `MERGED`; use `gh issue view ... --json state` and `gh pr view ... --json state`.
- Every follow-up is closed or explicitly deferred with the owner's agreement.
- Current review-thread API results confirm no unresolved threads, and all
  review-body/issue-comment findings have dispositions. Inline comment REST
  output alone does not expose thread resolution state.
- The final review has P0=0, P1=0, P2-blocking=0. Each deferred P2 has a linked
  issue, owner agreement quoted in the PR, and its severity-table category per
  the merge bar. The verdict includes applicable proof and probes, all five
  passes, cross-pass comparison, generated/docs/private-data scans, and verification.
- Required independent replacement reviews appear as PR comments when used,
  identifying their model, base/head, and unavailable reviewers.
- The promotion record includes clean preliminary review counts, reviewed
  inputs, exact preflight command/result, post-preflight head and clean status,
  and a verified receipt or replacement full review. GitHub CI evidence uses
  two stable full-check reads tied to the current head, as monitoring specifies.
- Before closing an issue as fixed, the applicable full verification suite from
  `docs/public/reference/local-testing.md` has run with exact tool versions and
  captured results. A code inspection or unsupported prior summary is not proof.

If the request ends at PR creation, report the PR and pending CI/review state;
do not claim the stronger merged/closed completion conditions or continue into
an unauthorized merge.
