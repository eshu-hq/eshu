# #10397 readback: owned-package OSV coverage under the extracted satellite

Read-only readback for `eshu-hq/eshu#10397`, measured 2026-11-08 against the
QA read replica (`vulnerability_source_state_packages` for the QA vuln-intel
satellite instance named on the issue). Nothing was written outside this note.
Every query ran with `statement_timeout = '120s'`; the password came from
the `eshu-postgresql-auth` secret into `PGPASSWORD` and was never printed or
committed.

## Result

Owned set = the reader's exact predicate
(`go/internal/storage/postgres/owned_package_targets.go`: active generation
set, alias-folded ecosystem with sbt kept, 11 configured ecosystems plus sbt),
collapsed to `DISTINCT (ecosystem, package_name)`. Covered = at least one
satellite OSV scope key row for the instance, joining owned `go` to stored
`gomod`, owned `sbt` to stored `maven` (unsuffixed name or binary artifact),
and all others identically, on the lowercase consumption-key form the recorder
always stores (`derivedTargetPackageKeys` keeps lowercase plus normalized).

| ecosystem | owned | covered | missing | covered % |
|---|---:|---:|---:|---:|
| npm | 7,667 | 4,901 | 2,766 | 63.9 |
| maven | 466 | 202 | 264 | 43.3 |
| pypi | 621 | 290 | 331 | 46.7 |
| go | 439 | 220 | 219 | 50.1 |
| composer | 359 | 187 | 172 | 52.1 |
| rubygems | 220 | 122 | 98 | 55.5 |
| cargo | 3 | 1 | 2 | — |
| nuget | 7 | 2 | 5 | — |
| pub | 1 | 1 | 0 | — |
| sbt | 2 | 1 | 1 | — |
| **total** | **9,785** | **5,927** | **3,858** | **60.6** |

Owned denominators reproduce the coordinator shim exactly (9,785 total, every
per-ecosystem count identical), so the owned side of the shim is confirmed.

## Shim reconciliation

