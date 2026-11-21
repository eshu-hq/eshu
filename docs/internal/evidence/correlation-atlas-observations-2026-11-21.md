# Correlation Atlas Observations, 2026-11-21

Read-only readings from the QA environment that back the statements marked
**Observed** in the [Correlation Atlas](../correlation-atlas/README.md). Each
query ran with `default_transaction_read_only = on`.

These are single readings on one day. They show what the system held at that
moment. They do not show why. Counts are recorded in
`docs/internal/measurements.jsonl` under the ids shown as `ledger:<id>`.

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
| Either | Any finding kind | 0 (`ledger:10860-atlas-score-scope-findings`) |

The winners table, grouped by the source family of `winner_scope_id`, held
73,317 winners from OSV scopes and 6,268 from GHSA scopes, and none from an
EPSS, KEV, or NVD scope.

```sql
SELECT split_part(replace(winner_scope_id, 'vuln-intel://', ''), '/', 1), count(*)
FROM supply_chain_impact_canonical_winners GROUP BY 1;
```

## 2. No impact work item named a score scope as its origin

Question: which scope family does each `supply_chain_impact` work item name as
its fan-out origin? The query has no status filter, so it counts work items in
every status.

```sql
SELECT split_part(replace(coalesce(payload->'fanout_origin'->>'scope_id', ''),
                          'vuln-intel://', ''), '/', 1) AS origin_family,
       count(*)
FROM fact_work_items
WHERE stage = 'reducer' AND domain = 'supply_chain_impact'
  AND scope_id LIKE 'vuln-intel://%'
GROUP BY 1;
```

Of 7,223 work items (`ledger:10860-atlas-impact-work-items`): 5,587 named an
OSV scope as origin, 70 a GHSA scope, 196 a repository scope, 2 a
security-alert scope, and 1,368 carried no origin. None named an EPSS or KEV
scope (`ledger:10860-atlas-impact-work-items-score-origin`). A work item keeps only its latest origin, so this
does not show what opened earlier runs.

## 3. A finding shows the score that was live when it was written

Sample: 40 owner scopes, 223 scored vulnerabilities, 30,895 finding rows, and
the EPSS records of all 20 EPSS scopes across every snapshot. The comparison
was done outside the database, from two result sets:

```sql
-- finding rows of the sampled owner scopes at their active snapshot
SELECT f.scope_id, f.payload->>'cve_id', f.payload->>'epss_probability', f.ingested_at
FROM ingestion_scopes s
JOIN fact_records f
  ON f.scope_id = s.scope_id AND f.generation_id = s.active_generation_id
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone
  AND s.scope_id = ANY($1) AND f.payload->>'cve_id' = ANY($2);

-- every EPSS record for the same vulnerabilities, with its snapshot's activation time
SELECT f.payload->>'cve_id', f.payload->>'probability', g.status, g.activated_at
FROM scope_generations g
JOIN fact_records f
  ON f.scope_id = g.scope_id AND f.generation_id = g.generation_id
WHERE g.scope_id LIKE 'vuln-intel://first_epss/%'
  AND f.fact_kind = 'vulnerability.epss_score' AND f.payload->>'cve_id' = ANY($2);
```

For the 46 vulnerabilities whose EPSS probability changed in that history,
3,741 finding rows (`ledger:10860-atlas-score-changed-rows`):

| The row shows | The row was last written | Rows |
|---|---|---|
| The current probability | After the change | 3,551 (`ledger:10860-atlas-score-changed-rows-current`) |
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

- Measurement: 4/4 runs skipped, Grafana (`ledger:10860-atlas-grafana-refresh-skipped`)
- Measurement: 4/4 runs skipped, Loki (`ledger:10860-atlas-loki-refresh-skipped`)
- Measurement: 5/5 runs skipped, Kubernetes (`ledger:10860-atlas-kubernetes-refresh-skipped`)
- Measurement: 4/4 runs skipped, Vault (`ledger:10860-atlas-vault-refresh-skipped`)

Not checked: the Tempo and Prometheus/Mimir logs, and whether the observed
content changed between the stored snapshot and the reading.

## 5. The producer-activation consumer was off

Method: list every Deployment and StatefulSet in the namespace Eshu runs in as
JSON, and
search each container's `env` names for `PRODUCER_ACTIVATION` and for any
`envFrom` reference.

```bash
kubectl get deployments,statefulsets -o json
```

Of 50 containers in that one namespace, none set `ESHU_PRODUCER_ACTIVATION_CONSUMER_ENABLED`, and none
used `envFrom`.
