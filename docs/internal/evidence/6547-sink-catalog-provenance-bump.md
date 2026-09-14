# #6547: sink catalog Provenance repoint and SinkCatalogVersion bump

This change repoints every dead `reducer/*.go` path in the `Provenance` strings
of `go/internal/exposure/sink_catalog.go` in one deliberate version bump. The
owner chose to bump now rather than keep deferring.

## Version bump

| | `SinkCatalogVersion()` |
| --- | --- |
| Before | `6db744b6723ff2943dd78c5ceeb095f9479b489f921708dd7c4bfd94191a6ed3` |
| After | `91a71ebd48ef65ff372bda82c38c62d12525192ac9bffbf9ac82c2d0dcdd970e` |

`hashSinkSpecs` serializes `Provenance` as the last field of each spec's
hashed line, so a path-only correction changes the version. The four repoints
change nothing else: kind, display name, relationship, target label, predicates,
severity, and graph-backed status are byte-identical. `MatchSink` recognition
is unchanged.

## What the bump invalidates

In practice, nothing at runtime. The issue assumed the bump would invalidate
every cached reachability finding. The code shows that no such cache exists
yet:

- `SinkCatalogVersion()` has no caller outside `go/internal/exposure`.
  `rg -n 'SinkCatalogVersion|sinkCatalogVersionGolden' --glob
  '!go/internal/exposure/**' .` returns only two prose mentions in
  `docs/internal/design/3157-pdg-taint-evaluation.md`.
- No Postgres column, fact payload, or queue row stores it. The
  `catalog_version` hits under `go/` and `testdata/` belong to the
  vulnerability-intelligence KEV catalog (`2026.06.24`), which is unrelated.
- No API or MCP field returns it, and none returns `Provenance`.
  `exposureSinkPayload` (`go/internal/query/impact/exposure_path_mapping.go`)
  emits only `kind`, `display_name`, and the sink node. The reducer value-flow
  loader reads specs through `MatchSink` and never persists `Provenance`.
- No golden snapshot or cassette contains the version or any sink `Provenance`
  text. The one filename match,
  `testdata/cassettes/replayoffline/s3-internet-exposure-materialization.cost-budget.json`,
  contains no `provenance`, `catalog_version`, or `sink` string.

The only pinned value is `sinkCatalogVersionGolden`, which
`TestSinkCatalogVersionIsStableAndChangeSensitive` checks, and this change
re-pins it. No recompute, reindex, or backfill is needed. If a future consumer
starts keying findings on the version, a findings cache built before this
change would be recomputed on its next run, because the key no longer matches.

## Repoint table

Paths are relative to `go/internal/`. "Resolves" was checked with `test -e`
from `go/internal`.

| Spec | Before | Resolves | After | Resolves |
| --- | --- | --- | --- | --- |
| `SinkIAMPrivilegedAction` / `CAN_PERFORM` | `reducer/iamcan/iam_can_perform_materialization.go` | yes | unchanged | yes |
| `SinkIAMPrivilegedAction` / `CAN_ESCALATE_TO` | `reducer/iam_escalation_materialization.go` | **no** | `reducer/iamescalation/iam_escalation_materialization.go` | yes |
| `SinkIAMPrivilegedAction` / `CAN_ASSUME` | `reducer/iamcan/iam_can_assume_edge_rows.go` | yes | unchanged | yes |
| `SinkSecretReference` / `SECRETS_IAM_GRANTS_SECRET_READ` | `reducer/secretsiam/secrets_iam_graph_projection_extract.go` | yes | unchanged | yes |
| `SinkSQLTable` / `QUERIES_TABLE` | `reducer/sql_relationship_materialization.go` | **no** | `reducer/sqlrelationship/sql_relationship_embedded_query.go` | yes |
| `SinkSQLTable` / `QUERIES_TABLE` (second path) | `storage/cypher/edge_writer_sql.go` | yes | unchanged | yes |
| `SinkInternetEndpoint` / `TO` | `reducer/security_group_reachability.go` | **no** | `reducer/secgroup/security_group_reachability.go` | yes |
| `SinkShellExec` / `EXECUTES_SHELL` | `reducer/shell_exec_materialization.go` | **no** | `reducer/code/shell/handler.go` | yes |
| `SinkShellExec` / `EXECUTES_SHELL` (second path) | `storage/cypher/edge_writer_shell_exec.go` | yes | unchanged | yes |

The two non-graph-backed specs (`config_security_key`, `iac_misconfiguration`)
cite #3191 prose and no file path.

Why each destination was chosen:

