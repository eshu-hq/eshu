# #10729 + #10728 criterion 5: disposable-DB baseline proof

Refs #10729, #10728. Part of #9209.

All numbers carry sample sizes. Extrapolations are labeled as such.
Anything not measured here is under NOT_CHECKED, not assumed.

## What was proven

On a disposable Postgres (18.6, docker `postgres:18-alpine`,
`pg-10729-disposable` on `127.0.0.1:55441`; no shared or QA database
touched), through core's real commit path (`FactStore.UpsertFacts`) and
the real reducer (`SupplyChainImpactHandler.Handle`), with the
production keyed resweep maintainer draining the dirty log:

- Fold arithmetic under the satellite's M5 rules on live NVD feeds.
- Dirty-log rows per committed baseline fact.
- Wall clock for commit, reducer passes, resweep, and feed scans.
- R6-style idempotency: two identical passes converge (dirty log 22 to 22,
  findings 20 to 20, zero new captures on the rerun).
- The "how much of NVD do we hold" count query inside an armed 15 s
  statement timeout, with an index-backed plan.

## 1. Facts per record on live year feeds (fold arithmetic)

Folding rule (from M5, an upper bound because the satellite folds
repeats): 1 `vulnerability.cve` fact + 1 per distinct sanitized
reference URL + 1 per distinct `(criteria, matchCriteriaId)` pair.
Measured 2026-11-16 with a throwaway whole-file loader
(`/tmp/10729-proof/fold.py`, kept out of the repo): `python3 fold.py
<file>`. Whole-file `json.load` is NOT the satellite's streaming path;
its RSS is an upper bound on the arithmetic, not a satellite proof (R7
stays NOT_CHECKED).

| Feed (live, 2026-11-16) | gz bytes | decomp bytes | records (n) | facts | mean | p50 | p95 | p99 | max | wall clock | peak RSS |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `nvdcve-2.0-Modified.json.gz` | 5,918,804 | 70,262,287 | 10,499 | 74,078 | 7.06 | 4 | 22 | 49 | 2,048 | 0.44 s | 362 MB |
| `nvdcve-2.0-2024.json.gz` | 25,173,750 | 293,955,107 | 39,376 | 357,958 | 9.09 | 4 | 22 | 102 | 2,436 | 2.48 s | 1.44 GB |

Checks: both `.meta` sizes match the downloads byte-for-byte, and both
decompressed sha256 digests match their `.meta` sha256 (lower-cased),
confirming the design note that `.meta` sha256 covers the decompressed
JSON. Modified span: M0 `2026-11-08T09:16:33Z`, M1 `2026-11-16T10:17:06Z`
(n = 10,499). The Modified mean 7.06 reproduces M5's 7.07 (n = 12,818);
the 2024 mean 9.09 is higher, so the arbiter's 3.01M-fact upper bound
(425,770 x 7.07) has an upper neighbor at 425,770 x 9.09 ~= 3.87M
(extrapolated, not measured).

## 2. Dirty-log rows per committed fact (disposable DB)

Planted slice (n = 20 NVD-identity CVEs, holder
`repository:r_10729_baseline`): per CVE one `vulnerability.cve`, one
`vulnerability.affected_package`, one package-consumption correlation,
and one `vulnerability.affected_product` (CPE) fact: 80 facts committed
via `UpsertFacts` in 0.017 to 0.029 s (two runs). Reducer pass: 0.067 to
0.131 s (three runs), `findings=20, written_rows=20`,
`possibly_affected=20`.

Dirty log after pass 1 (n = 22 rows): `fact_upsert=20`,
`activation=2`. The 2 activation rows are the one-time scope-seeding
pointer moves, not per-fact cost.

- `fact_upsert` rows per committed fact: 20 / 80 = **0.25**.
- Per CVE (per derived finding): **1.0**.
- Total per fact incl. one-time seeding: 22 / 80 = 0.275.

The design's guess was 1.0 row per fact; the measured slice gives 0.25
per fact (1.0 per CVE). Per CVE is the better planning unit: a CVE with
several affected packages would derive several findings (unmeasured).

Extrapolation (labeled): 3.01M facts at 0.25 ~= 750k `fact_upsert`
rows over a full baseline run; at one finding per ~4 facts and one
chunk per 30-minute planner run (~301 claims, ~150 h), about 5k rows an
hour, 5 to 8 percent of today's 60k to 108k/hour OSV/GHSA inflow, under
the 15k/hour trip line at minimum pacing. Real per-CVE fan-out at scale
is unmeasured.

