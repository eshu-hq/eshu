# #7033 fleet reservation capacity fallback

The change on `fix/7033-reader-reservation-20261009` classifies one failure:
a four-reader fleet allocator admission wait reaches its internal deadline
while the caller is still live and no snapshot setup attempt has failed. It
adds the existing `ErrSnapshotReservationCapacity` marker, allowing the
code-topic reader's existing fenced single-statement fallback. It does not
change pool sizes, deadlines, SQL, result ranking, the HTTP contract, or the
healthy snapshot-set path. #7033 remains open for deployed acceptance.

Root-Cause Evidence: On ops-qa, the 2026-10-06 code-topic request returned
HTTP 500 after `connection_reservation_wait_ms=2001`, with no successful
`reserved_connections`; the exact resource holder was not established
([issue observation](https://github.com/eshu-hq/eshu/issues/7033#issuecomment-6016326058)).
On exact base `92b49128a`, a focused real-allocator test held one of four
slots and let the internal 25 ms reservation budget expire with a live caller.
`TestFleetReservationTimeoutClassifiesCapacityWithLiveCaller` failed in
0.034 s: `runFleet` returned a generic deadline/replay-stale error without
`ErrSnapshotReservationCapacity`, while `ctx.Err()` was nil. The fallback
consumer checks precisely this marker. This proves the code failure path,
not the identity of the holder in the historical incident.

Behavior proof: On candidate `452f5f4bc`, the runtime package tests and race
tests passed with Go 1.26.9. The focused tests cover live-caller admission
timeout, caller deadline, single-reader wait, prior setup failure, subsequent
reservation, waiter cleanup, and 50 cancel/release races. The marker is added
only before SQL setup; the existing query fallback passes the same checkpoint
context to its fenced one-reader statement. The existing disposable-Postgres
guarded-reader test compares complete uncapped ordered code-topic results to
the serial query and checks cleanup, but uses a legacy reader rather than the
fleet. There is no newly run fleet-plus-content SQL integration test here.

No-Regression Evidence: An in-memory `runFleet` benchmark with byte-identical
helpers and benchmark bodies was compiled from base `92b49128a` and candidate
`452f5f4bc` using Go 1.26.9 on the same 12-CPU host. Base binary SHA-256 was
`f86894410dc325d71693571a23e16f7fe945fe22c5aee591cab2a2955ccd2f55`;
candidate was
`57a4c7269dab26d37217d907a843388c7c91f230094caf92ef0843aaac9f2039`.
Each variant had one discarded warmup, then eight alternating ABBA/CBBC
blocks (16 observations per variant per profile). The available profile ran
20,000 reserve/release operations per observation; the contended profile
held one of four slots and ran 512 internal-timeout operations per observation.
The source has no PostgreSQL I/O, corpus, or storage state. Source bodies were
verified byte-identical before compilation; the candidate change was the only
production-code difference.

| Profile | Base median | Candidate median | Base p95 | Candidate p95 | Allocations/op |
| --- | ---: | ---: | ---: | ---: | --- |
| Available | 1,185.5 ns/op | 1,186 ns/op | 1,238 ns/op | 1,312 ns/op | 11 / 11 |
| Contended | 1,130,402 ns/op | 1,130,578 ns/op | 1,133,762 ns/op | 1,136,615 ns/op | 14 / 16 |

Raw ns/op, in execution order within each variant (the actual run alternated
variants by block):

- Available base: 1220, 1197, 1201, 1166, 1160, 1147, 1214, 1176, 1158,
  1195, 1213, 1169, 1129, 1238, 1147, 1236.
- Available candidate: 1241, 1182, 1172, 1170, 1225, 1149, 1224, 1167,
  1190, 1312, 1241, 1217, 1171, 1167, 1274, 1145.
- Contended base: 1130610, 1133762, 1129889, 1125866, 1129430, 1130898,
  1132772, 1131898, 1129383, 1130194, 1131712, 1129418, 1127243,
  1128018, 1131122, 1132483.
- Contended candidate: 1134871, 1129596, 1133470, 1132113, 1126521,
  1131290, 1125531, 1131129, 1136615, 1127384, 1130027, 1127211,
  1132219, 1125565, 1132801, 1129018.

The healthy-path medians differ by 0.5 ns/op, within observed variation;
candidate p95 is higher in this small sample, so this is not a tail-latency
proof. Contended latency is dominated by Go timer scheduling around a 100 us
requested deadline; the marker adds two allocations and about 56 bytes per
contended operation. These profiles do not prove PostgreSQL or HTTP latency.
The separate deployed ops-qa read-only warm endpoint windows before this
change measured 0.836949 s and 0.763618 s medians
([window one](https://github.com/eshu-hq/eshu/issues/7033#issuecomment-6082041820),
[window two](https://github.com/eshu-hq/eshu/issues/7033#issuecomment-6082079671));
they are not a before/after comparison for this patch.

Observability Evidence: Existing `reader_borrow` stage duration/outcome
records allocator wait. Existing per-member reservation, waiter, and
connection gauges expose pressure without member hostnames in query spans.
The code-topic parent already emits
`code_topic.parallel_fallback_reason=reservation_acquire_timeout` when the
typed marker selects the fenced fallback. This patch adds no label, payload,
credential, or raw error. The October 6 pool-holder cause and the second
window's 2.284438 s discarded warmup remain unattributed.

Next proof before #7033 closure: a rebuilt candidate endpoint run on a fixed
corpus with a controlled capacity case, plus a separate read-only deployed
ops-qa check after review and rollout. Neither is claimed here.
