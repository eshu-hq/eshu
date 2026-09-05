# Monitor Owned PRs

Monitor only PRs owned by this drive. Do not rebase, fix, or resolve comments
on a peer-owned PR without explicit assignment. Read review bodies and issue
comments as well as inline threads; an inline-only scan misses findings.
Keep outward-facing comments and review requests within user authorization.

- Rebase open PRs on `main`; resolve conflicts immediately (PRs merge fast).
  During CI/review waiting, check `gh pr view <n> --json mergeable,headRefOid`
  about every 60 seconds. If `mergeable` becomes `CONFLICTING` or `UNKNOWN`
  for more than one poll, fetch, rebase on `origin/main`, rerun affected focused
  proof, and repeat the entrypoint promotion sequence before pushing
  with `--force-with-lease` and restarting the poll. A rebase never permits a
  direct push. Active agents merge constantly; a green check snapshot can
  become stale while the PR is waiting.
- CI is done only after **two consecutive stable reads of the full check set**
  (`gh pr checks <n> --json bucket` shows `pending == 0` AND the total count is
  unchanged across two polls). GitHub registers large check sets in waves, so a
  single `0-pending` read is a false "done" — never merge or claim green on it.
  Report CI status with the exact query used, **and the head SHA it was read
  against**. Re-resolve the SHA at read time, never at watcher launch: a poller
  that captured `$SHA` when it started keeps reporting that commit after a
  force-push, so both of its "stable" reads can honestly describe a head that no
  longer exists. A read whose SHA is not the current head is stale — discard it.
  Before merging, confirm local `HEAD` equals the PR's `headRefOid`.
  Only the orchestrator runs the late
  `make pre-pr`; dispatched executors run focused proof only (see
  [Orchestration, PR, And CI Discipline](../../../../CLAUDE.md)).
- **Cancelled is not failed, but a cancelled BLOCKING gate is not self-healing.**
  A cancelled job does not re-run itself — only a new push or an explicit rerun
  restarts one; a cancelled required gate strands
  `required-gates-complete` once every other check finishes, and nothing
  re-triggers it. Re-run those explicitly (`gh run rerun <run-id>`) and confirm
  the aggregate re-fires. An aggregate failure whose only cause is cancelled
  children says nothing about the diff: read the failing job's steps before
  concluding, since a hung setup step — a package install stalling until the
  runner kills it — presents identically to a real failure.
- **Keep the machine quiet for the live lanes.** They bind fixed host ports and
  saturate CPU, so a build, a second gate, or another worktree's `make pre-pr`
  running alongside produces a RED that is starvation, not a defect. One session
  lost a 936s run this way and drew two wrong conclusions from it before a quiet
  re-run passed the identical diff in 451s; the same gate takes 139s unloaded.
  Serialize gate runs, and before treating any live-lane failure as real, check
  what else was running during it. Detect a live run by the lock in
  `scripts/lib/live-gate-lock.sh`, by `pgrep -f verify-golden-corpus-gate`, or
  by port 15432 being bound — `pgrep -f "make pre-pr"` has failed to match a
  running gate and been misread as the process having died.
- Fetch ALL inline + bot review comments:
  `gh api repos/eshu-hq/eshu/pulls/<n>/comments`. Treat every reviewer
  uniformly — **codex (`chatgpt-codex-connector[bot]`), GitHub Copilot
  (`github-copilot[bot]`), Cursor, Claude, and human reviewers** — by reading
  the comment body and the cited `file:line`, not by trusting (or skipping) a
  bot label. Address each; resolve a thread only after the referenced code is
  fixed in HEAD (classify each unresolved thread `fixed` / `unchanged` / `ambiguous`;
  resolve only `fixed` threads through the review-thread API). Duplicate findings across bots: fix the code once, resolve
  both threads. When bots disagree, trust the code and the project rules.
- **If GitHub Copilot returns "couldn't review any files"** on its first pass,
  re-request the review immediately via `gh pr edit <n> --add-reviewer @copilot`
  (reviewer re-requests use `gh pr edit`, not `gh pr review`) and poll again
  before proceeding. An empty first review is not a pass — it is a failed
  request that must be retried.
