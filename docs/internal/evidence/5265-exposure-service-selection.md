# Exposure Service Selection Evidence

This note records the focused correctness and performance proof for issue
#5265. The change makes Exposure Path resolve human input to a canonical
workload before loading service context, while keeping ambiguous, unauthorized,
missing, and truncated results explicit.

## Theory proof

The old generic entity query authorized a `Workload` through
`Repository-[:REPO_CONTAINS]->File-[:CONTAINS]->entity`. Canonical workload
ownership is instead represented by `w.repo_id` and, for legacy or shared
workloads, `Repository-[:DEFINES]->Workload`.

The old and proposed property-anchored shapes were run on the same retained
NornicDB corpus with `name=api-node-boats`, `type=Workload`, and `limit=11`:

| Query | Result | Duration |
| --- | --- | ---: |
| Old unscoped generic entity scan | one workload ID | 13.642678041s |
| New unscoped `MATCH (w:Workload)` property read | identical workload ID | 1.092792ms |
| Old scoped `CONTAINS` ownership | zero rows (wrong) | 14.204501333s |
| New scoped `w.repo_id` ownership | one authorized workload (expected delta) | 7.349333ms |

The output-preserving unscoped comparison had identical workload IDs. The
scoped comparison intentionally differs because the old query returned the
wrong authorization result.

NornicDB returned a literal projection placeholder during an earlier
`OPTIONAL MATCH` experiment. That result was rejected. Production code does
not project a synthetic empty repository name and hydrates real repository
names through a separate bounded raw repository lookup.

A second retained probe exercised the exact grouped relationship fallback:

```cypher
MATCH (w:Workload)<-[:DEFINES]-(repo:Repository)
WHERE w.name = $name
RETURN w.id, labels(w), w.name, min(repo.id) AS repo_id
ORDER BY w.id
LIMIT $limit
```

The representative workload `iac-eks-argocd` had two `DEFINES` edges. The
query returned one unique workload row, a real repository ID (not a projection
placeholder), and completed in 67.296625ms. This proves the limit applies to
unique workload identities rather than relationship-edge rows.

## Performance Evidence:

Exact API input hash:
`b85f8c231ab2e0cd9a06c55b05a23ad90edf5dca9a0a646b17f2b946bdeb80aa`.
The isolated API image was
`sha256:f56d2cb21530d16e273a26ebf8def71eb45e32116524498e36bb026f9c68ea55`.
The retained backend was `eshu-nornicdb-pr261:149245885258`, image
`sha256:8b2207ec7f53836a29c375cf924744fdfeec0e70ec902e66aa22fa0ad648d1bb`.

The corpus was operator-attested and live-inventory validated at 887
repositories. It contained 5,232 indexed entities, including 76 workloads,
332 modules, and 3,737 cloud resources.

The exact-branch authenticated browser-session route proof selected
`api-node-boats`. The combined submit flow completed in 3.000s and recorded:

- `POST /api/v0/entities/resolve` -> HTTP 200;
- `GET /api/v0/services/:service/context` -> HTTP 200;
- one visible exposure result, chain selector, and ingress panel;
- zero network errors.

This total uses the same user action as its start event and visible ingress
truth as its terminal event. It includes resolver, context, browser rendering,
and the route workflow wait rather than presenting a non-comparable raw query
time as the user-facing total.

## No-Regression Evidence:

Focused regression commands:

```bash
cd go && go test ./internal/query -run 'TestResolveEntityWorkload|TestListCatalog(TruncatesEachCollectionByLimit|DistinguishesRepositoryOnlyTruncation)|TestOpenAPICatalogDistinguishesWorkloadTruncation' -count=1
npm test -- --run src/api/exposureServiceSelection.test.ts src/api/eshuConsoleSections.test.ts src/pages/ExposurePathPage.test.tsx src/pages/ExposurePathPageHistory.test.tsx src/components/ServiceDrawer.test.tsx
npm run typecheck
```

The retained proof used `scripts/run-console-retained-e2e.sh` with an isolated
auth schema and exact API sidecar, `ESHU_E2E_ROUTE_PATHS=/exposure`, and
`ESHU_E2E_SERVICE_NAME=api-node-boats`. It passed 1/1 selected routes. The
proof runner hash was
`aabf0f1fc69f0cd2ab2ea7b08191a427037060940c0171d9d8c6112dc7b78338`.

A separate authenticated retained-browser check on the branch UI submitted
`definitely-not-a-retained-service-5265`. The page rendered the precise
no-authorized-service selector alert, cleared the prior ingress result, and
made no service-context request for the invalid value.

Regression coverage also proves exact catalog aliases preserve canonical IDs,
duplicate display names do not guess, invalid names clear prior results, an
externally removed deep-link parameter clears stale ingress truth, scoped
ownership is applied before limits, multiple `DEFINES` edges cannot consume
the unique-workload page, and workload resolution never falls back to content
entities. A missing graph backend returns HTTP 503 with a precise unavailable
message instead of falsely reporting an authoritative empty HTTP 200 result.

## No-Observability-Change:

The change uses the existing entity-resolver and service-context HTTP routes,
truth envelopes, graph query spans, request logs, and duration instrumentation.
It adds no metric instrument, metric label, span name, queue, worker, graph
write, collector call, runtime flag, or deployment knob. Operators continue to
diagnose selection through the existing HTTP status/envelope, bounded resolver
`count`/`limit`/`truncated` fields, service-context truth/freshness, and route
performance evidence.
