# Collector / Reducer Briefing

Date: 2026-04-17
Audience: future reviewer or design partner
Status: working note, not for commit

## Executive Summary

Two conclusions matter for the next milestone:

1. `go/internal/reducer/inline_followup.go` is architecturally outdated but
   dormant on this branch. It should not be patched now.
2. The future AWS collector should be a new collector service, not an expansion
   of the existing Git ingester.

## Current Branch Reality

- The live reducer path is the durable queue-driven path.
- Shared and code-call acceptance-unit handling were hardened in the reducer.
- `RunInlineSharedFollowup` is not wired into the live reducer service path on
  this branch.
- The platform freshness model is now based on bounded acceptance keys:
  `(scope_id, acceptance_unit_id, source_run_id)`.

## Inline Followup Conclusion

The dormant inline shared-followup path still thinks in repo/run/generation
terms and uses a fixed repo/run sample to infer partitions. That no longer
matches the bounded acceptance-key model.

Why we are not patching it now:

- it is not a live production path on this branch
- widening the sample would only soften the symptom, not fix the abstraction
- redesigning it now would be speculative unless and until a future collector
  actually needs an inline fast path

If this path is ever revived, it must be redesigned to:

- require a full `SharedProjectionAcceptanceKey`
- discover work by acceptance unit, not repo/run sample
- use bounded pagination or capped widening scans
- fail loudly on saturation
- determine completion from the bounded acceptance slice, not a repo-scoped
  generation counter

## AWS Collector Conclusion

The future AWS milestone should start from service separation, not service
expansion.

Recommended position:

- Git is one collector family
- AWS should be another collector family
- both should feed the same facts-first projector/reducer pipeline

Do not:

- bloat the existing Git ingester into a multi-source collector
- introduce a second correctness model outside reducer-owned reconciliation
- assume `acceptance_unit_id == repository_id`

## Why This Matters

Git-shaped assumptions are survivable in the current branch because repository
is often the natural ingestion unit. AWS-shaped collection breaks that mental
model quickly.

Future AWS design needs to decide explicitly:

- source truth
- bounded scope
- generation semantics
- acceptance-unit semantics
- relationship between live cloud evidence and IaC/state evidence

Those decisions should exist before code structure is finalized.

## Default Platform Rule

The platform should continue to operate like this:

1. collectors observe source truth
2. collectors assign scope and generation
3. collectors emit durable typed facts
4. projector handles source-local work
5. reducer owns shared reconciliation and canonical writes

Any inline fast path is optional optimization only.

## Immediate Guidance For Future Work

If someone starts the AWS collector milestone next:

1. define the bounded work unit first
2. define acceptance-unit semantics before reducer integration work
3. keep the AWS collector as a separate service
4. treat inline shared followup as out of scope unless a real latency target
   demands it
5. if an inline fast path becomes necessary, redesign it around acceptance keys
   before wiring anything

## Companion Notes

Longer investigation and constraint notes live here:

- `docs/internal/2026-04-17-inline-shared-followup-investigation.md`
- `docs/internal/2026-04-17-future-aws-collector-design-constraints.md`
