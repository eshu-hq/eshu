# Telemetry Logs

Logs are Eshu's highest-context runtime signal. Use them after metrics identify
the changed service or phase and traces show where time went.

The code contract lives in `go/internal/telemetry/logging.go`,
`go/internal/telemetry/contract.go`, and `go/internal/telemetry/registry.go`.
For the platform envelope, see [Logging Standard](../logging.md).

## Log Shape

Go runtimes create JSON `slog` loggers through `telemetry.NewLogger` or
`telemetry.NewLoggerWithWriter`.

Every line from those loggers carries:

| Field | Meaning |
| --- | --- |
| `timestamp` | UTC RFC3339 timestamp normalized from the built-in `slog` time key. |
| `severity_text` | Normalized `slog` level. |
| `message` | Human-readable log message. |
| `service_name` | Runtime service name from telemetry bootstrap. |
| `service_namespace` | Stable namespace, normally `eshu`. |
| `component` | Component name passed by the runtime. |
| `runtime_role` | Runtime role passed by the runtime. |

When the log call receives a context with an active OpenTelemetry span,
`TraceHandler` also adds `trace_id`, `span_id`, and `severity_number`.
Startup logs and logs emitted outside an active span can omit those fields.

## Event Names

`event_name` appears only when a call site attaches
`telemetry.EventAttr(...)` or writes the key directly during early fallback
startup. It is not a required field on every log line.

Current event-name call sites include runtime startup, shutdown, listener,
Postgres, Neo4j, data-plane schema bootstrap, documentation extraction and
drift completion, and repository or service query stage timing. Verify current
names with `EventAttr(...)` call sites before documenting a new event family.

Older examples such as `resolution.work_item.completed` and
`graph.batch.commit.started` are not universal Go event families in the current
code.

`graph.write_timeout.unbounded` is a startup WARN from the ingester, reducer,
projector, and bootstrap-index when the graph backend is Neo4j and
`ESHU_CANONICAL_WRITE_TIMEOUT` is unset or invalid. It carries `graph_backend`
and `env_var`: Neo4j graph writes then have no transaction timeout, so a hung
write can outlive the lease that admitted it.

`query.graph_read.warning` is emitted only for slow, deadline, or unavailable
graph-read outcomes. It carries `pipeline_phase="query"`, a bounded
`failure_class`, and `duration_seconds`; it deliberately omits Cypher text,
graph addresses, and raw driver errors.

