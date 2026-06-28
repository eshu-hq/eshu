# recorder

Records a live collector run as a canonical replay cassette — the write side of
the deterministic replay framework (epic #4102, R-2).

## Why

Cassettes were hand-authored JSON: a dozen-plus implicit fields per fact,
error-prone, and — critically — a hand-authored `object_id` is a *guess* at what
the real collector emits. The real kuberneteslive collector derives
`object_id = facts.StableID("KubernetesLiveObject", …)`, an opaque hash, so a
hand-authored human-readable id can silently diverge from production and the
golden gate cannot catch an `object_id` format regression (#3928).

Recording fixes this structurally: the **real collector runs** during record, so
every emitted field — `object_id` included — is captured exactly as production
produces it. There is no separate "derive the id" step to get wrong.

## What it does

`Run(ctx, src, opts)`:

1. polls `src` (any `collector.Source`) for one batch, until `Next` reports the
   batch exhausted;
2. captures every emitted `facts.Envelope`, aborting if a generation's
   post-stream `FactStreamErr` fires (never writes a partial cassette);
3. writes the batch as a **canonical** cassette to `opts.Path` — keys sorted,
   `observed_at` collapsed to a sentinel, `generation_id` derived from
   `scope_id`, configured `RedactKeys` redacted — then loads it back through the
   real replay loader as a guard.

It performs **no durable commit**, so recording needs only the collector's live
credentials, not Postgres. The written cassette replays credential-free.

## Determinism

Re-recording the same input is byte-identical. The package test proves a full
`record → replay → record` cycle produces identical bytes even when the second
pass is fed a different raw `generation_id` and `observed_at`, because
canonicalization normalizes both. `testdata/pilot.recorded.json` is a committed,
reviewable example (regenerate with `-update`).

## Collector wiring (`-mode=record`)

A collector adds record mode symmetric to its existing `-mode=cassette`: build
the live source, then call `recorder.Run` with the `-cassette-file` path.
`collector-kubernetes-live` is the pilot. Collectors whose fact payloads can
carry a secret pass `Options.RedactKeys`; most pass none because fact payloads
are already collector-sanitized (the HTTP boundary is redacted by the input
tape, R-4).