- **If no external reviewer produces a review, dispatch a replacement.**
  A quota message is NOT a review and NOT a retryable failure — re-requesting it
  only burns polls. Recognise the shape rather than the exact wording: Copilot
  posts "reached their quota limit", `chatgpt-codex-connector[bot]` posts
  "reached your Codex usage limits". More than one can be down at the same time,
  and when all of them are, a PR silently drops to zero external review while
  every other gate stays green.

  "Every external reviewer" means every reviewer actually requested on this PR,
  not every reviewer available to the repository. Verify it: for each
  requested reviewer, the fallback triggers only when it has either posted a
  quota/unavailable message or failed to produce a review. Cursor and Claude
  emit no quota shape, so if either was requested and has not answered, chase
  that review first — the fallback is for when no external reviewer can answer,
  not for when the two GitHub-App bots happen to be the only ones asked.

  The trigger is the set of reviewers that actually produced a review being
  empty after retries — whatever the reason, quota or silence. When it is, the
  coverage MUST be replaced, not waived: dispatch a subagent in a separate
  context to run the full `eshu-code-review` against the final diff. Ask it to
  evaluate requirements against evidence, covering proof tier, all required passes
  including hostile read, cross-pass contradiction check,
  severity/confidence/disposition per finding, and the generated-artifact,
  docs, private-data and evidence scans.

  This replacement is **additional to the author-side review gate, never the same
  verdict relabelled**. Re-pointing at the author-side verdict would leave the PR with the
  one review this clause exists to prevent.

  The replacement verdict MUST be posted as a PR comment with required verdict
  evidence, reviewing model, and the base/head SHAs it covers — so the independence
  claim lands in GitHub truth rather than in a PR body the dispatching agent
  wrote about itself.

  The replacement must be **independent in a way the dispatching agent cannot
  self-certify**. Two requirements are hard, because both are checkable in any
  harness: it MUST run in a fresh context that did not write the diff, and its
  verdict MUST be posted as a PR comment naming the reviewing model.

  Prefer a **different model lineage than the author's wherever the harness
  offers one** — a different lineage catches what a single family rationalizes
  away ([Agent Orchestration Model](../../../../docs/internal/agent-orchestration.md)
  reaches for a cross-family model as an independent verifier for that reason).
  This is a preference, not a MUST, because some harnesses expose only one
  lineage: Claude Code's Agent tool offers Anthropic models only, so a
  Claude-authored diff cannot get a cross-lineage reviewer there. When the
  harness cannot offer one, say so in the PR comment — "this review shares the
  author's lineage" — rather than leaving the reader to assume independence the
  setup could not provide. An unsatisfiable MUST would simply be skipped, and
  the clause skipped would be the one replacing external review.

  Self-review is the last resort, not a fallback of convenience. Delegation is
  unavailable when the harness lacks the capability or active instructions
  prohibit its use. Taking that branch requires stating the specific limitation
  AND owner confirmation of that exception (including an existing explicit
  grant for that exception) before proceeding — an agent that wrote the
  diff, reviewed it itself, and self-certified that delegation was unavailable
  has produced no independent review at all.

  State in the PR which reviewers were unavailable and what replaced them.
  Merging with "the bots were out of credit" and nothing in their place is a
  rule violation, not a shortcut: the merge bar is a reviewed diff, and where
  that review comes from is an implementation detail.
- Check GHA on every PR. Enumerate **every** check's state, not just the green
  rollup; on red, root-cause (no symptom patch), fix, rerun. While checks are
  pending, poll the PR about every 60 seconds for merge conflicts and new review
  threads instead of staring only at the check watcher. If the rollup is stale
  or empty after a push, poll the underlying workflow runs for the head SHA
  before treating CI as absent. A clean PR *diff* can still inherit pre-existing
  red: this repo has no required-status-check enforcement, so whole-module Lint
  Go / Go tests that an earlier sequential gate masked (e.g. a failing "Verify
  hot-path evidence" step that aborts the job before Lint Go) surface only on
  the first PR to pass those earlier gates. Fix inherited blocking debt when it fits the authorized scope; otherwise
  report the blocker and obtain direction. Do not merge through red because
  "it is not my diff." The only red you may
  carry is a check on a documented advisory allowlist (state it explicitly, e.g.
  the Docker `verify-reproducibility` build-determinism job); treat every other
  red as blocking.
