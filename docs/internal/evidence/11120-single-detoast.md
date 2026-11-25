# #11120 single-detoast extraction: evidence

## Claim

The winner body's candidate CTEs extracted every text field with a separate
`payload->>`, re-detoasting the same finding payload (~3.4 KB at QA) once per
expression. The rewrite extracts all nineteen text fields once through one
guarded `LATERAL jsonb_to_record` per row; the final projection reads the
carried columns. Winner output is byte-identical (differential below), so the
contract re-pins under version 2 without a bump.

## What changed, and what deliberately did not

- Converted: `source_candidates`, `operator_candidates`, the final projection
  (carried columns), and the scope-identifier expansion (5 fields).
- Kept `->>` in `winnersKeyedNarrowProbeSelect`: the five equalities are the
  291 keyed index expressions. A record Var cannot match an expression
  index, and at 3 fields the lateral overhead exceeds the saving (below).
- Kept: `cvss.BucketSQL("source.payload")` (shared with the read-time dedup),
  the three jsonb array projections (JSON-null semantics differ, proven
  live), and the winner-scale override reads.

## Measurements (local PG18.6, 5,000 rows x ~3.4 KB payloads, warm cache)

Same heap buffers on both arms in every pair (313); the gap is CPU (detoast).

- Full body select list (~19 distinct fields, ~25 reads): legacy 74.9 ms
  vs single lateral 16.4 ms (~4.6x).
- Five-field identifier list: legacy 17.9 ms vs lateral 10.3 ms (~1.7x).
- Three-field list: legacy 11.4 ms vs lateral 15.3 ms (lateral slower;
  small fixed probes stay as they are).
- Probe plan shape on an expression-indexed table: legacy `->>` form is an
  Index Scan (3 buffers, 0.06 ms); the record form is a Seq Scan with a per-row
  Function Scan (313 buffers, 9.9 ms). Converting the probe would trade five
  detoasts for a full scan.

## Differential (byte-identity proof)

- Hermetic: `TestWinnersDetoastReversesToLegacy` restores the legacy text
  from the shipped statements; `TestWinnersContractRepinnedForSupportsInputs`
  chains the supports and detoast reversals back to the pre-supports pin
  `25e4d891…`, which passes only on a byte-exact round trip.
- Live: `TestSupplyChainImpactWinnersDetoastFieldEquivalenceLive` runs the shipped lateral
  against legacy `->>` on PG18 over 20 edge payloads (missing keys, JSON
  nulls, empty strings, ints, 5.0, booleans, padded/unicode, a toasted wide
  row, scalar string/number/bool/array, JSON null, SQL NULL): all 19 fields
  plus the c0/priority/flag/suppression derivations match on every row, and
  the CASE guard returns NULLs instead of erroring on non-objects.

## Evidence markers

- Performance Evidence: local PG18.6, baseline 74.9 ms vs 16.4 ms after on
  5,000 x ~3.4 KB toasted payloads (warm cache, identical 313-buffer heap
  reads on both arms; 5,000 rows in and out). The full body select list
  (~19 distinct fields, ~25 reads) drives the win; the 5-field ident list
  measures 17.9 ms vs 10.3 ms. Same-machine relative improvement only; the
  end-to-end keyed-tick proof is criterion 1's bounded replay in its own
  lane.
- Observability Evidence: no new signals. The improvement is visible in the
  existing maintainer tick telemetry: per-tick duration and the
  scanned/kept-copy counts in the tick result and logs, plus the keyed-tick
  duty and dirty-depth gauges the QA live reading (criterion 4) samples
  after deploy. The change is safe by construction of the differential:
  hermetic reversal proves the shipped text differs from the legacy text
  only by the live-proven field pairs, every rewritten statement plans on
  PG18, and the probe, bucket, and array exceptions are pinned by tests.

## Falsifiers checked

- Non-object payloads: `jsonb_to_record` ERRORS on scalars (`cannot call
  populate_composite on a scalar`) while `->>` returns NULL. The guard was
  added for exact parity, not assumed away; no CHECK constraint guarantees
  object payloads.
- JSON-null arrays: `->` keeps jsonb null, the record maps it to SQL NULL
  (proven live on `{"service_ids":null}`); the arrays stay on `->`.
- Probe conversion: falsified twice (index loss + fixed overhead above).
