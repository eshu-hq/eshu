# #6851 File Import Cycles: Bounded Multi-Node Python Enumeration

## Scope

`file_import_cycles` answered reciprocal Python pairs only. This lane adds
bounded simple-cycle enumeration (reciprocal pairs through `max_cycle_length`,
default 5, range 2-8) with rotation dedupe, deterministic order, and a hard
1,000-cycle enumeration cap. Epic #6833 is out of scope. Per the arbiter
verdict posted on #6851, type-only/deferred exclusion, inferred-edge
labelling, and non-Python languages stay deferred to follow-ups: those flags
do not reach the graph (edges carry only `imported_name`, `alias`, and
`line_number`).

## Theory proof (read-only PROFILE on the deployed Neo4j graph)

Largest repository `portal-java-ycm`: 38,240 `IMPORTS` edges (java 34,865,
tsx 2,441, typescript 870, python 46, javascript 18) over 3,387 importing
files. Largest Python corpus `trident-automation`: 4,522 Python edges over
626 files and 930 modules, 172 resolved in-repo.

| Shape | Time | DbHits | Rows |
| --- | ---: | ---: | ---: |
| Bounded edge fetch (4,522 Python edges) | 40ms | 27,058 | 4,522 |
| Cypher reciprocal join over the same edges | 1,522ms | 5.5M | 0 |

The variable-length Cypher alternative was rejected on these numbers;
enumeration runs in-process over the single bounded fetch. No Cypher text
changed and the 25,000-row scan ceiling still fails closed with HTTP 422.

## Local proof (pinned `neo4j:2026-community`, kernel 2026.08.1)

Seeded `proof-cycles`: 4,218 `IMPORTS` edges (2-, 3-, and 5-node Python
cycles, a diamond negative, plus bulk noise at trident scale). Fixture
intent, graph truth, and API/MCP truth agree: the edge census reads 4,218,
the handler returns exactly cycles `[2 3 5]` with `count` 3 and
`truncated` false, and `investigate_import_dependencies` via MCP dispatch
returns the same three cycles through the same handler.

Cold 276ms; 21 immediate repeats: warm p50 25ms, warm p95 34ms, warm max
38ms — under the 1s budget at repo scale.

Unit fixtures prove 2-, 3-, and 5-node positives, the diamond negative,
rotation dedupe with duplicate-edge earliest-line retention, the
1,000-cycle truncation flag with the cap value, the `max_cycle_length`
bound and its validation, and no-regression on the reciprocal path.

## Observability

No spans, metrics, structured logs, status fields, or pprof surface were
added, removed, or renamed. The existing
`query.import_dependency_investigation` span attributes (`result_count`,
`truncated`, `scan_overflow`) and the truth envelope are unchanged.
