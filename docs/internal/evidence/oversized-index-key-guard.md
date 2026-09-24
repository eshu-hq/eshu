# Oversized Index Key Guard Evidence

Issue #7058: in a 984-repository Neo4j trial (commit 8d5949f90) a 32,473-byte
`Module.name` produced by the parser bug in #7056 hit the `module_name_lookup`
range index. Neo4j rejected it with
`Neo.DatabaseError.Statement.ExecutionFailed: Property value is too large to index`,
and because the canonical write is one atomic transaction per repository, none
of that repository's graph was written. NornicDB accepts such values silently.

The change adds `canonical.DropOversizedIndexKeys`, called by
`CanonicalNodeWriter.Write` before any statement is built. It removes Module
rows whose name, entity rows whose name plus path, and Parameter rows whose
name plus path exceed `canonical.MaxIndexedKeyBytes` (8000 UTF-8 bytes), plus
IMPORTS, HAS_PARAMETER and CONTAINS rows that reference those keys. The node is
absent rather than written with a truncated or hashed identity. Rows at or
under the bound are unchanged, and when nothing is dropped the materialization
is returned with its original slices.

## Limit measurement

Measured by binary search against `neo4j:2026-community@sha256:eabfbb04…`
(Neo4j Kernel 2026.08.1, `range-1.0`), one CREATE per probe:

| Key shape | Largest accepted string bytes |
| --- | --- |
| `(:Module {name})`, ASCII | 8164 |
| same, 2-, 3-, 4-byte runes | 8164, 8163, 8164 |
| `(:Function) (name, path, line_number)` unique | 8151 total for name + path |

The limit counts encoded bytes, not characters. The source cap is
`DynamicSizeUtil.FIXED_MAX_KEY_VALUE_SIZE_CAP = 8175`, enforced by
`GenericIndexKeyValidator` at commit (neo4j `2026.06` source). 8000 leaves at
least 151 bytes for extra key slots such as `kind` on `K8sResource` and
`CrossplaneClaim`.

Root-Cause Evidence: `TestLiveCanonicalWriteSkipsOversizedIndexKeys` with the
`dropOversizedIndexKeys` call removed from `Write` fails on that Neo4j image with
`canonical atomic write: Neo4jError: Neo.DatabaseError.Statement.ExecutionFailed (Property value is too large to index ... Index( id=11, name='module_name_lookup', type='RANGE' ...`,
the trial error. With the guard it passes: repository, file, normal Function,
normal Module and its IMPORTS edge are written; the 32,473-byte Module and the
8,053-byte Function are absent; an 8,000-byte Module name and an 8,000-byte
Function (name + path) key are both written.

No-Regression Evidence: no Cypher text, batching, transaction shape, or
statement order changes. The guard is one pass of `len()` checks over the row
slices. `BenchmarkDropOversizedIndexKeysNoneDropped` (50,000 entities, 5,000
modules, 20,000 imports, 20,000 parameters, 15,000 class/nested rows, nothing
dropped) runs in about 365 µs with 0 allocations on an Apple M5 Max
(`go test ./internal/projector/canonical -bench DropOversizedIndexKeys -count=3`:
367049, 364861, 364731 ns/op). A canonical write of a repository that size
takes seconds, so the added cost is well under 0.1%.
`TestDropOversizedIndexKeysLeavesNormalMaterializationUntouched` pins that the
common path returns identical rows and the same backing arrays.
`go test ./internal/storage/cypher/... ./internal/projector/... ./internal/telemetry/... -count=1`
passes.

Observability Evidence: new counter
`eshu_dp_canonical_oversized_index_keys_skipped_total` labeled `node_label`
(closed canonical label set) and `property` (`name` or `path`), plus one WARN
per skipped node, `canonical row skipped: indexed value exceeds key size limit`,
carrying `scope_id`, `repo_id`, `generation_id`, `node_label`, `property`,
`key_bytes`, `limit_bytes`, `entity_id`, `file_path` and a 64-byte
`value_prefix`. The canonical write span gets `oversized_index_keys_skipped`.
Rows are in `docs/public/observability/telemetry-coverage.md`,
`docs/public/reference/telemetry/metrics.md` and
`docs/public/reference/telemetry/metrics-reducer-storage.md`.
