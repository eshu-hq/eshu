# #11584 key-range proof: CVE-2026-26996 on primary-shaped data

Refs #11584 (Part of #11584), parent #10671, planner verdict recorded on
#11584 (2026-11-27): route this id through the existing key-range read, no
new statement, no index change. Lane: `drive/11584-keyrange-proof` (proof
only; no production code change). Skills active: `performance-rigor`,
`postgres-rigor` (read-only), `golang-engineering` (throwaway statement
generator, removed after use). Date: 2026-11-27.

## Summary

We tested whether the guard-marginal id `CVE-2026-26996` (true mass now
165,283 rows) can be served through the existing 16-statement key-range
holder read inside the 10 s production guard. The routing gate fires
(routing mass 3001, saturates above the 3000 gate), the ranged holder sets
are byte-equal to the shipped read (245 = 245 on one snapshot), and none
of the 128 ranged statements on the live primary fired the guard — but the
stated per-statement bar (p95 <= 2 s, max <= 5 s) FAILS on the full bounded
run (p95 9,122 ms, max 9,987 ms): cold first-touch rides the guard with
~13 ms of margin, and live-primary variance re-spikes mid-run. Warm
steady-state (80 statements) passes the bar (p95 859 ms, max 974 ms).

## Verdict: FAIL vs the stated bar (with numbers)

Bar: all 16 per-range statements (pinned `force_custom_plan`, one
REPEATABLE READ snapshot, texts byte-identical to the shipped constants)
complete with per-statement p95 <= 2s and max <= 5s, plus byte-equal holder
sets vs the shipped read.

| Check | Result |
|---|---|
| Routing gate fires for this id on current main | PASS: routing mass 3001 = gate+1, saturates, ranges |
| Byte-equal holder sets vs shipped read | PASS: 245 = 245, one replica snapshot |
| Per-statement p95 <= 2s and max <= 5s (bounded primary run, n=128) | FAIL: p95 9,122.4 ms, max 9,986.7 ms |
| Warm steady-state cut (passes 3-7, n=80) | PASS: p95 858.8 ms, max 973.7 ms |
| Statement-timeout fires in 128 ranged statements | 0 (shipped fires at 10.051 s) |

The routing verdict is not refuted (the gate fires; the ranged read never
breaches the 10 s guard). The per-statement bar does not hold
cold/first-touch or under live-primary variance at this mass. Details below.

## Gate check (current main): PASS, gate value note

Current `origin/main` (`0588e9ce5c`) retuned the gate since the lane brief:
`HolderKeyRangeMassGate = 3000`
(`go/internal/reducer/supplychain/core/fanout_key_range.go`), not 78435.
The router (`fanout_split.go`: `mass > HolderKeyRangeMassGate` ranges) is
unchanged. Routing mass for `CVE-2026-26996` from the shipped
`HolderRoutingMassQuery` (production settings: read-only txn,
`enable_seqscan=off`, 15 s LOCAL guard, replica):

- `lookup_id=CVE-2026-26996 mass=3001` in 818.930 ms, exit 0.

3001 = gate+1: saturated above the gate three ways (cve arm alone exceeds
it), so the id reads as 16 key-range statements. Gate fires; verdict route
holds. (If it had reported <= 3000, this lane would have stopped here.)

Host: read replica (`pg_is_in_recovery=t`, lag 0.12 s), PostgreSQL
18.6-eshu-7957, supports ~12,829,231 reltuples, 160 GB total / 32 GB heap.
Capped read role (`direct-read-dsn-capped`); its `?statement_timeout`
URI param is rejected by host psql, so URI params were stripped and every
txn set an explicit LOCAL guard that subsumes the DSN cap (recorded).

## Re-census: true mass grew past Nov-26

Nov-26 census: 156,870. Replica, one REPEATABLE READ snapshot (first attempt
at 15 s LOCAL fired on the cve-arm count; single labeled 300 s diagnostic
rerun per H2 census method, no storm):

| Arm | Rows | Time |
|---|---|---|
| `cve_id` | 165,283 | 8.210 s |
| `advisory_id` | 90,331 | 0.493 s |
| member (`?`) | 90,331 | 61.089 s |
| union (any arm) | 165,283 | 5.506 s |