The shim's 6,132 covered is a key-side count (`DISTINCT (ecosystem,
package_name)` over stored keys), not an owned-side count. Stored maven keys
hold both the lowercase `group:artifact` form and the normalized
`group/artifact` form of each covered package (406 distinct names over
502 rows for ~202 packages), so maven reads double. True maven coverage is
**202/466 (43.3%)**, not 406. The same dual-form effect adds ~8 to go
(228 key-side vs 220 owned-side). Residual deltas (npm 4,902 vs 4,901, pypi
283 vs 290) are case/verbatim join artifacts of ±8 packages; the direction
differs because the shim matched verbatim while this readback matches the
lowercase key form the recorder guarantees.

## Key-side integrity

14,078 key rows over 1,532 distinct scopes, all 1,532 present in
`vulnerability_source_states` (zero orphan scopes). 1,586 osv scopes exist, so
54 scopes carry no keys; all 54 are `fresh` and date from the 10-29 seed
window. No `_slot` scopes exist: the satellite plans legacy single/batch
scopes only (832 batch, 754 single).

## Trajectory

New osv scopes land at ~150/day, every day since 10-29 (94 on the partial
day 11-08), and `max(updated_at)` is today. Planning is progressing, not
stalled. Rotation math at the effective defaults (`target_limit` 100,
rotating, 30-minute passes, T = 32,828 package@versions on 11-08 per #9222):
P = 1024 slots, 48 passes/day, full rotation 21.3 days from 10-29, so ~52%
of slots visited at readback against 60.6% package coverage. ETA for a full
rotation is ~11-19, when coverage should read ~100% if the rotation keeps
pace. Per-scope freshness is observable (`updated_at`, all osv scopes
`fresh`); per-package freshness is not (keys carry no timestamps,
`vulnerability_target_refresh_progress` holds 0 rows).

## What plans the missing packages (criterion 2)

The satellite activation config (`eshu-component-activations`, the satellite
entry) enables `derive_from_owned_packages` for the same 11 ecosystems over
`osv`, with no `target_limit` and no `planning_mode`: L = 100, rotating. The
host path is `scheduleVulnerabilityActivationDerivedScopes`
(`go/internal/coordinator/component_extension_service.go`, `scheduleVulnerabilityActivationDerivedScopes`): it reads one
clock hash-slot through `vulnerabilityOwnedTargets` with a **zero
`OwnedTargetSlotSelector`** (slot progress is in-tree only), and
`PlanActivationDerivedWork` mints the batch/single scopes the satellite then
executes. The satellite does no owned-package planning of its own. Nothing
else plans a missing package sooner: the activation path has no OSV delta
replan (no `OSVDelta` in its plan request) and records no owned-target
refresh progress (it records findings scopes only), so the empty progress
table is by construction, not by failure. Claim-to-scope evidence is the
state rows themselves: 1,586 executed osv scopes, steady cadence, zero slot
scopes.

## Narrowed gap (criterion 3)

Coverage completion needs no code change yet: re-run the query below after
~11-19 and expect ~100% per ecosystem. If it lands, the remaining gap is
freshness observability only: per-package signal requires either wiring
`refresh_interval` record (and/or slot progress) through the activation
path, or adopting the scope-`updated_at` readback as the standing freshness
signal. If coverage stalls short of ~100%, file the core scheduling change
against the activation rotation (missed slots, guard-dropped scopes, or
replan wiring), with acceptance = this query at ~100% plus a per-package
freshness signal. Either way the standing readback is the query below.

<details>
<summary>Coverage query (read-only, statement_timeout 120s)</summary>

Bind `:satellite_instance` to the satellite instance id named on #10397
(`psql -v satellite_instance="'<id>'"`), then run:

```sql
WITH active_dependencies AS (
  SELECT
    CASE LOWER((fact.payload->'entity_metadata'->>'package_manager') COLLATE "C") COLLATE "default"
      WHEN 'gradle' THEN 'maven'
      WHEN 'gomod' THEN 'go'
      WHEN 'go-module' THEN 'go'
      WHEN 'go_module' THEN 'go'
      ELSE LOWER((fact.payload->'entity_metadata'->>'package_manager') COLLATE "C") COLLATE "default"
    END AS ecosystem,
    fact.payload->>'entity_name' AS package_name,
    fact.payload->'entity_metadata'->>'sbt_binary_artifact' AS sbt_binary_artifact
  FROM fact_records AS fact
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = fact.scope_id
   AND scope.active_generation_id = fact.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = fact.scope_id
   AND generation.generation_id = fact.generation_id
  WHERE fact.fact_kind = 'content_entity'
    AND fact.source_system = 'git'
    AND fact.is_tombstone = FALSE
    AND fact.payload->>'entity_type' = 'Variable'
    AND fact.payload->'entity_metadata'->>'config_kind' = 'dependency'
    AND fact.payload->'entity_metadata'->>'package_manager' = ANY('{cargo,composer,go,go-module,go_module,gomod,gradle,hex,maven,npm,nuget,pub,pypi,rubygems,sbt,swift}'::text[])
    AND generation.status = 'active'
    AND fact.generation_id = ANY (ARRAY(SELECT active_generation_id FROM ingestion_scopes WHERE active_generation_id IS NOT NULL))
),
owned AS (
  SELECT ecosystem, package_name,
         LOWER(package_name) AS lname,
         REPLACE(LOWER(package_name), '/', ':') AS maven_flat,
         COALESCE(array_agg(DISTINCT LOWER(sbt_binary_artifact)) FILTER (WHERE NULLIF(sbt_binary_artifact, '') IS NOT NULL), '{}') AS sbt_arts
  FROM active_dependencies
  GROUP BY 1, 2
),
flagged AS (
  SELECT owned.ecosystem, owned.package_name,
         EXISTS (
           SELECT 1 FROM vulnerability_source_state_packages k
           WHERE k.collector_instance_id = :'satellite_instance'
             AND k.ecosystem = CASE WHEN owned.ecosystem = 'sbt' THEN 'maven'
                                    WHEN owned.ecosystem = 'go' THEN 'gomod'
                                    ELSE owned.ecosystem END
             AND (k.package_name = owned.lname
               OR (owned.ecosystem IN ('maven', 'sbt') AND REPLACE(k.package_name, '/', ':') = owned.maven_flat)
               OR (owned.ecosystem = 'sbt' AND k.package_name = ANY(owned.sbt_arts)))
         ) AS covered
  FROM owned
)
SELECT ecosystem,
       count(*) AS owned_packages,
       count(*) FILTER (WHERE covered) AS covered_packages,
       count(*) FILTER (WHERE NOT covered) AS missing_packages
FROM flagged GROUP BY 1 ORDER BY 1;
```

</details>

## Commands (all read-only)

- Instance census: `SELECT collector_instance_id, count(*) FROM
  vulnerability_source_state_packages GROUP BY 1` → the satellite instance
  alone holds key rows (14,078).
- Key ecosystems: npm 11,701; os 700; pypi 522; maven 502; composer 254;
  gomod 243; rubygems 152; nuget 2; cargo 1; pub 1.
- States: osv 1,586 fresh (10-29 → today); nvd 6 failed; cisa_kev 1
  partial; `vulnerability_target_refresh_progress` 0 rows.
- Scope shapes: 832 batch, 754 single, 0 slot.
- Cadence: ~150 osv scopes/day since 10-29; 54 keyless scopes all fresh,
  all from 10-29.
- Config: the `eshu-component-activations` configmap, satellite entry.
