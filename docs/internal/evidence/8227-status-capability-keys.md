# 8227: capability-matrix keys for the root status and live evidence-bundle routes

Issue #8227 registers one capability-matrix key per route that answered the
reader-fence `503` with no `error.capability`, passes that key from each fence
write, and deletes `querycontract.NoCapability` with its allowlist.

| Route | Key |
| --- | --- |
| `GET /api/v0/status/pipeline` | `operations.pipeline_status` |
| `GET /api/v0/status/collectors`, `GET /api/v0/collectors` | `operations.collector_status` |
| `GET /api/v0/status/ingesters/{ingester}`, `GET /api/v0/ingesters/{ingester}` | `operations.ingester_status` |
| `GET /api/v0/status/index`, `GET /api/v0/index-status` | `operations.index_status` |
| `GET /api/v0/status/hosted-readiness` | `operations.hosted_readiness_status` |
| `GET /api/v0/status/freshness-causality` | `operations.freshness_causality_status` |
| `GET /api/v0/status/collector-readiness`, `GET /api/v0/collector-readiness` | `operations.collector_readiness_status` |
| `GET /api/v0/status/operator-control-plane` | `operations.operator_control_plane_status` |
| `GET /api/v0/evidence/bundle` | `operations.evidence_bundle_live` |
| `GET /api/v0/auth/admin/audit/events` | `audit.events.list` |
| `GET /api/v0/auth/admin/audit/summary` | `audit.summary.read` |

## Claim tiers

The local profiles are `supported` with `go_test` verification, except
`operations.index_status` in `local_lightweight`, which is `unsupported`: both
its paths read the repository count from the graph, and the lightweight
profile wires a driverless graph reader (a scoped caller gets a 500, an
unscoped caller a silent `repository_count` 0). The
`production` profile of every new key is `experimental`: the committed
evidence is handler tests only, and no deployed run or
`docs/internal/remote-validation/<slug>.md` artifact exists for these routes.
Each key stays experimental until a deployed run proves its route end to end
(see [Claim Evidence Locations](../claim-evidence-locations.md)).

## No-Regression Evidence

No-Regression Evidence: no SQL, Cypher, queue, lease, batching, or
concurrency change. The only runtime change is the capability argument passed
to the existing `WriteGraphReadError` call at 12 sites. The verdict switch,
status, `Retry-After`, code, and message are unchanged, and the success path is
untouched. Test proof: the status, evidence, operator-control-plane, and audit
fence tests now require the exact key in `error.capability`, and the OpenAPI
spec tests require each operation description to name its key. The audit test
failed before the change with `fence envelope capability = <nil>`.

## Observability Evidence

No-Observability-Change: no instrument, span, or log key was added, removed,
or renamed. The `error.capability` field in the reader-fence `503` envelope is
the operator signal: it says which route a stale or timed-out PostgreSQL
reader fenced, where those routes carried no name before.