- **CAN_ESCALATE_TO**: `reducer/iamescalation/iam_escalation_materialization.go`
  is the handler that writes the edge ("materialized %d CAN_ESCALATE_TO
  edge(s)", line 213). It is the same file the old path named, now inside the
  family subpackage.
- **EXECUTES_SHELL**: the `shell_exec` family moved into `reducer/code/shell/`
  while this change was in flight (#6061). `handler.go` is the half that builds
  the row: `rg -c EXECUTES_SHELL` finds 3 matches there and 0 in `intents.go`.
  The new path-existence guard this change adds is what caught it — the gate
  failed on the stale path before this repoint, which is the guard working.
- **QUERIES_TABLE**: the `sqlrelationship` family was split, and the half that
  kept the old filename is the wrong one. `rg -c QUERIES_TABLE` finds zero
  matches in `sqlrelationship/sql_relationship_materialization.go`. The
  `Function-[:QUERIES_TABLE]->SqlTable` row is built in
  `sqlrelationship/sql_relationship_embedded_query.go` (edge key at line 37,
  `"relationship_type": "QUERIES_TABLE"` at line 49).
  `ExtractSQLRelationshipRows` reaches it through `appendEmbeddedSQLQueryRows`.
  The spec comment already describes this as "promotes parser embedded-query
  evidence".
- **TO → CidrBlock{is_internet:true}**:
  `reducer/secgroup/security_group_reachability.go` builds the
  `SecurityGroupRule -[:TO]-> endpoint` row carrying `is_internet` (lines
  185-199) and resolves the `CidrBlock` target label (line 268). It is the same
  file the old path named, now inside the family subpackage.

## Guard against the next move

`go/internal/exposure/sink_catalog_provenance_test.go` adds
`TestSinkCatalogProvenancePathsExist`. It resolves every `.go` path in every
spec's `Provenance` against `go/internal/` and fails on any that does not
exist. It also requires every graph-backed spec to cite at least one Go file
and fails if it checked zero paths, so it cannot pass vacuously.
`TestProvenanceGoPathsExtractsEveryCitedFile` pins the path extractor.

## Verification

All Go commands ran from `go/` with an isolated `GOCACHE` and `GOTMPDIR`.

RED, before the repoint: `go test ./internal/exposure -run
'TestSinkCatalogProvenancePathsExist|TestProvenanceGoPathsExtractsEveryCitedFile'
-count=1 -v` exited 1. It failed on exactly the three dead paths known then and on
nothing else. The extractor test passed.

The fourth path went stale later, while this branch was in flight: the #6061
shell_exec family moved to `reducer/code/shell/`. The guard this change adds
is what caught it — `make pre-pr` failed with
`sink "shell_exec" (relationship "EXECUTES_SHELL") Provenance cites
"reducer/shell_exec_materialization.go", which does not resolve`. After
repointing to `reducer/code/shell/handler.go` (3 `EXECUTES_SHELL` matches
there, 0 in `intents.go`) and re-pinning the golden,
`go test ./internal/exposure/... -count=1` exited 0.

GREEN, after the repoint and re-pin:

- `go test ./internal/exposure/... -count=1`: exit 0.
- The two new tests plus `TestSinkCatalogVersionIsStableAndChangeSensitive`,
  run with `-v`: exit 0, all three `PASS`.
- `go test ./internal/reducer/code/value ./internal/query/impact -count=1`:
  exit 0 (`ok .../reducer/code/value 0.266s`, `ok .../query/impact 0.673s`).
  These are the two `MatchSink` / `GraphBackedSinkSpecs` callers at this head:
  `query/impact/exposure_path_mapping.go:87` and
  `reducer/code/value/cloud_sink_loader.go:129`. An earlier revision of this
  note recorded the same run against `reducer/valueflow`, which does not exist
  in the tree — the value-flow family moved to `reducer/code/value`. The
  command as written could not have passed; it has been re-run as above.
- `go vet ./internal/exposure/...`: exit 0.
- `gofmt -l internal/exposure`: exit 0, no files listed.

Mutation check on the new guard: I copied `sink_catalog.go` aside and pointed
the `SinkInternetEndpoint` Provenance at
`reducer/secgroup/does_not_exist_6547.go` (`rg -c` confirmed one hit). Then
`go test ./internal/exposure -run TestSinkCatalogProvenancePathsExist
-count=1` exited 1, naming that spec and the missing file. After restoring the
copy, `cmp` against the backup exited 0, the bogus string left zero hits, and
`go test ./internal/exposure -count=1` exited 0.

No-Regression Evidence: this change edits four `Provenance` string literals and
the pinned `sinkCatalogVersionGolden` constant. Recognition fields are
unchanged, so `MatchSink` and `GraphBackedSinkSpecs` return the same matches.
The exposure suite and both caller packages (`reducer/code/value`,
`query/impact`) pass unchanged. No query, graph write, SQL statement, queue
path, or hot loop is touched, so there is no performance surface to measure.

No-Observability-Change: the change adds or alters no metric, span, log field,
status field, or runtime knob. `SinkCatalogVersion` and `Provenance` are not
emitted by any telemetry, API, or MCP surface.