`query.graph_read.error` is a record for a graph read the backend failed
outside the deadline and availability classes. It is ERROR level, except that a
statement the backend rejects as malformed (`Neo.ClientError.Statement.*`) is
WARN, so a client cannot raise an ERROR stream with a bad query. Authentication,
authorization, and missing-database errors (`Neo.ClientError.Security.*`,
`Neo.ClientError.Database.*`) are Eshu's own configuration faults and stay
ERROR. The reader cannot tell who wrote a rejected statement, so a rejected
Eshu-built statement is WARN too; the `outcome="error"` metric still counts it.
The record fires once per failed read. It carries the same bounded fields as the
warning plus `graph_read.error`: the first line of the driver text with every
numeric and string literal replaced by `<REDACTED>`. The lines after it, which
quote the statement, are dropped because the redactor reads text as Cypher and a
double-quoted literal inside the quoted line would survive; the redacted
statement is in `graph_read.statement_head`. The HTTP response for the same read
carries only the fixed text `graph query failed`, except on the two
caller-authored Cypher routes, which answer 400 with the redacted message for a
rejected statement (#7253).

`postgres.store.error` is a record for a Postgres driver failure on the API or
MCP server writer pool (#7253). It fires once per failure (a close that repeats an earlier error adds no second
record), at the `database/sql` driver seam, so it covers every store that takes the writer pool. It
carries `failure_class` (`unavailable`, `timeout`, `canceled`, `failed`),
`postgres_store.operation` (`connect`, `prepare`, `query`, `exec`, `rows`,
`begin`, `commit`, `rollback`, `ping`, `reset_session`, `close`),
`postgres_store.sqlstate` (empty unless the server answered), a bounded
`postgres_store.statement_head` (the store's SQL with `$N` placeholders, never a
value), and `postgres_store.error`, the driver's own text truncated to 1,024
bytes. That text is operator-only: it can name the connection target, a
relation, a constraint, or a bound value, and the HTTP response for the same
failure carries only a fixed per-class string. A caller-canceled request logs
nothing. A timeout logs at WARN, as does a data or integrity error a request can
trigger (SQLSTATE class 22 or 23); everything else is ERROR. A client
disconnect or a bad input therefore cannot raise an ERROR stream. During a
Postgres outage every business request logs one record, because its freshness
checkpoint runs on the writer pool before any business read; the volume is the
request rate and is not sampled.

See [Graph-read safety](graph-read-safety.md) for the shared deadline and
operator triage contract.

## Structured Keys

`telemetry.LogKeys()` exposes the frozen registry. Start with these groups:

| Key group | Use |
| --- | --- |
| `scope_id`, `scope_kind`, `source_system`, `generation_id`, `collector_kind` | Locate source scope and collection generation. |
| `domain`, `partition_key`, `failure_class`, `refresh_skipped`, `pipeline_phase` | Triage reducer, projection, shared-work, retry, and skip behavior. |
| `lease_ttl_seconds` | A runner's configured partition-lease TTL in seconds — always the emitter's local configured value, never another holder's remaining time. Reducer sweep/cleanup cycle logs: the TTL claimed at cycle start (no renewal: a slow cycle's `duration_seconds` can exceed it even on success, so compare the two fields before inferring ownership lasted through completion); on a cycle-failed log, the TTL that would have guarded the cycle (the claim itself may have errored before any lease was held), so pair it with the error before inferring anything about expiry (#7047); the sweep/cleanup lease-release-failure warnings carry the configured TTL after which the unreleased lease expires server-side. Repo-dependency logs: the local claim TTL on `repo dependency partition lease held by another owner`, and the TTL after which the unreleased lease expires server-side on the lease-release-failure warning (#6747). |
| `request_id` plus emitted `trace_id` and `span_id` | Correlate request logs with traces. |
| `acceptance.*` | Debug shared-acceptance decisions. |
| `resource.fingerprint`, `resource.identity_kind`, `resource.type` | Correlate cloud or infrastructure resources without exposing raw ARNs, Terraform addresses, or secret-shaped names. |
| `depth`, `prior_config_addresses`, `state_only_addresses`, `addresses_promoted_to_removed_from_config`, `multi_element.*`, `resource_type`, `attribute_key`, `path`, `error` | Debug Terraform-state drift and composite-capture behavior. |
| `semantic_extraction.status`, `semantic_extraction.source_class`, `semantic_extraction.provider_kind`, `semantic_extraction.provider_profile_class`, `semantic_extraction.budget_state`, `semantic_extraction.budget_reason` | Debug semantic extraction queue, provider, and budget lifecycle without logging prompts, provider responses, credentials, source IDs, or provider profile IDs. |
| `producer_grant.producer_id`, `producer_grant.version`, `producer_grant.decision`, `producer_grant.stage`, `producer_grant.reason`, `producer_grant.fact_kind` | Tell a producer-grant deny from an allow. Denies log at WARN; allows log at INFO only for `install` and `activation` (per-emission and readback allows are counted, never logged). Never carries credentials, grant scope, config, or fact payloads. See [Producer-Grant Decisions](producer-grant-decisions.md). |
| `group_call_id`, `attempt`, `statement_index`, `statement_count`, `template_id`, `row_count`, `run_duration_s`, `consume_duration_s`, `outcome`, `attempts`, `duration_s`, `post_callback_duration_s` | With `ESHU_NORNICDB_PROFILE_FILE_GROUPS=true`, separate each files-phase transaction callback attempt, each fixed File template, and the final managed-transaction outcome. `consume_duration_s` appears only when `Result.Consume` was called. `post_callback_duration_s` appears only after a callback completed successfully; it includes driver retry, network, and commit work, so it is not an isolated commit timer. Query text, parameters, paths, and raw errors are omitted. |
| `fencing_token` | On the "supply chain impact write superseded" WARN line: the database-issued token the rejected supply-chain impact pass drew before its evidence load. Diagnose a persistent superseded rate by comparing `SELECT last_value, is_called FROM supply_chain_impact_fencing_token_seq` with `SELECT MAX(fencing_token) FROM supply_chain_impact_write_admission`: the sequence lags the admitted watermark (see the core README repair) when `last_value` is below the maximum, or equals it while `is_called` is false (the next `nextval()` would reissue the admitted token); otherwise two workers are overtaking each other. It is an integer ordering value and carries no repository or package data. |

High-cardinality values such as file paths, repository paths, package names,
state locators, image digests, delivery IDs, and raw cloud resource identifiers
must not become metric labels. For cloud/runtime resource logs, use
`resource.fingerprint`, `resource.identity_kind`, and `resource.type` instead
of the raw identifier.

## Pipeline Phases

`pipeline_phase` is the stable filter for end-to-end debugging:

| Value | Covers |
| --- | --- |
| `discovery` | Repository selection and scope assignment. |
| `parsing` | File parse, snapshot, and content extraction. |
| `emission` | Fact envelope creation and durable commit. |
| `projection` | Fact-to-graph, content, or intent projection. |
| `reduction` | Reducer intent execution. |
| `shared` | Shared projection partition processing. |
| `query` | Read-path query operations. |
| `serve` | API or MCP request handling. |

Use `pipeline_phase` before searching by message text. Messages can change;
phase values are the durable operational contract.

## Triage Order

1. Start from `/admin/status` or queue metrics to identify the affected runtime.
2. Filter logs by `scope_id`, `generation_id`, `domain`, `partition_key`, or
   `request_id`.
3. Check `pipeline_phase` and `failure_class`.
4. Pivot to `trace_id` when the log line carries one.
5. Use exact errors and high-cardinality identifiers from logs to decide
   whether the failure is source data, storage, graph, retry, contention, or
   caller shape.

## Change Rules

When changing log behavior:

1. Add new frozen keys in `go/internal/telemetry/contract.go`, or in
   `logging.go` for scoped diagnostic keys when the grandfathered contract file
   cannot grow.
2. Register keys in `go/internal/telemetry/registry.go`.
3. Add helper functions in `go/internal/telemetry/logging.go` only when
   repeated call sites need them.
4. Update this page and [Cross-Service Correlation](cross-service-correlation.md)
   when the key affects async traceability.
5. Run `go test ./internal/telemetry -count=1`.

Do not add high-cardinality metric labels to avoid writing a log. Logs and
trace attributes are the right place for unbounded operational detail.
