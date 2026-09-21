# #6912: /infra/resources/{count,inventory} restored by removing #6843

## Summary

`GET /api/v0/infra/resources/count` and `/infra/resources/inventory`
regressed from 145ms/103ms to 1.76-2.46s when #6843 landed, with Postgres
work rising from 24,913 to 221,025 blocks per request. Removing #6843's
read-model workaround returns both routes to the graph and to their
pre-regression cost. No budget ceiling value changed.

## Root cause, and why an index could not fix it

#6843 moved CloudResource and TerraformStateResource off a whole-label
NornicDB scan onto per-request CTEs over `fact_records`. Its stated cause
(`docs/internal/evidence/6843-graph-only-labels-read-model.md:10-12`) was
that the only selective predicate, `evidence_source`, "has no NornicDB
index the planner takes on these shapes".

The `fact_kind` predicate alone is an Index Only Scan. The CTEs also
project six `payload->>'...'` fields per row, and that projection forces a
heap fetch per row, turning each leg into an Index Scan. Measured on the
gate's own corpus and recorded on #6912:

| fact_kind | plan | shared hit |
| --- | --- | --- |
| `reducer_cloud_resource_identity` | Index Scan | 46,129 |
| `terraform_state_resource` | Index Scan | 46,134 |
| `terraform_state_provider_binding` | Index Scan | 46,140 |
| `ec2_instance_posture` | Index Scan | 41,423 |

No practical index covers a six-field projection. A partial index on the
payload expression was measured and the planner ignored it: the plan
stayed an Index Scan at 46,134 buffers. Generated STORED columns plus a
covering index reached 1.94x for +37% table growth on `fact_records`.

## Why removal rather than another optimisation

NornicDB `a427a468` fixes the planner defect #6843 worked around:
conjunctive predicates never reached the property-index seek, measured
27,709,862 -> 8,721 ns/op on a 50k-node label. #6894 (`b2b2445d0`) made
that build the default backend, so the workaround was paying a permanent
8.9x Postgres cost for a bug that no longer exists.

## What the revert keeps

Two parts of #6843 are deliberately retained.

`295fe3a14` stays whole. Its fall-back-to-graph-when-the-table-leg-fails
behavior covers the #6793 `infra_resource_entities` read model, not the
fact path, so it survives the removal of the fact path.

`terraformStateResourceStaleContentPropRemoveStatements` stays. It strips
the six content properties (`environment`, `kind`, `data_type`,
`resource_service`, `resource_category`, `service_kind`) that migrated
pre-#5443 `TerraformStateResource` nodes keep and the resource upsert
never overwrites. The #6843 parity battery found it, but it fixes the
graph. Dropping it while making the graph authoritative again would have
put exactly those stale values back into bucket reads.

The #6843 parity battery itself is removed: it proves the Postgres fact
path buckets identically to the graph scan, and its Postgres side no
longer exists.

What that costs, stated precisely rather than waved at: the battery was
added BY #6843 to prove its new Postgres path matched the graph, so with
that path gone the comparison has no second side. No coverage that
predated #6843 is lost. There is no live route-level bucket test for
`TerraformStateResource` specifically -- but there was none before #6843
either, so that gap is pre-existing rather than opened by this change.
Graph bucket truth keeps three proofs:

