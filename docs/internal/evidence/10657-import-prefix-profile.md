<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# #10657 import prefix + external-only filter: index proof

Refs eshu-hq/eshu#10657. This record is the tracked measurement-first proof
for the `target_module_prefix` (`STARTS WITH`) predicate and the
`external_only` exclusion on `investigate_import_dependencies`. The claim
under proof: a prefix lookup on `Module.name` is cheap enough for the hot
path because the `module_name_lookup` index serves it. The issue stops at
this proof if the plan scans every Module; it does not.

## Setup (isolated, pinned backend)

| Item | Value |
|---|---|
| Neo4j | `neo4j@sha256:6c162e2432f861f2c4e3da77a6ba478e7f10e2160b8705412f85294532bc6ff5f` (same pin as `scripts/verify-query-plan-profile.sh`), Kernel 2026.05.0, Cypher 25, COST/SLOTTED |
| Schema | `module_import_key` UNIQUE RANGE on `Module(import_key)` + `module_name_lookup` RANGE on `Module(name)`, both ONLINE (from `go/internal/graph/schema_tables.go`) |
| Dataset | 30,000 `Module` nodes (> 25,000 `ImportDependencyInternalScanLimit`), 29,940 `IMPORTS` edges, 500 `File`s, 1 `Repository` |
| Families | `@scope/pkg-*` 5000, `github.com/acme/svc*` 5000, `acme.svc.mod*` 5000, `./x-*` 5000, `../y-*` 5000, `bare-pkg-*` 5000 |

## PROFILE evidence (all shapes: `NodeIndexSeekByRange`, zero whole-graph scans)

| Query shape | Anchor operator | Indexes used | DbHits | Rows | Forbidden ops present |
|---|---|---|---|---|---|
| Focused: `MATCH (m:Module) WHERE m.name STARTS WITH '@scope/' RETURN m.name ORDER BY m.name LIMIT 25` | `NodeIndexSeekByRange` on `RANGE INDEX m:Module(name)`, ordered `m.name ASC` from index | `module_name_lookup: 1` | 27 | 25 | none (`AllNodesScan`, `NodeByLabelScan`, `CartesianProduct`, `Eager` all absent) |
| Hot-path: `MATCH (repo:Repository)-[:REPO_CONTAINS]->(source_file:File)-[rel:IMPORTS]->(target_module:Module) WHERE target_module.name STARTS WITH '@scope/' RETURN DISTINCT ... ORDER BY target_module SKIP 0 LIMIT 25` | same `NodeIndexSeekByRange` anchor, then `Expand(All)` + label filters | `module_name_lookup: 1` | 227 | 25 | none |
| `STARTS WITH 'github.com/acme/'` (trailing `/`) | `NodeIndexSeekByRange` | `module_name_lookup: 1` | 27 | 25 | none |
| `STARTS WITH 'acme.svc.'` (trailing `.`) | `NodeIndexSeekByRange` | `module_name_lookup: 1` | 27 | 25 | none |

`EXPLAIN` on the focused shape plans `READ_ONLY` under COST; `PROFILE`
above is the binding proof.

## Row counts per acceptance case

| Prefix | Rows matched | Note |
|---|---|---|
| `@scope/` | 5000 | family search works |
| `github.com/acme/` | 5000 | Go import paths work |
| `acme.svc.` | 5000 | dotted Python names work |
| `./x-` | 5000 | relative names also seek the index (the external-only exclusion is a separate predicate) |
| `` (empty) | 30000 (every Module) | confirms empty prefix must be rejected in validation |
| exact match `{name: '@scope/'}` (today's shape) | 0 | confirms the failing-test setup: the prefix string returns no rows on exact match |

## Consequences for the build

- No new index needed: the existing `module_name_lookup` RANGE index serves
  every prefix shape, including trailing `.`/`/`, and supplies `ORDER BY name`.
- The 25,000-row scan refusal and the `SKIP <= 10000` cap stay as-is; the
  seek touches ~27 db hits per 25-row page regardless of the 30k population.
- New queryplan pins `QP-CODE-IMPORT-PREFIX-ROWS` and
  `QP-CODE-IMPORT-PREFIX-PACKAGES` bind the exact production prefix shapes;
  old pins are untouched.

## Performance Evidence:

Prefix `STARTS WITH` on `Module.name` seeks `module_name_lookup`
(`NodeIndexSeekByRange`, 27 DbHits per 25-row page) on the pinned Neo4j at a
30,000-`Module` population, versus the unmeasured exact-match-only baseline
which returns no rows for a family prefix by construction. The external-only
predicates (`NOT ... STARTS WITH '.'/'/'`) are static range exclusions on the
same indexed property. No index DDL: incremental write/storage cost is zero
by unchanged schema.

## Observability Evidence:

Existing `eshu.import_dependencies.*` span attributes
(`query_type`, `result_count`, `truncated`, `has_more`, `scan_overflow`) and
the response `scope` echo (now including `target_module_prefix` and
`external_only`) expose which filter ran; no new telemetry was needed.
