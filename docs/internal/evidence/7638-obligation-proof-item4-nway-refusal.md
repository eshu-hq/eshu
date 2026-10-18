# #7638 item 4: N-way refusal on bulk onboarding (Refs #7638)

Status: **recorded — IMMATERIAL.** Owner-recorded verdict transcribed to the
tree. No production code changed in this step. No new measurement was run for
this doc.

## Verdict source

Issue item 4 requires the N-way refusal product for an onboarding wave of N:
each claim pays the catalog scan and the stale-memo check, is refused with
`catalog_changed` or `no_memo_baseline`, holds for one lease, and repeats.
The single-refusal baseline in the issue is 0.0519 s and 3,338 buffers at
900 scopes (k0), with exactly one `listRepositoryCatalogQuery` call.

The verdict below is the owner's, recorded in the
2026-10-17T20:07:08Z issue comment (linuxdynasty) on #7638:

> Item-4 N-way refusal product verdict: IMMATERIAL (61 wave obligations, all
> refused exactly once, lease fit at ~2%) — recorded locally, will fold into
> the next PR.

This doc is that fold. The three stated numbers — 61 wave obligations, each
refused exactly once, lease fit at ~2% — are transcribed verbatim. The raw
run data (per-claim walls, buffer counts, the lease-fit arithmetic and its
denominator) is held by the owner and is NOT_CHECKED here. Do not cite this
doc for per-claim cost figures beyond the three numbers above.

## What IMMATERIAL settles

- The 61-obligation wave showed no repeat-claim loop: every obligation was
  refused exactly once, so the held-and-repeat cycle the issue feared did
  not materialize at this wave size.
- At ~2% of lease, the refusal product leaves the lease budget to admitted
  passes; no drain-cycle starvation was observed.
- The arbiter-named optimization from the issue (one catalog load and
  fingerprint per drain cycle, with an early hold of the remaining claims
  when the fingerprint is already known stale) is therefore NOT_REQUIRED
  unless a later, larger wave shows material cost. That optimization still
  needs its own proof if it is ever built.

## Code anchors (unchanged behavior)

- Catalog scan per claim: `listRepositoryCatalogQuery` in
  `go/internal/storage/postgres/backfill/catalog.go`.
- Refusal-to-hold mapping (`catalog_changed`, `no_memo_baseline` held one
  callback per lease): `go/internal/storage/postgres/activation/README.md`
  and the obligation `runner.go` in
  `go/internal/reducer/maintenance/obligation/`.
- Live refusal coverage stays in `targeted_live_test.go` under
  `go/internal/storage/postgres/activation/` (real `catalog_changed` and
  `no_memo_baseline` refusals held, then completed after the epoch pass).

## Upgrade path

A committed built-binary measurement supersedes this transcription: an
onboarding wave of stated N with per-claim wall and buffers, the N-way
product, and the lease-fit arithmetic with its denominator shown. Re-open
the optimization question if any later wave exceeds a material share of
the lease.
