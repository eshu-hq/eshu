# #8608: First-Execution DUC Completion-Wake Bound (Decision: Accept)

Issue #8608 (part of #8018). A first-execution `deployable_unit_correlation`
item woken before it records its fingerprint can land succeeded-stale, with
convergence deferred to the next pass.

## Decision

Option (a): accept late convergence as the documented bound. Contract-doc
update only; no predicate, trigger, worker, batch, lease, or claim change.

The wake's in-flight arm keeps requiring a recorded fingerprint row. A
first-execution item whose pass appends rows after its read but before its
about-to-succeed fingerprint record is left unflagged and lands succeeded on
stale input; the next pass sees the recorded fingerprint mismatch and the
succeeded arm reopens it. Only a run that never gets another pass keeps the
stale success — the pre-#8294 one-shot bound for non-append input, already
accepted. The loss is delay, not truth.

## Why not (b): Inserted-gated flagging of fingerprint-less rows

Flagging fingerprint-less in-flight rows reintroduces the kill-recovery
evidence erasure fixed in #8601: the flag forces a redundant replay whose
migration-093-diverted ACK resets `attempt_count` to 0, erasing the
killed-claim reclaim evidence (`go/internal/storage/postgres/migrations/093_cross_scope_completion_queue.sql`,
`enforce_cross_scope_required_replay`). That failure was observed, not
hypothetical: a pass that inserted nothing flagged the admission-blocked
victim and the replacement's reclaimed attempt read back as the reset
(`TestCompletionWakeClaimedMissingFingerprintNotFlaggedLive`;
hermetic twin `TestCompletionWakeFlagArmRequiresRecordedFingerprint` was
RED pre-fix, GREEN post-fix). Inserted-gating does not rescue the design:

- An `Inserted > 0` pass still erases reclaim evidence on every genuine
  append that overlaps a mid-first-execution claim.
- Gating only the fingerprint-less subset on `Inserted` reintroduces the
  lost-wake non-convergence for exactly that subset: per-row inserts
  autocommit before the wake, so a pass whose wake is lost to a crash
  retries with `Inserted = 0` and never flags — the same late window as
  (a), plus the extra predicate complexity.
- With no recorded `repo_ids` there is no overlap predicate to scope the
  flag: the wake would have to flag every fingerprint-less in-flight item
  in scope, including ones that already read post-append input.

Preserving reclaim evidence while flagging fingerprint-less rows would need
a migration-093 trigger change or a provisional at-claim fingerprint — a
schema/claim-path change out of scope for this bound, with its own live
proof burden. Not pursued.

## Contract wording

- `go/internal/reducer/crossrepo/cross_repo_resolution_completion_wake.go`:
  `DeployableUnitCorrelationWaker` doc gains the first-execution bound
  paragraph (stale success lands, next pass reopens, one-shot exception).
- `go/internal/storage/postgres/relationship_store_completion_wake.go`:
  `ReopenDeployableUnitCorrelationsForRepos` doc gains the matching bound
  paragraph plus the pointer to the migration-093 erasure it avoids.

## Correlation-truth matrix (doc-only; behavior pinned by existing tests)

- Positive (stale success converges next pass): succeeded arm reopens
  overlapping succeeded items with mismatched/missing fingerprints —
  `TestReopenDeployableUnitCorrelationsForReposReopensOverlappingSucceeded`
  (hermetic) plus the live floor/missing-fingerprint proofs.
- Negative (first-execution item not flagged):
  `TestCompletionWakeFlagArmRequiresRecordedFingerprint` (hermetic) plus
  `TestCompletionWakeClaimedMissingFingerprintNotFlaggedLive`.
- Ambiguous (pass inserting nothing with a fingerprint-less victim — the
  #8601 kill-worker shape): stays unflagged by the same `IS NOT NULL`
  predicate; no `Inserted` gate consulted anywhere in the wake path.

## Phase 1 proof (hermetic only; this host holds no live gates)

- `go test ./internal/reducer/crossrepo/ -count=1` → ok, exit 0.
- `go test ./internal/storage/postgres/ -count=1` → ok (7.638s), exit 0;
  `*Live` cells skip without a database.
- `go vet ./internal/reducer/crossrepo/ ./internal/storage/postgres/` →
  exit 0.
- `gofmt -l` on both touched files → clean; `git diff --check` → clean.

No-Regression Evidence: #8608 changes godoc comments only; the wake
predicates, trigger, worker, batch, lease, and claim shapes are
byte-identical, and the full `crossrepo` and `storage/postgres` hermetic
suites pass on the branch (see Phase 1 proof above).

No-Observability-Change: #8608 adds no metric, span, log field, route,
worker, lease, queue domain, or runtime knob; the wake's existing
`eshu_dp_reducer_completion_wake_total` / `eshu_dp_correlation_reopened_total`
coverage is untouched.

## CI proof remaining (promotion, not this phase)

- Completion-wake live tier
  (`relationship_store_completion_wake_live_test.go`, incl.
  `TestCompletionWakeClaimedMissingFingerprintNotFlaggedLive`) on a
  Postgres backend.
- Fault-injection kill-worker cell (shard 3/4
  `killworkerdeployableunit`) confirming killed-claim reclaim evidence
  still reads above the kill-time value with this wording-only change.
- `make pre-push`, `eshu-code-review` + `review-attest`, then PR.
