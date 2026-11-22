# Arbiter ruling: winners payload vs digest exclusions (#10880, 2026-11-21)

Recorded by the coordinator; the arbiter does not edit files. Arbiter:
`arbiter-eshu`. Branch `drive/10880-move-reducer-results` at `6bb41961a5`.

**Ruling: disposition (c)** — normalize the stored winners payload by
stripping the non-served volatile digest-excluded keys, and stage keyed
capture when the one served excluded key (`evidence_fact_ids`) changes.
Slices 0-2 do not merge until this lands with the proof below.

## Verified facts

1. Digest excludes `intent_id`, `cause`, `generation_id`,
   `evidence_fact_ids` (`winners_keyed.go:112`) plus
   `provenance.content_digest` (`writer_changed.go:42-55`), because they
   differ on every pass without the finding changing.
2. Slice 1 made `payload` a write-on-change ROW column projected wholesale
   from `source.payload`; the failing test's `Updated:1` proves the select
   output now contains excluded keys.
3. The select-side guard regex only matches `payload->>'key'`, so the
   wholesale copy evades it (guard passes vacuously on HEAD).
4. Capture skips digest-equal same-generation rows while the upsert still
   rewrites `fact_records.payload`. Generation flips are safe (fact ID
   embeds generation).
5. Only `evidence_fact_ids` is API/MCP-served among excluded keys
   (`findings_decode.go:83`); the others are never decoded.

## Decisions

1. (b) rejected: pre-slice-2 the list read served `refetch.payload` fresh
   from fact_records, so capture-skip staleness did not exist. Post-slice-2
   it serves `win.payload`, stale until the 10-minute audit — a new
   freshness regression that also breaks steady-state `Repairs=0`.
2. Pure (a) rejected: staging on any excluded-key change stages every
   finding every pass (`intent_id`/`cause` churn by design), defeating
   #9805 economics for bytes no reader decodes.
3. Adopted (c): in the shared winner-select body (full, keyed, audit),
   project payload as
   `(source.payload - 'intent_id' - 'cause' - 'generation_id') #- '{provenance,content_digest}'`
   before the suppression bake. Same strip in migration 326's fill
   expression (unmerged; fix on this branch). `payload` stays in the ROW
   comparison.
4. Capture stages on `evidence_fact_ids` change via a secondary comparison,
   not the content digest: extend the stored-row read to return
   `evidence_fact_ids`, carry the sorted set on `changedRow`, stage in
   `dirtyIdentifiersToCapture` when digest is equal but the evidence set
   differs. `contentDigestOf`/`changedCVEIDs`/`touchedGroupIDs` untouched.
5. Blocks merge of slices 0-2 as a unit until decisions 3-4 land with
   proof; no assertion relaxed to accept `Updated:1` as-is.

## Required proof

1. Rename failing test to `...ResweepVolatileDigestExcludedFieldsChangeNothingLive`:
   volatile-only rewrites -> 0/0/0 and byte-equal stored payload; update
   contention inventory enrollment.
2. New live `...ResweepEvidenceFactIDsRefreshWinnerPayloadLive`:
   evidence-only rewrite -> `Updated=1`, stored set equals new set.
3. Hermetic `TestDirtyIdentifiersToCaptureStagesEvidenceOnlyChange` plus
   live keyed proof: evidence-only writer pass -> keyed resweep refreshes
   stored evidence, then `AuditWinners` `Repairs=0`; intent-only rerun ->
   no keyed writes, `Repairs=0`.
4. Extend `TestWinnerSelectReadsNoDigestExcludedPayloadField` to catch the
   bare `source.payload` projection and require the strip (seeded RED/GREEN).
5. Re-run parity, 326 backfill, and payload-carried live tests unrelaxed.

## Will not do

Change `contentDigestOf` exclusions; stage on volatile keys; accept
audit-bounded evidence staleness; strip `evidence_fact_ids` (it is served).

## NOT_CHECKED (arbiter)

Failing live test not re-run by the arbiter (mechanism proven from source);
retention vs audit window and staged-audit compare internals not verified.
