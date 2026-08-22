# Babysit PRs

You own every PR you opened until it merges or closes. Detecting a problem and
not acting on it is the same as not detecting it.

Triggers: a PR you created is open; review bots are running; CI is in flight;
`main` moved under a branch you have out.

## Steps

1. **Arm one watcher over all your open PRs, not one per PR.** A per-PR watcher
   misses the interesting case, which is two of your PRs colliding on the same
   path. Poll on an interval of 30 seconds or less, and act on the first signal
   rather than waiting for the full check set to finish.

2. **Scope "yours" correctly.** The GitHub account is shared by concurrent
   agents, so `--author` is not ownership. A PR is yours when you can point at
   the local worktree and branch you created it from. If your only evidence is a
   remote branch you did not push, someone else is driving it — do not touch it,
   and do not resolve its threads.

3. **Read the check set by status, not by conclusion.** A running check has a
   null conclusion, so any filter shaped like `.conclusion == null` counts
   in-flight checks as done and reports a false green.

   ```bash
   gh pr view "$PR" --repo eshu-hq/eshu --json statusCheckRollup --jq '
     [.statusCheckRollup[]] as $c
     | { total:   ($c | length),
         pending: ([$c[] | select((.status // "") == "QUEUED"
                                or (.status // "") == "IN_PROGRESS"
                                or (.state  // "") == "PENDING")] | length),
         failing: ([$c[] | select((.conclusion // "") == "FAILURE"
                                or (.conclusion // "") == "TIMED_OUT"
                                or (.conclusion // "") == "CANCELLED"
                                or (.state      // "") == "FAILURE")] | length) }'
   ```

4. **CI is complete only after two consecutive stable reads.** Both
   `pending == 0` and an unchanged `total` across two reads. Check sets on this
   repo run to well over eighty entries and register in waves, so a single
   zero-pending read lands in the gap between waves and is a false done. State
   which query you used when you report.

5. **Read the thread bodies, not the review states.** A bot review marked
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
   what changed. Never reply announcing an intention.

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
  or open with a reason
- merge decision and what it rests on
