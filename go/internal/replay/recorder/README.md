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
`collector-kubernetes-live` was the first; `collector-aws-cloud` is the first
pseudonymizing recorder. Collectors whose fact payloads can carry a secret
pass `Options.RedactKeys`; most pass none because fact payloads are
secret-sanitized by their collector (the HTTP boundary is redacted by the
input tape, R-4). Secret-sanitized is not identifier-free: account ids, ARNs,
names, hostnames and addresses do reach envelopes, which is what the next
section is for.

## Pseudonymized record mode (#6965 Phase 3)

A recording of a real estate must still be committable. With
`Options.Pseudonymize` set to a `recordpseudo.Config` (a corpus `Key` plus the
collector's field `Policy`), `Run` wraps the source with `recordpseudo.Wrap`
so every identifier reaches the cassette as a keyed, structure-preserving
pseudonym: accounts stay 12 digits in the reserved `0000` range, ARNs keep
their grammar, names become `n` + 11 hex, addresses land in RFC 5737, and so
on (the shapes are tabled in `go/internal/replay/recordpseudo/README.md`).
Equal raw tokens under one key give equal pseudonyms, so joins between facts
and between sibling cassettes survive -- which is why the key is per corpus,
not per recording.

`Options.RequirePseudonymization` makes `Run` refuse, before polling the
source, unless a usable key is configured: a collector that records real
estates sets it so a missing key can never produce a raw cassette.
`Options.OnPseudonymized` receives the `recordpseudo.Report` (counts, opaque
and unclassified field paths, IPv4 address and slot-collision counts,
account collisions, unlisted ARN type tokens, key fingerprint -- never a
value) for the collector to log.

Two belts guard the write, pseudonymized or not:

1. `recordpseudo.Verify` runs on the canonical bytes between `Canonicalize`
   and `WriteFile`. It scans with the private-data gate's own alternatives
   and refuses any candidate that is neither a documented safe form nor a
   pseudonym this run produced; a refused recording leaves no file behind.
   The reserved `0000` account form is admitted only when this run minted
   it, which closes the residual of a raw account that happens to start
   with `0000`.
2. The load-back through `cassette.LoadFile`, as before.

The cassette records the key's 8-hex fingerprint in
`pseudonym_key_fingerprint` (never the key), so a gate can check that every
cassette of one corpus was recorded under one key.

What is classified and what is not: payload fields and scope metadata go
through the collector's policy per key (an unlisted key is made opaque and
its path reported). The composite envelope fields the recorder maps in
`toScope`/`toFact` -- `scope_id`, `partition_key`, `stable_fact_key`,
`source_record_id`, `source_uri` -- are substitution-only by design: the
collector composes them from tokens that also appear in classified fields,
from one-way hashes, or from structural URI text, so there is no field key
to classify them by; `Verify` is the belt for them. The residuals that
remain (Keep-class free text, AWS-suffixed customer hosts refused rather
than admitted) are listed under "Declared limits" in
`go/internal/replay/recordpseudo/README.md`.
