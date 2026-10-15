# AGENTS.md — go/internal/collector/repo/gitlab

## Read first

1. `go/internal/collector/repo/gitlab/README.md` — listing contract
2. `docs/internal/design/7765-gitlab-group-discovery.md` — the decision,
   especially "GitLab listing client" and "Rejected alternatives"
3. `go/internal/collector/repo/git/AGENTS.md` — the `git -> leaf -> model`
   import direction

## Rules

- This package is a leaf. `git` imports it; it must never import `git`
  (not even `git/rest`: the retry set here is 429/502/503/504 only, while
  the shared helper retries every 5xx).
- It lists and classifies only. Do not add selection, scope identity,
  observation writes, graph writes, retirement, or deletion here.
- The request shape is a contract: `include_subgroups=true`,
  `with_shared=false`, `order_by=id`, `sort=asc`, fixed `per_page=100`,
  offset paging, no `archived`, no `min_access_level`, `PRIVATE-TOKEN`
  auth. Change one and update the httptest suite in the same edit.
- Completeness mirrors `listGitHubOrgRepositoriesFrom` exactly: only an
  empty page ends the listing, and it is complete only when that page has
  no `rel="next"` and the count is below the limit. Reaching the limit is
  always incomplete.
- Every error carries a closed `failure_class`. Transport and decode
  faults are terminal; only 429/502/503/504 retry, at most 3 times per
  page. Never widen the retry set without a test that shows the fault it
  can no longer surface immediately.
- The sleep is injected so tests never wait out a backoff. Production
  passes `time.Sleep`.
- Keep every file under 500 lines; split before approaching the cap.
