# Correlation Atlas Observations, 2026-11-21

Read-only readings from the QA environment that back the statements marked
**Observed** in the [Correlation Atlas](../correlation-atlas/README.md). Each
query ran with `default_transaction_read_only = on`.

These are single readings on one day. They show what the system held at that
moment. They do not show why.

## 1. Score scopes hold no findings

Question: do the EPSS and KEV scopes hold any `supply_chain_impact` finding?

```sql
SELECT split_part(replace(g.scope_id, 'vuln-intel://', ''), '/', 1) AS family,
       g.status, f.fact_kind, f.is_tombstone, count(*)
FROM scope_generations g
JOIN fact_records f
  ON f.scope_id = g.scope_id AND f.generation_id = g.generation_id
WHERE g.scope_id LIKE 'vuln-intel://first_epss/%'
   OR g.scope_id = 'vuln-intel://cisa/kev'
GROUP BY 1, 2, 3, 4;
```

| Family | Record kind | Records, all snapshot states |
|---|---|---|
| EPSS (20 scopes) | `vulnerability.epss_score` | 23,237 |
| EPSS | `vulnerability.source_snapshot` | 246 |
| KEV (1 scope) | `vulnerability.known_exploited` | 44,300 |
| KEV | `vulnerability.source_snapshot` | 25 |
| Either | Any finding kind | 0 |

The winners table held 73,317 winners from OSV scopes and 6,268 from GHSA
scopes, and none from an EPSS, KEV, or NVD scope.

## 2. No queued impact run came from a score scope

Question: which scope family opened each queued `supply_chain_impact` run?

```sql
SELECT split_part(replace(coalesce(payload->'fanout_origin'->>'scope_id', ''),
                          'vuln-intel://', ''), '/', 1) AS origin_family,
       count(*)
FROM fact_work_items
WHERE stage = 'reducer' AND domain = 'supply_chain_impact'
  AND scope_id LIKE 'vuln-intel://%'
GROUP BY 1;
```

Of 7,223 runs: 5,587 named an OSV scope as origin, 70 a GHSA scope, 196 a
repository scope, 2 a security-alert scope, and 1,368 carried no origin. None
named an EPSS or KEV scope. A work item keeps only its latest origin, so this
does not show what opened earlier runs.

## 3. A finding shows the score that was live when it was written

Sample: 40 owner scopes, 223 scored vulnerabilities, 30,895 finding rows, and
the EPSS records of all 20 EPSS scopes across every snapshot. For each finding
row the query returned the stored `epss_probability` and the row's
`ingested_at`; for each EPSS record, its probability and its snapshot's
`activated_at`.

For the 46 vulnerabilities whose EPSS probability changed in that history,
3,741 finding rows:

| The row shows | The row was last written | Rows |
|---|---|---|
| The current probability | After the change | 3,551 |
| The previous probability | Before the change | 180 |
| An older probability | Before the change | 8 |
| No probability | Before the change | 2 |

No row showed an old probability and was written after the change. The 190
rows that were behind sat in 4 owner scopes whose impact run had not completed
since the change.

Not checked: the known-exploited flag, and whether any finding in the sample
was anchored through `reducer_package_consumption_correlation` rows.

## 4. Observability snapshots are skipped as unchanged

Question: how many snapshots carry a constant freshness value?

```sql
SELECT freshness_hint, status, count(*), count(DISTINCT scope_id),
       min(ingested_at), max(ingested_at)
FROM scope_generations
WHERE freshness_hint LIKE '%\_observed\_metadata' OR freshness_hint = 'snapshot'
GROUP BY 1, 2;
```

| Freshness value | Snapshots | Stored |
|---|---|---|
| `grafana_observed_metadata` | 1, active | 2026-10-26 |
| `loki_observed_metadata` | 1, active | 2026-10-26 |
| `prometheus_mimir_observed_metadata` | 1, active | 2026-10-26 |
| `tempo_observed_metadata` | 1, active | 2026-10-26 |
| `snapshot` | 2 active, 1 superseded | 2026-10-30 |

The same query for the values `complete` and `partial`:

| Scope prefix | Freshness value | Snapshots | Stored |
|---|---|---|---|
| `kubernetes_live` | `complete` | 1, active | 2026-10-27 |
| `vault_live` | `complete` | 1 active, 1 superseded | 2026-10-29, 2026-10-28 |
| `vault_live` | `partial` | 2 superseded | 2026-10-28 |

Collector logs over about two hours before the reading:

| Collector | Runs | Records returned per run | Outcome of every run |
|---|---|---|---|
| Grafana | 4 | 88 | `ingestion refresh skipped`, reason `unchanged` |
| Loki | 4 | 53 | Same |
| Kubernetes | 5 | 6,074 | Same |
| Vault | 4 | 13 | Same |

Each skip named the old snapshot as the match.

Not checked: the Tempo and Prometheus/Mimir logs, and whether the observed
content changed between the stored snapshot and the reading.

## 5. The producer-activation consumer was off

Method: list every Deployment and StatefulSet in the namespace as JSON, and
search each container's `env` names for `PRODUCER_ACTIVATION` and for any
`envFrom` reference.

```bash
kubectl get deployments,statefulsets -o json
```

Of 50 containers, none set `ESHU_PRODUCER_ACTIVATION_CONSUMER_ENABLED`, and none
used `envFrom`.
