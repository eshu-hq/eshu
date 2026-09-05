# Live GitHub Review State

## Mandatory Live PR Truth Collection

When reviewing an open PR, collect live GitHub truth before the verdict and
again after every pushed review fix. Do not rely on the compact `gh pr view`
review summary; it can omit inline thread bodies. Read review bodies and issue
comments too; findings may exist without an inline thread.

For first-time pre-PR review of a branch that has not been published as a PR,
record `no PR exists yet` with the branch, base SHA, and head SHA instead of
treating absent PR APIs as a blocker. After creating the PR, immediately collect
the live review-thread and check-rollup snapshots below and re-run the review if
GitHub reports new comments, red checks, mergeability problems, or base drift.

Required commands or equivalent GraphQL/API calls:

```bash
gh pr view <pr> --json headRefOid,baseRefOid,mergeable,mergeStateStatus,reviewDecision,statusCheckRollup
gh api repos/<owner>/<repo>/pulls/<pr>/reviews
gh api repos/<owner>/<repo>/issues/<pr>/comments
gh pr checks <pr> --json name,state,bucket,link,startedAt,completedAt,workflow
gh api graphql -F owner=<owner> -F repo=<repo> -F number=<pr> -f query='<reviewThreads query>'
```

Classify results exactly:

- unresolved latest-head review threads are findings until fixed and resolved;
- outdated threads still need a disposition when they named a real bug;
- queued or in-progress checks are pending, not failures;
- completed red checks are concrete CI findings and need log evidence;
- skipped checks are acceptable only when the workflow condition explains them.

For every red GitHub Actions check, fetch the job log or artifact, name the
failing step, and connect the fix to a local reproducer. If the failure can only
be proven in Actions, say why and add the smallest static workflow-contract
mirror that can catch future drift.
