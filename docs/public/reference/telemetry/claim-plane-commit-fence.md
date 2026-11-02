# Claim-Plane Commit Fence Signals

When a collector's claim lives in the extracted claim plane
(`eshu-workflow`) instead of core Postgres, the ingestion commit cannot
lock the claim row on the fact transaction. It fences the commit across
services instead (#9376). The commit pins the claim on the plane before
the transaction opens, re-pins it every lease/3 while the transaction
runs, records a per (generation, work item) fence row inside the
transaction, and refuses to `COMMIT` once the latest pin deadline is
within 5 s of the store clock. Collectors whose claims stay in core keep
the in-tree fence and emit none of these signals.

- `eshu_dp_ingestion_claim_fence_rejections_total{reason}`: commits the
  fence refused. Terminal reasons end the claim:
    - `stale_fence`: the plane no longer holds the claim live at this token.
    - `not_found`: the plane does not know the claim.
    - `fence_token`: a higher token already committed this generation for
      the work item.
    - `fence_claim_mismatch`: the same token was committed under another
      claim. The plane forbids that, so treat any non-zero value as an
      integrity alarm.

    Retryable reasons leave the claim to retry:
    - `deadline`: the pin deadline was inside the margin at `COMMIT`.
    - `pin_unavailable`: the pin did not answer after three identical
      retries. No core transaction opened.
- `eshu_dp_ingestion_commit_repins_total{outcome}`: re-pins during a
  commit. `ok` extended the pin; `stale` cancelled the commit; `error`
  left the decision to the deadline check.
- `eshu_dp_ingestion_commit_pin_slack_seconds`: the latest pin deadline
  minus the store clock at the pre-`COMMIT` check. With the default 60 s
  lease, healthy commits sit between 40 s and 60 s. Samples creeping
  toward 5 s mean slow commits or a slow plane, before they fail.

No label carries a scope, generation, claim, or work item id. The
`ingestion.commit.claim_fence` span carries those as attributes
(`fence_mode`, `claim_id`, `fencing_token`, `work_item_id`,
`generation_id`, `commit_id`, `outcome`), and every refusal writes one
`claim-plane commit fence refused the commit` WARN log with the scope,
generation, work item, claim, reason, requested and stored tokens, and the
pin slack.

At 3 AM: a rising `fence_token` or `stale_fence` count means writers are
losing their claims before they commit. Check lease and reap settings on
the plane and the commit latency of that collector. A rising `deadline`
or `pin_unavailable` count with flat `stale_fence` points at the plane's
availability or clock skew, not at stale writers.