- `TestLiveInfraProviderInventoryBucketsNonNull` (#5283), a live
  backend-required test that runs the shipped
  `GraphInfraResourceAggregateStore.InfraResourceInventory` by-provider
  read over `CloudResource` and asserts no null bucket, the
  `source_system` fallback, and no collapse of a real provider.
- The B-7 golden corpus snapshot
  (`testdata/golden/e2e-20repo-snapshot.json`). Its floors are asymmetric
  and worth stating exactly: `CloudResource` is min 118 / max 124, a real
  count floor; `TerraformStateResource` is min 1 / max 30, which alone
  would not catch a collapse from 30 nodes to 1. TSR's protection is the
  snapshot's #5446 non-vacuous guard instead -- at least one
  `TerraformStateResource` node carrying `provider='aws'`, with row tokens
  on `provider` and `tf_attr_instance_type` -- which fails if the provider
  binding stops reaching the node.
- `tfstate_canonical_writer_stale_attrs_test.go`, which the battery
  itself named as the owner of the TSR stale-scalar behavior.

## The revert is exact, by the repo's own digests

The queryplan manifests freeze a SHA-256 of both the production builder's
source and its rendered Cypher. After the revert, production renders
`QP-INFRA-RESOURCE-AGGREGATE-GRAPH` at `cypher_sha256`
`5bfabda32742f7065a2e4afad85fea257559b88183ae83092838a6a992c239bc` and
`infraGraphOnlyCountCypher` at `source_sha256`
`76bc6eb4d2b12d54b490a62e6316c6cb68a72441607c9413f4bd526296e0ddfc` --
both byte-identical to the values committed at `ebbe18d9d^`.

The only two symbols whose digests legitimately move are
`countFromReadModel` and `inventoryFromReadModel`, which is exactly the
pair carrying `295fe3a14`'s fallback.

## The corpus the revert nearly removed

#6843's `seed_graph_only_facts.go` writes `reducer_cloud_resource_identity`
rows into `fact_records` for its read model. `GET /api/v0/cloud/inventory`
reads that same kind independently: the `cloudInventoryFactKind` constant
in `go/internal/query/cloud_inventory_read_model.go` is that same string.

Deleting the seed with the read model dropped that route from 48,617 to 7
blocks, and regenerating the work budgets from such a run rendered its
guard at 21 blocks instead of 145,851: a 6,945x tightening of an unrelated
production route's guard. Before #6843 the route had no named row and sat
under the default, so #6843 had given it a real corpus as a side effect.

The seed is therefore kept as corpus and renamed to say so
(`seed_cloud_state_facts.go`). The other three seeded kinds were checked
for the same exposure: `ec2_instance_posture` has only a writer consumer,
`terraform_state_provider_binding` has no non-test read consumer, and
`/iac/resources` draws its corpus from the IaC seeding (74,925 -> 74,928,
immaterial).

## Performance Evidence: infra resource aggregate routes

Baseline and after are the same gate, same corpus, same machine, same
backend image; only the tree differs.

- Backend: NornicDB
  `ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-490-a427a468@sha256:eb69530f...`,
  self-reporting `NornicDB v1.3.3`, native `linux/arm64` child. Postgres
  18-alpine.
- Input shape: 800 ingestion scopes, 1,120 generations, 67,200 work items,
  150,000 graph nodes per infra label across 8 labels
  (`VerifyGraphNodeCounts` fails the run if any label is short), 150,000
  IaC facts, plus the cloud/state fact corpus at `nodesPerLabel`.
- Sweep: 113 no-arg GET routes, 20 counted iterations each, 65 exercised.

Two runs on the shipped tree. Run A is commit `95c217ef4`, run B is
`c32a48ccf`; the only difference between them is one `_test.go` comment
and this file, so both built an identical gate binary.

| route | before (main, #6843) | run A | run B | budget |
| --- | --- | --- | --- | --- |
| `/infra/resources/count` p95 | 1.25s | 108ms | 106ms | 2000ms |
| `/infra/resources/inventory` p95 | 1.22s | 90ms | 76ms | 2000ms |
| `/infra/resources/count` blks | 221,025 | **24,913** | **24,913** | 74,739 |
| `/infra/resources/inventory` blks | 221,025 | **24,913** | **24,913** | 74,739 |
| `/cloud/inventory` blks | 48,617* | 48,622 | 48,622 | 145,866 |
| `/iac/resources` blks | 24,976 | 24,977 | 24,977 | 74,931 |

24,913 is the pre-#6843 figure exactly, not an approximation of it. The
issue predicted it: `infra_resource_entities` is 24,893 heap pages and the
restored query is one clean scan of it.

Both runs agree on every work counter to the block, across the two routes
plus the two neighbours that read the same corpus. That reproducibility is
what makes the block count usable as the load-bearing number: it does not
move between runs, so it does not move between runners either -- which is
the same property that refutes the #6909 variance diagnosis below.

`/cloud/inventory` and `/iac/resources` are listed because they read the
same corpus and must not move. They did not.

\* The `/cloud/inventory` before-figure is back-derived, not measured by
this drive: main's committed budget is 145,851 and the renderer's formula
is `ceil(max * 3.0)`, which divides exactly to 48,617. No gate run on main
was taken for it here. The after-figures are measured.

Work budgets were regenerated from both runs' reports with
`scripts/refresh-read-api-work-budgets.sh`, never edited by hand:

```
GET /api/v0/infra/resources/count       13  663066  88   ->  13  74739  80
GET /api/v0/infra/resources/inventory   13  663066  16   ->  13  74739  14
```

74,739 is `ceil(24,913 * 3.0)`, the renderer's own formula.

The rest of the table moved as follows, stated per column because the
columns behave differently and an unscoped claim here would be wrong:

- `blks`: 16 named rows rose and three fell. Every rise is under 0.2%,
  the largest being `GET /api/v0/status/operations` at 72,831 -> 72,960
  (+0.18%, 129 blocks). The three falls are the two infra routes and
  `GET /api/v0/freshness/generations` at 92,919 -> 91,860 (-1.14%).
  `/cloud/inventory` is one of the 16, at 145,851 -> 145,866: it held
  rather than collapsing to 21 -- see the corpus section above.
- `rows`: nothing rose. `GET /api/v0/status/governance` moved 68 -> 66
  (-2.94%), `GET /api/v0/infra/resources/count` 88 -> 80 (-9.09%), and
  exactly ten named routes moved 16 -> 14. Eight of those ten are
  floor-riders: the whole row is identical to the `default` row in both
  snapshots, so under the renderer's "no named row below the default
  row" rule they track the default row's own cross-run noise (16 -> 14)
  rather than any change in their own traffic. The other two,
  `/cloud/inventory` and `/infra/resources/inventory`, are not identical
  to the default row -- they carry their own `blks` and are accounted for
  separately above. Only their `rows` column tracks the default row's
  16 -> 14 move.

Every `rows` delta tightens a guard. The `blks` column moved both ways:
16 named budgets rose, none by more than 0.18%, and three fell. Those
shifts are between the reports main's committed table was rendered from
and this drive's two reports: the renderer takes the max over whatever
reports it is given, so a budget moves when its inputs do. That holds
whether or not the two runs agreed. This drive's runs did agree to the
block on the four routes compared above; no such comparison was made for
the other changed routes, and this claim does not rest on one.

A budget that rises is a looser guard, so the residual risk here runs in
both directions at noise scale: a raised budget hides that much more, and
a lowered one breaches if a future run lands between the old and the new
value. Both are inherent to regenerating from measured maxima, not
specific to this change. An independent check confirms the rendered table
admits every measured route:
65 routes checked across both reports, zero breaches, and the checker
fails as expected on a seeded violation.

## No-Regression Evidence: every other exercised route

Both runs report `all 65 exercised routes within budget` with gate exit 0
and no 5xx in the sweep. The regenerated work budgets are rendered from
both runs' reports by `scripts/refresh-read-api-work-budgets.sh`, never by
hand, and every measured route's max across the two reports is within its
rendered budget.

## Observability Evidence: no signal added, renamed or removed

No metric, span, log key or status field is added, renamed or removed.

`eshu_dp_infra_inventory_reads_total{route,source}` keeps its name and
label set, and the truth envelope keeps its bases. What changes is which
values these report for these two routes, which is the intended,
operator-visible signal that the revert took effect:

- `category=cloud` reads move from `source="read_model"` back to
  `source="graph"`, and their truth basis from `content_index` back to
  `authoritative_graph`.
- Unscoped mixed reads stay `hybrid`, with CloudResource and
  TerraformStateResource counted in the graph leg again rather than the
  Postgres leg.

An operator watching that metric sees the store change at deploy, which is
how they confirm the rollback landed. The route/source pair is pinned by
`TestInfraAggregateCategoryRoutesToOneSide` and the readiness test.

## Cold-start behavior, disclosed

The gate's pre-sweep truth check logs
`infra read model check attempt N/3 failed: HTTP 504` before succeeding.
This is documented pre-existing behavior of the graph path, not something
this change introduces. `go/cmd/read-api-latency-gate/infra_truth.go:43-48`
records that "on a cold NornicDB the count route's graph pass for the
graph-only labels took 8.3s against the API's 10s deadline, so a FIRST
request can 504 while the next succeeds", and that 3-attempt retry shipped
with the gate itself (`326a12d30`, #6860) before #6843 existed.

#6843 masked it for these two routes by serving them from Postgres;
restoring the graph path restores it. The measurement is unaffected by
construction: the truth check runs before the sweep and warms the backend,
and a 5xx inside the sweep breaches as `HardFailed` regardless of any
latency exemption. Neither run had one.

## Latency ceilings

`LatencyExemptions` is empty again. #6909 had made both ceilings advisory
on the reading that the p95 swing was runner-speed variance over
deterministic Postgres work. The constancy refutes that: the grant's own
evidence block in `read-api-route-budgets.txt` records four CI runs with
"identical work every time (3 calls, 221025 buffers)", and run
`35593350187` on a fifth unrelated PR reads 221,026, against 24,913 before
#6843. Runner speed varies p95; it cannot hold buffer hits constant to
within two blocks across five runners.

No ceiling value changed. Both routes keep their 2000ms budget and block
on it again. The map held only these two entries, so emptying it took
cover from nothing else: a latency breach on any other route after this
change is not a side effect of it.

## Reproducing, and the limits of this evidence

```bash
NORNICDB_PLATFORM=linux/arm64 \
GATE_COMPOSE_PROJECT=eshu-6912-$$ \
GATE_WORK_REPORT=/tmp/r.json \
  bash scripts/verify-read-api-latency-gate.sh
```

`NORNICDB_PLATFORM` matters on an Apple-silicon host. `docker-compose.yaml`
pins `linux/amd64`; if the image was already pulled natively, Compose fails
with `cannot overwrite digest sha256:eb69530f...`, because the tag is a
multi-architecture index whose arm64 child is cached and Compose is asking
for the amd64 one. The image and the pin are both fine. Run the native
child, or drop the cached tag and let Compose pull amd64.

The p95 figures below are arm64-native on one developer machine: a
same-machine before/after, not a CI-comparable absolute. An emulated amd64
backend on this host would measure the emulator. The Postgres block counts
are architecture-independent, which is why they, not the milliseconds,
carry the argument.
