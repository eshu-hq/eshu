# Writer WAL with the generation_id index (#10880 PR-B1)

Refs #10880. Arbiter ruling `arbiter-b1-cascade`, acceptance check 3. The real
writer harness ([m6_write_cost_and_backfill_harness.go.txt](m6_write_cost_and_backfill_harness.go.txt),
`TestZZB1WriteCostScratch`), `postgres:18.6` (disposable container,
`shared_buffers=1GB`, `fsync=off`), 3,000 findings in one scope, three
repetitions, host load about 50. Each repetition runs three fresh databases:
without the supports maintainer (the baseline), with it and migration 338's
`(generation_id)` index dropped (the shape measured before the ruling), and with
it and the shipped migration. WAL bytes are the figure; wall times are not kept
(host load). The passes are insert, replay of the same 3,000 under a new intent
id, and retract 1,500 of 3,000.

Raw lines (WAL in bytes; percentages are against the baseline of the same
repetition):

```text
rep 0 insert  WAL without= 17639040 supports-no-genidx= 24852320 (+ 40.9%) supports+genidx= 25120752 (+ 42.4%) | genidx adds   1.5 pts, 268432 bytes
rep 0 replay  WAL without= 14032168 supports-no-genidx= 21219216 (+ 51.2%) supports+genidx= 21487688 (+ 53.1%) | genidx adds   1.9 pts, 268472 bytes
rep 0 retract WAL without= 10871416 supports-no-genidx= 14596424 (+ 34.3%) supports+genidx= 14731008 (+ 35.5%) | genidx adds   1.2 pts, 134584 bytes
rep 1 insert  WAL without= 17638912 supports-no-genidx= 24852320 (+ 40.9%) supports+genidx= 25120712 (+ 42.4%) | genidx adds   1.5 pts, 268392 bytes
rep 1 replay  WAL without= 13760048 supports-no-genidx= 21219200 (+ 54.2%) supports+genidx= 21487704 (+ 56.2%) | genidx adds   2.0 pts, 268504 bytes
rep 1 retract WAL without= 10871416 supports-no-genidx= 14596440 (+ 34.3%) supports+genidx= 14822496 (+ 36.3%) | genidx adds   2.1 pts, 226056 bytes
rep 2 insert  WAL without= 17701872 supports-no-genidx= 24852336 (+ 40.4%) supports+genidx= 25120792 (+ 41.9%) | genidx adds   1.5 pts, 268456 bytes
rep 2 replay  WAL without= 13760048 supports-no-genidx= 21219200 (+ 54.2%) supports+genidx= 21487688 (+ 56.2%) | genidx adds   2.0 pts, 268488 bytes
rep 2 retract WAL without= 10871416 supports-no-genidx= 14596440 (+ 34.3%) supports+genidx= 14731008 (+ 35.5%) | genidx adds   1.2 pts, 134568 bytes
```

| Pass | Supports without the index | Supports with the shipped migration | The index adds |
| --- | --- | --- | --- |
| Insert 3,000 | +40.4% to +40.9% | +41.9% to +42.4% | 1.5 points (268 KB) |
| Replay 3,000 | +51.2% to +54.2% | +53.1% to +56.2% | 1.9 to 2.0 points (268 KB) |
| Retract 1,500 | +34.3% | +35.5% to +36.3% | 1.2 to 2.1 points (135 to 226 KB) |

The ruling's stop thresholds are 3 points on insert and 6 on replay. The index
adds 1.5 and 2.0, so the index swap to `(generation_id, scope_id)` gets no
hearing. The ruling predicted about 2 points on insert and 3 to 5 on replay; the
measured replay cost is lower.

These stay above the epic's +25% impact-WAL rule. The earlier ruling reasoned on
+19.4%, a figure that measured the support statements alone; the real writer
measures about twice that. The premise is falsified and the payload stays for
the PR-C reasons (see the execution record).
