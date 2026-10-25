# Babysit PRs

You own every PR you opened until it merges or closes. Detecting a problem and
not acting on it is the same as not detecting it.

Triggers: a PR you created is open; review bots are running; CI is in flight;
`main` moved under a branch you have out.

## Steps

1. **Arm the PR watcher in the same turn as every `gh pr create` (and every
   push that opens or revives a PR you own).** Do not hand-roll a poll loop.
   Run the committed command over all your open PRs, in the background:

   ```bash
   cd go && go build -o "$TMPDIR/pr-watch" ./cmd/pr-watch
   "$TMPDIR/pr-watch" -dir "$WORKTREE" 9101 9102      # -dir = repo root; run_in_background / Monitor
   ```

   On Muse Code, run it under the Monitor tool; each output line is one event.
   Rebuild the watched set on each new PR by restarting it with every PR you
   own — a watcher armed only at session start misses later PRs, and a purely
   per-PR watcher misses two of your PRs colliding on the same path. Never end
   the turn that created a PR without a live watcher covering it. Its last
   output line is always a `WATCH` line; if the process exits, read that line
   and restart it. It polls every 60 seconds by default, the same cadence as
   [monitoring.md](../../eshu-issue-driver/references/monitoring.md) and
   [sustained-drives.md](../../eshu-issue-driver/references/sustained-drives.md),
   and it reads head, rollup, check count, and threads fresh on every poll.
   Act on the first event rather than waiting for the full check set.

   The watcher pins the GitHub account from the checkout path (`linuxdynasty`
   under `~/personal-repos`; pass `-account` anywhere else), so it never reads
   as the wrong user. Full event contract: `go/cmd/pr-watch/doc.go`.

   | Event | What to do |
   |---|---|
   | `CI-FAIL job=… tail=…` | Reproduce that gate locally, fix, push. Includes a cancelled blocking gate. |
   | `CONFLICT state=… recipe=…` | `4a`: every conflicted file is a named recipe file; follow `.agents/skills/eshu-code-review/references/conflict-recipes.md` and run its check. `count-choice-lines`: some conflicted file is not a recipe file; count your choice lines (row 4a at 10 or fewer in 2 or fewer files, else 4b). `agent_rule_files=N` means run `bash scripts/verify-docs-contradiction.sh`. `rebase`: clean test merge; follow [rebase-verify.md](rebase-verify.md). `unknown`: the test merge could not run; read the conflict yourself. |
   | `NEW-THREAD`, `NEW-REVIEW`, `NEW-COMMENT` | Any author, including this account and bots that commented unrequested. Read the body and fix first (steps 5–6). |
   | `HEAD-MOVED … source=foreign` | Someone else pushed. Fetch and rebase onto the remote head before you push; never force. `source=local` is your own push. |
   | `MERGED` / `CLOSED` | Stop pushing to the branch. A follow-up is a new branch and PR. |
   | `GREEN` | Rollup SUCCESS, nothing pending, every check read, every merge-rule context from the gate registry present and successful, head and total unchanged on two reads (step 4). It does not mean mergeable: judge threads and the merge bar (step 7). A red anywhere already came as `CI-FAIL`. |
   | `ERROR` | A read failed; it keeps polling. A repeating error needs a look. |

   Tell every subagent/workflow you dispatch that owns a PR to arm the same
   command for its PRs; never take over a peer-owned PR to watch it.

2. **Scope "yours" correctly.** The GitHub account is shared by concurrent
   agents, so `--author` is not ownership. A PR is yours when you can point at
   the local worktree and branch you created it from. If your only evidence is a
   remote branch you did not push, someone else is driving it — do not touch it,
   and do not resolve its threads.

3. **Read the check set by status, not by conclusion.** The watcher already
   does this. For a one-off read, remember that a running check has a null
   conclusion, so any filter shaped like `.conclusion == null` counts
   in-flight checks as done and reports a false green.

   ```bash
   gh pr view "$PR" --repo eshu-hq/eshu --json statusCheckRollup --jq '
     [.statusCheckRollup[]] as $c
     | { total:   ($c | length),
         pending: ([$c[] | select((.status // "") == "QUEUED"
                                or (.status // "") == "IN_PROGRESS"
                                or (.state  // "") == "PENDING")] | length),
         failing: ([$c[]
                    | select(((.conclusion // "")
                        | test("FAILURE|TIMED_OUT|CANCELLED|ACTION_REQUIRED|STARTUP_FAILURE|STALE"))
                      or ((.state // "") | test("FAILURE|ERROR")))] | length) }'
   ```

   Match the failure states as a set, not as the two or three that come to
   mind. A check run can end `ACTION_REQUIRED`, `STARTUP_FAILURE`, or `STALE`,
   and a status context can be `ERROR` — none of which is `FAILURE`. Listing
   only the obvious ones reports `failing: 0` over a red check set and lets the
   merge step below treat it as clean.

4. **CI is complete only after two consecutive stable reads, with the merge-rule
   contexts present.** The watcher's `GREEN` event is this rule: it also needs
   the registry's ruleset-required contexts (`go-core-complete`,
   `go-race-complete` today) to exist and succeed, because a fresh PR shows only
   its always-on external checks for minutes before the Actions wave. Both
   `pending == 0` and an unchanged `total` across two reads. Check sets on this
   repo run to well over eighty entries and register in waves, so a single
   zero-pending read lands in the gap between waves and is a false done. State
   which query you used when you report.

5. **Read the thread bodies, not the review states.** The watcher prints each
   unresolved thread with its body. A bot review marked
   `[COMMENTED]` routinely carries unresolved inline findings, and the
   conversation tab count does not always agree with the API. Fetch the threads
   directly:

   ```bash
   gh api graphql -f query='query($o:String!,$r:String!,$n:Int!){
     repository(owner:$o,name:$r){ pullRequest(number:$n){
       reviewThreads(first:100){ nodes{
         isResolved isOutdated path line
         comments(first:1){ nodes{ author{login} body } } } } } } }' \
     -F o=eshu-hq -F r=eshu -F n="$PR" \
     --jq '.data.repository.pullRequest.reviewThreads.nodes[]
           | select(.isResolved == false)
           | "\(.path):\(.line)\t\(.comments.nodes[0].author.login)"'
   ```

   Judge each thread by its body and the cited `file:line` against current HEAD,
   not by which bot wrote it. Every reviewer is treated the same way.

6. **Fix first, reply after.** Land the fix, then answer the thread describing
   what changed. Never reply announcing an intention. A derivable-figure P3 is
   the exception (see `resolve-review-threads`).

7. **Merge only on the real bar.** Zero failing checks, and every thread from
   every reviewer addressed and resolved by you. Advisory lanes still pending is
   acceptable; an unread thread is not. If a PR merges with a comment
   unacknowledged, open a fresh branch and PR to address it.

## Two failures that look like problems and are not

- **`required-gates-complete` red with no other failing check.** That is the
  `cancel-in-progress` and `if: always()` publisher stamping a false failure in
  `required-gates.yml`, not a real gate failure. Confirm no other check is red
  before chasing it.
- **`required-gates-complete` absent entirely, and `mergeable` stuck.** `main`
  is guarded by a ruleset rather than branch protection, `--admin` cannot bypass
  it, and concurrent PRs starve the aggregator. The fix is to stop the churn and
  let it settle, not to force anything.

## Reply

Your report must contain:

- PR number, head SHA, and the check query you ran
- the two stable reads that justify calling CI complete, or an explicit
  statement that it is still in flight
- every review thread, its reviewer, and its disposition: fixed and resolved,
  resolved as P3, no action, or open with a reason
- merge decision and what it rests on
