# GitHub Actions delivery inbox

The listener verifies the GitHub HMAC before storing bounded, immutable
delivery identities keyed by `X-GitHub-Delivery`. The coordinator plans every
configured GitHub Actions scope that matches the delivery repository. It uses the existing
open-target admission guard, verifies the exact durable workflow item for each
scope, then ACKs the delivery with its current fencing token. Another open run
cannot satisfy the ACK check. A duplicate delivery cannot reopen completed work
or alter the event identity. The store returns
`webhook.ErrActionsDeliveryConflict` for a mismatched retry; the listener maps
it to HTTP 409 while exact duplicates remain HTTP 202. The first identity and
status remain intact. No raw payload or credential is persisted.

The claim conflict domain is a delivery ID. Two coordinators use `SKIP LOCKED`
and fencing tokens to claim disjoint deliveries and reject stale ACKs. Each
claim takes at most 100 deliveries with a one-minute lease. Within one claim,
selected deliveries with the same exact `(repository, repository_id)` receive
one persisted `batch-v1:` handoff key. The key is assigned only while empty;
release, reap, and reclaim preserve it. A delivery inserted after that claim's
selection receives a fresh key on a later claim, even if an earlier group is
still collecting. Concurrent coordinators can select separate groups for the
same repository, so one group per repository is a bound within a selected
claim, not a promise for an entire burst.

The coordinator plans one full-snapshot work item per configured scope in a
group. Its `webhook-group-v1-` plan key hashes the persisted handoff key,
scope, and normalized tenant boundary. This namespace is distinct from the
older per-delivery plan keys, so exact work for a legacy anchor delivery cannot
cover the other group members. Each scope has an independent run identity:
if one scope finishes while another is blocked, the missing scope can still be
admitted on retry. Every delivery is ACKed separately with its current fence
only after the group's exact durable work exists for every scope. Admission errors
retry up to eight claims and then fail terminally; expired claims are reaped in
batches of 100. A reap returns bounded requeued and exhausted counts from the
updated row statuses. The coordinator records each count in
`eshu_dp_ci_cd_run_webhook_reaps_total` and warns with aggregate counts on
exhaustion; reaps are separate from handoff attempts. Benign blocked or
inactive-target releases wait 30 seconds
without consuming that attempt budget. The queued/claimed repository index lets
scheduled GitHub fallback defer while any delivery is pending, including a
blocked delivery whose next claim is not visible yet. GitLab scheduling retains
its existing path. The bootstrap DDL and migration 236 must match.
