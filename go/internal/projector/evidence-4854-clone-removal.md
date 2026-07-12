<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# Evidence — #4854 drop per-fact Clone() in buildProjection

Tracked performance/observability evidence for the change at
`go/internal/projector/runtime.go` (buildProjection per-fact loop). Retained in
the repository so future agents have the proof without re-deriving it.

## Change

`buildProjection` deep-cloned every input fact (`facts.Envelope.Clone`, a
recursive `Payload` map copy) solely for read-only validators and builders, then
discarded the clone; `buildCanonicalMaterialization` already consumes the
un-cloned `inputFacts`. The loop now borrows `inputFacts[i]` instead of cloning.
Every consumer in the loop reads `Payload`/`SourceRef` only and never retains an
alias (record/intent metadata is deep-copied in `payloadAttributes` and
`entityMetadataFromPayload`), so sharing the caller's `Payload` map read-only is
safe.

## Measurement

- Baseline (backend/version): in-process Go projector unit path, no graph
  backend; `go test ./internal/projector -bench BenchmarkProjectionCloneRemovalProof -benchmem -count=8 -benchtime=20x`.
- Input shape: 5,000-fact mixed fixture (alternating content-record and
  content-entity facts each carrying nested map+slice payloads, plus one
  repository fact and one quarantined codegraph_repository fact) —
  `buildLargeMixedProjectorFixture`.
- Terminal counts: identical projection on both shapes — canonical + content +
  reducer-intents + quarantine rows byte-identical
  (`TestBuildProjectionBorrowMatchesClonePathEquivalence`, `reflect.DeepEqual` of
  the full `projection` struct, `0/0`).

 Performance Evidence: OLD (Clone) ~10.1 ms/op, 21.87 MB/op, 113954 allocs/op vs NEW (Borrow) ~7.8 ms/op, 17.41 MB/op, 78954 allocs/op over the 5,000-fact fixture.
 Benchmark Evidence: BenchmarkProjectionCloneRemovalProof {Clone,Borrow}, -benchmem -count=8 -benchtime=20x, delta -23% ns/op, -20% B/op, -35000 allocs/op (-30.7%).
 No-Regression Evidence: TestBuildProjectionBorrowMatchesClonePathEquivalence proves 0/0 byte-identical projection borrow-vs-clone; TestBuildProjectionDoesNotMutateInputFactPayloads proves no consumer mutates the now-shared input Payload; 549 projector+runtime tests green, race lane green, golden-corpus-gate unit + static verify green.
 No-Observability-Change: no metrics, spans, logs, or status surfaces are added or altered; the existing eshu_dp_projector_stage_duration_seconds{stage="build_projection"} span/metric still bounds this path and now reports the lower cost.

## Why safe

The change is output-preserving (proven `0/0`), touches no committed B-12
snapshot or `query_shapes`, and adds no concurrency: `inputFacts` is immutable
after claim and not shared across projections, and `Clone()` never provided
synchronization (it fully reads the same map to copy it). `facts.Envelope.Clone`
remains referenced elsewhere and is not removed.