Union == cve-arm count: every advisory/member row is also a cve-arm row
(member-only rows 0, same shape as H2). True mass 165,283 (+5.4%).
Estimated pairs ~277k per the issue. Member arm 90,331 (up from ~67k est).

## Exactness: PASS (replica, one snapshot)

One REPEATABLE READ snapshot, `force_custom_plan`, shipped reference at a
labeled 120 s diagnostic timeout + 16 range statements at 15 s LOCAL:

- shipped: 245 holders in 4,235.664 ms (warm; cold fires guards per #11584).
- ranges: 245 holders union, per-statement 211-245 ms.
- `cmp` after `sort -u`: BYTE_EQUAL=yes.

Statement texts verified byte-identical to the shipped constants by
construction (throwaway Go generator concatenating the exported constants;
`go test ./tmp-11584proof/ -run TestPrepareBodiesByteIdentical` PASS, exit
0) and fed via `PREPARE` + `EXECUTE` (server sees the exact production
text; `force_custom_plan` mirrors the store). First run caught a script
bug (a `set_config` output row leaking into the capture file, 246 vs 245);
fixed ordering and re-ran clean: 245 = 245.

## Primary scored run: FAIL on cold/variance, PASS warm

Primary (`eshu-postgresql-0` via `eshu-postgresql` svc, read-only txns,
10 s LOCAL guard per statement, `force_custom_plan` pinned, one snapshot
per 16-statement pass). 8 passes x 16 statements, 0 timeouts, all 8 union
sets 245 holders and mutually byte-equal (matches the replica 245).

Per-statement ms (16 per pass, in range order):

| Pass | min | max | wall |
|---|---|---|---|
| 1 (cold discard) | 7,066.0 | 9,986.7 | 142.0 s |
| 2 | 1,057.1 | 8,204.7 | 55.6 s |
| 3 | 452.2 | 970.4 | 11.0 s |
| 4 | 496.3 | 737.2 | 9.2 s |
| 5 | 489.3 | 854.2 | 9.8 s |
| 6 | 424.6 | 760.6 | 9.2 s |
| 7 | 495.6 | 856.7 | 10.7 s |
| 8 | 593.1 | 4,743.6 | 24.3 s |

Full set (n=128): min 424.6, p50 693.3, mean 2,122.7, p95 9,122.4,
p99 9,926.2, max 9,986.7; 26 statements over 2 s, 21 over 5 s, 0 timeouts.
p95 and max both breach the bar: FAIL.

Warm steady-state (passes 3-7, n=80): min 424.6, p50 598.1, p95 858.8,
max 973.7, 0 over 2 s: meets the bar.

Reading: cold first-touch rides the 10 s guard (max 9,986.7 ms, ~13 ms
of margin) and holds the snapshot ~142 s; live-primary variance reappears
mid-run (pass 2 first five statements 7.0-8.2 s; pass 8 five statements
1.8-4.7 s with zero guard fire), the same load variance H2 reported
(statements to 6.6 s). The A2 proof topped out below this mass and its
warm-only verdict does not transfer to cold primary at 165k rows. The
production-critical property holds throughout: no 57014, while the
shipped statement fires the guard at 10.051 s.

## Limits (NOT_CHECKED)

- Replica lag between census/exactness and primary passes (0.12 s at read;
  union counts agree at 245 across hosts, no drift observed).
- 4-parallel contention at this mass (H2 A2 covered smaller ids).
- Snapshot-hold bloat of a ~142 s cold xmin on a busy primary.
- No new issue filed. No production code changed.

## Commands and exits

- `go test ./tmp-11584proof/ -run TestPrepareBodiesByteIdentical` exit 0.
- Replica: identity/gate/census/exactness scripts exit 0 (census first
  15 s attempt fired once, labeled 300 s rerun exit 0; not a storm).
- Primary: 8/8 passes exit 0, 128/128 statements under the 10 s LOCAL
  guard, 8/8 unions 245 holders byte-equal.
