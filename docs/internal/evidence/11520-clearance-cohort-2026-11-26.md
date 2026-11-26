# #11520 cohort evidence (2026-11-26 ~04:03Z)

Refs #11520. Run `qa-canary-20261119`. All reads against the QA Postgres
read replica (`default_transaction_read_only=on`, per-statement
`statement_timeout='15s'`), via port-forward of the in-cluster read
service. No writes, no deploys, no code changes.

## Q1: cohort query (15s-compliant, PROVEN)

```sql
SET statement_timeout='15s';
SELECT date_trunc('day', created_at) AS day, status, count(*)
FROM fact_work_items
WHERE domain='supply_chain_impact'
  AND status IN ('pending','retrying','claimed')
  AND strpos(scope_id,'vuln-intel://nvd/_baseline/')=0
GROUP BY 1,2 ORDER BY 1,2;
-- cold 388.973 ms on the grouped query above; warm repeats used
-- SELECT count(*) with the same WHERE clause: 100.454 ms, 71.416 ms
```

Cohort totals at read time: pending 7,772 (oldest 2026-10-22),
retrying 628 (oldest 2026-10-23), claimed 10 (oldest 2026-11-09) —
**8,410 open non-baseline rows**. Baseline-prefix check from P1a
(2026-11-26 03:57Z): 0 of the open sci rows carry the
`vuln-intel://nvd/_baseline/` prefix (strict and loose patterns agree
at 17 rows repo-wide, all terminal/projector states).

## Q2: clearance target (FORM-CORRECT, DATE ABSENT — correct)

Target form: clearance date = cohort rows / (vuln-filtered drain rate
minus inflow rate), computed only after a mechanism lifts drain above
inflow with headroom. P1a 24h worst-partition read (identical
boundaries, 03:57Z): inflow 910 vs drain 733 (0.81x, net +177/day).
With drain below inflow no finite date exists; any fixed date now
would be a round number. The shim's 91/h 7-day figure is the gross
queue, not this cohort, and must not be reused here.

## Q3: proof ladder (designs only, none implemented)

Binding constraints for any candidate: no timeout raise, no
worker/batch cuts, no batch-size-1, cheapest-shim proof first on a
>=10min vuln-filtered cut with abort bars; any claim-ordering variant
needs a new shim proof (DROP precedent: +16-17% CPU, +22-24% buffers
for zero reorder).

1. H2 hot-id holder-read isolation: cheapest shim is EXPLAIN ANALYZE
   BUFFERS at production shape plus a bounded replay of one hot-id
   pass isolated-vs-shipped with findings identity.
2. Shared file-fold index (#11093): re-profile gate first (deferred),
   then bounded replay with/without index. Risks: shared mutable
   state, memory pressure, index-build lock contention.
3. Transitive criteria-expansion bound (#11087): bounded replay with
   identity comparison; risk is correctness (missed findings).
4. 6h-bounded vuln window-intake throttle (collector admission only):
   shim is the gate reads themselves; risk shifts backlog upstream to
   intake (10k ceiling per #10882).
5. Drain-gating tier extension: RULED OUT for this cohort (0% baseline
   gets nil yield by construction).
6. Worker bump: RULED OUT until the H2 holder shape is fixed.

## Sources

- P1a figures (inflow 910, drain 733, 17-row baseline check, 03:57Z)
  are recorded in the #11520 issue body (coordinator live read).
- DROP cost deltas (+16-17% CPU, +22-24% buffers) are recorded in the
  Box-2 verdict comment on #10972.

## Verdicts

- Q1 (15s cohort query): PASS with timings above.
- Q2 (measured target): form recorded, date absent pending headroom.
- Q3 (no serialization): PASS on the written designs with the stated
  conditions; candidates 5 and 6 ruled out with reasons.
