# Inline Shared Followup Investigation

Date: 2026-04-17
Status: working note, not a committed architecture decision
Scope: `go/internal/reducer/inline_followup.go`

## Summary

`RunInlineSharedFollowup` is a real design mismatch with the current bounded
acceptance-key model, but it is dormant on this branch. It is not wired into
the live reducer service path today. The correct action is to leave it
unpatched for now and redesign it before any future collector or reducer fast
path tries to use it.

## Why This Was Investigated

During the reducer hardening pass for acceptance-unit processing, a follow-up
question came up: does `inline_followup.go` have the same kind of visibility
and starvation risk as the shared and code-call runners?

The answer is yes in principle, but the severity is different because this path
is not currently active in production flow.

## Findings

### 1. The abstraction is out of date

The inline path is centered on:

- `repository_id`
- `source_run_id`
- `generation_id`
- a per-domain repo/run sample

That no longer matches the canonical bounded freshness key:

- `scope_id`
- `acceptance_unit_id`
- `source_run_id`

Relevant references:

- `go/internal/reducer/inline_followup.go`
- `go/internal/reducer/shared_projection.go`
- `schema/data-plane/postgres/011_shared_projection_acceptance.sql`

### 2. Partition discovery is based on a fixed head sample

`pendingPartitionIDs(...)` calls `ListPendingRepoRunIntents(..., 10000)` and
derives partition IDs from that first slice only.

This means:

- older unrelated rows can hide relevant work deeper in the repo/run queue
- the function cannot prove that all relevant partitions were observed
- widening the sample would improve visibility, but would not fix the wrong
  unit of truth

Relevant references:

- `go/internal/reducer/inline_followup.go`
- `go/internal/storage/postgres/shared_intents.go`

### 3. Progress detection is repo-scoped, not acceptance-unit scoped

The stall check uses `CountPendingGenerationIntents(repository_id,
source_run_id, generation_id, domain)`.

That creates a mismatch:

- partition discovery is repo/run sampled
- progress is repo/run/generation counted
- acceptance is actually decided by bounded acceptance unit

This is especially risky for future collectors where one repository may emit
multiple acceptance units, or where `acceptance_unit_id` is not repository
shaped at all.

### 4. The storage layer already has the better contract

The reducer/storage path already supports listing pending rows by bounded
acceptance unit:

- `ListPendingAcceptanceUnitIntents(...)`
- `shared_projection_acceptance` keyed by
  `(scope_id, acceptance_unit_id, source_run_id)`

That is the direction the code-call lane now uses successfully.

### 5. The path is dormant

Repo search during the investigation showed `RunInlineSharedFollowup` is only
referenced in its own file and test file on this branch. It is not part of the
live reducer service wiring.

This changes the recommended response:

- do not patch it as if it were a live incident
- do not redesign and merge speculative behavior just to tidy the file
- carry the conclusion forward as design input for future collector work

## Recommendation

Do not patch `inline_followup.go` now.

If inline shared draining ever becomes necessary as a latency optimization for
future collectors, redesign it before wiring it in.

## Required Future Redesign Rules

Any future replacement should:

1. Accept a full `SharedProjectionAcceptanceKey`
2. Require `ScopeID` explicitly
3. Discover work by bounded acceptance unit, not by repo/run sample
4. Use pagination or a capped widening scan on that bounded slice
5. Fail loudly on saturation instead of silently returning "done"
6. Determine completion from the bounded acceptance-unit slice, not from a
   repo-scoped generation counter

## What Not To Do

Avoid these partial fixes as the final design:

- increasing the `10000` sample limit
- paginating the repo/run query while keeping repo/run as the control key
- keeping wildcard scope matching
- coupling future collector fast paths to `repository_id ==
  acceptance_unit_id`

Those changes reduce symptoms but preserve the wrong control-plane boundary.

## Architectural Position

The default platform contract remains:

- collectors observe source truth and emit durable work
- reducer owns reconciliation and shared truth
- inline followup, if it exists at all, is an optional optimization layer and
  must not become a second freshness model

This matches the current direction for future collector families, especially
non-Git collectors where the authoritative ingestion unit will not necessarily
be repository-shaped.

## Future Execution Checklist

Use this checklist if a future collector milestone proposes wiring an inline
shared-drain fast path.

### Decision gate

1. Confirm there is a measurable latency problem that durable reducer draining
   alone cannot satisfy.
2. Confirm the fast path is an optimization, not a second correctness path.
3. Confirm the collector's bounded work unit is defined explicitly and is not
   being inferred from `repository_id`.

### Design gate

1. Define the exact `SharedProjectionAcceptanceKey` the fast path will use.
2. Require `ScopeID` in the contract. Do not allow wildcard scope matching.
3. Decide whether the fast path needs:
   - full bounded-slice loading with pagination
   - distinct partition-key enumeration for one acceptance unit
4. Define saturation behavior:
   - cap + explicit error and telemetry
   - no silent "done" result when visibility is truncated
5. Define the fallback:
   - when the fast path declines or saturates, durable reducer draining remains
     the source of truth

### Storage and query gate

1. Use acceptance-unit scoped reads, not repo/run reads.
2. Reuse or extend the existing acceptance lookup index and bounded listing
   path before adding new repo-scoped helpers.
3. Avoid loading full payload-heavy rows if only partition discovery is needed.
4. Add a storage API that matches the real question being asked:
   - "what partitions remain for this acceptance unit?"
   - "how many accepted-generation rows remain for this acceptance unit?"

### Runtime boundary gate

1. Keep collector ownership limited to source observation, scope assignment,
   generation assignment, and fact emission.
2. Keep reducer ownership over shared reconciliation and canonical writes.
3. Do not let a collector-specific fast path introduce a collector-specific
   freshness model.
4. Do not couple future collectors to `acceptance_unit_id == repository_id`.

### Telemetry gate

1. Emit explicit logs for:
   - acceptance key
   - domain
   - scan limit
   - remaining accepted rows
   - saturation or fallback reason
2. Add metrics for:
   - fast-path attempts
   - fast-path completions
   - fast-path saturation
   - fallback-to-durable-runner count
3. Ensure the fast path uses the stable `acceptance.*` log contract.

### Verification gate

1. Add regression tests for acceptance-unit visibility beyond the initial scan
   window.
2. Add tests where `acceptance_unit_id != repository_id`.
3. Add tests for multiple acceptance units emitted from one repository/run.
4. Add tests for explicit saturation behavior.
5. Run reducer package tests, relevant storage tests, and race checks before
   enabling the path.

### Rollout gate

1. Ship the fast path behind an explicit config flag.
2. Default to disabled until staging proves it improves latency without
   correctness drift.
3. Validate that fallback to the durable reducer path always preserves
   correctness.
