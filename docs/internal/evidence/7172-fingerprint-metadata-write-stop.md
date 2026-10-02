# Fingerprint Metadata Write Stop (#7172)

## Theory

#7167 strips the five clone-detection fingerprint keys (`body_shingles`,
`body_sketch`, `body_fp_exact`, `body_fp_renamed`, `body_token_count`) from
query responses, but the store kept persisting them in
`content_entities.metadata` even though the reducer and clone grouping read
fingerprints from the `code_function_fingerprint` / `code_fingerprint_band`
side tables. The theory: the keys are pure write bloat with no store-side
reader, so the writer should stop persisting them (no backfill: pre-existing
rows keep their keys and stay safe behind the read-time strip).

## Measurement

No-Regression Evidence: `sum(pg_column_size(metadata))` with vs without the
five keys, measured on lane-N Postgres (postgres:18-alpine,
127.0.0.1:15433, scratch database, since dropped) over 1000 rows built with
the production encoders (`EncodeSketch` over 128 registers,
`EncodeShingles` over N sorted unique FNV-64a ids): 400 plain entities, 200
exact-only, and 100 full-tier entities at each of 50/200/800/2000 shingles.

| tier | rows | with keys | without keys | key share |
| --- | --- | --- | --- | --- |
| exact-only | 200 | 58 kB | 35 kB | 39.2% |
| full-50 | 100 | 320 kB | 18 kB | 94.5% |
| full-200 | 100 | 554 kB | 18 kB | 96.8% |
| full-800 | 100 | 1491 kB | 18 kB | 98.8% |
| full-2000 | 100 | 3366 kB | 18 kB | 99.5% |
| plain | 400 | 70 kB | 70 kB | 0.0% |
| total | 1000 | 5859 kB | 175 kB | 97.0% |

(The 97.0% total reflects the synthetic mix, which over-weights
full-fingerprint rows; the per-tier shares are the transferable numbers, and
they bracket #7167's measured 23-59% of row bytes on MCP responses.) The
sketch alone is a fixed 2048-char hex string per fingerprinted entity;
shingles scale at 16 hex chars per id.

Reader audit (rg over production code): no store reader consumes the
metadata-column copy. `fingerprintRowFromMetadata` derives side-table rows
from the in-memory map during `Write` (never a DB re-read); clone and
divergence grouping read `code_function_fingerprint`; responses strip per
#7167/#7210; incident `NameFingerprint` is name hashes, a different concept.
Pre-existing key-bearing rows remain readable: the single decode seam still
strips, and the reap/withdraw path compares side-table rows, not metadata.

## Fix

`persistedEntityMetadata` (copy-minus-five-keys, nil-safe, input never
mutated) applied at both `content_entities` persist sites:
`ContentWriter.Write` (the production path) and the test-only
`ContentStore` path (same table, same contract). Side-table derivation
still reads the untouched in-memory metadata. No migration, no backfill.

No-Observability-Change: no metric, span, or log key added, removed, or
renamed; the write path emits the same counters over strictly fewer
persisted bytes.