## 3. R6-style idempotency (two passes converge)

Pass 2 ran the identical intent shape (new intent id, same 80 facts):
0.060 to 0.107 s, `findings=20, written_rows=20`, dirty log 22 to 22
(zero new captures: a same-digest rerun captures nothing, per the
`#9805` capture rule). Winners 0 to 0 before the maintainer tick
(winners are populated by the resweep, not the writer). No duplicate
keys, no fencing errors.

Resweep through the production keyed tick
(`ResweepWinnersKeyed`, `MaxKeyFraction=1000`):
`Inserted=20, Keys=20, Path=keyed, TriggerCaptured={Activation:2}`,
`winners_final=20, dirty_final=0`. The full path closes: 80 facts to 20
findings to 20 winners to an empty dirty log.

## 4. Held-count query under a 15 s statement timeout

```sql
SELECT count(DISTINCT payload->>'cve_id')
FROM fact_records
WHERE fact_kind = 'vulnerability.cve' AND is_tombstone = FALSE
  AND (payload->>'cve_id') LIKE 'CVE-%';
```

Run in `BEGIN; SET LOCAL statement_timeout = '15s'; SELECT; COMMIT`
(`SET LOCAL` is a no-op outside a transaction, so the timeout is armed
inside one): 20 distinct CVEs in 0.001 to 0.005 s (three runs, n = 80
fact rows). Plan (`EXPLAIN (COSTS OFF)`):

```text
Aggregate
  ->  Sort
        Sort Key: ((payload ->> 'cve_id'::text))
        ->  Index Scan using fact_records_vulnerability_active_purl_lookup_v2_idx on fact_records
              Index Cond: (fact_kind = 'vulnerability.cve'::text)
              Filter: ((payload ->> 'cve_id'::text) ~~ 'CVE-%'::text)
```

Index-backed, no sequential scan on this shape. Production scale (the
407 GB `fact_records` scan that timed out in M10) is NOT_CHECKED; only
the plan shape transfers. The query-surface read path was also
exercised: `ListSupplyChainImpactFindings` with a `CVEID` filter
returned the planted row.

## Reusable harness (for collector#100 and the #10728 baseline)

Disposable database only, never a shared one:

```bash
docker run -d --name pg-10729-disposable \
  -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=postgres -p 127.0.0.1:55441:5432 postgres:18-alpine
export ESHU_POSTGRES_TEST_DSN="host=127.0.0.1 port=55441 user=postgres password=postgres dbname=postgres sslmode=disable"
export ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN="$ESHU_POSTGRES_TEST_DSN"
export ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE=1
```

Then `openAdvisorySuppressionIsolatedDB` (template0, fresh database,
`ApplyBootstrapWithoutContentSearchIndexes`, 323 migrations, ~1.8 s of
migration apply in this run, template clone per process):
`FactStore.UpsertFacts` for the commit path,
`SupplyChainImpactHandler.Handle` with a fencing-token issuer for the
reducer, `ResweepWinnersKeyed` for the maintainer. The throwaway proof
test (`zz_10729_proof_live_test.go` in
`go/internal/storage/postgres`) was deleted before this commit; its
three runs went red (findings filter arity), then green, green, green.
`go vet` on the package is clean after the deletion, and the adjacent
unit tests
(`TestSampleNVDBaselineTrip*`, writer capture tests) pass.

## NOT_CHECKED

- R7 satellite streaming RSS against the 256 MiB target: the loader
  above holds the whole file; the satellite streams. Not the same thing.
- Full 25-file year-set scan wall clock: only Modified and 2024 were
  scanned; the full set is extrapolated.
- QA-scale held-count latency on the production-size `fact_records`.
- Collector-side items (different repo): a planted Modified window the
  satellite can read, cursor/M1 clamping, seed and manifest reuse, and
  the window cursor never advancing on baseline claims.
- Dirty fan-out above one finding per CVE (multi-package CVEs),
  `affected_product`-only derivation, OSV/GHSA interaction, and #10706
  gate behavior.
- DB-side process RSS isolated from the shared-host `go test` harness
  (whole-process peak 4.3 GB includes build and test orchestration, not
  a runtime claim; per-pass wall clocks above are the measured figures).
