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
longer exists. The TSR stale-scalar behavior it deferred to keeps its own
coverage in `tfstate_canonical_writer_stale_attrs_test.go`.

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

PLACEHOLDER_TABLE

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
on it again.

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
