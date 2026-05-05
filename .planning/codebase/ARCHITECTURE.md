# Architecture

**Analysis Date:** 2026-04-13

## Pattern Overview

**Overall:** Three-tier facts-first pipeline with service-specific roles and durable work queues.

**Key Characteristics:**
- **Facts-First Write Path**: Source observations are persisted as typed facts before graph projection, enabling replay, audit, and multi-collector support
- **Service-Role Separation**: CLI (local indexing), API (serving), Ingester (collection/parsing), and Resolution Engine (projection) operate independently with clear ownership boundaries
- **Durable Cross-Service Coordination**: Work items, facts, and decisions flow through Postgres-backed queues and state, not through in-memory channels
- **Incremental Ingestion**: Repositories emit facts once per snapshot; projectors claim and process one scope generation at a time with bounded backpressure

## Layers

**Collector Layer:**
- Purpose: Discover sources and emit typed facts before graph projection
- Location: `src/eshu/collectors/`, `src/eshu/runtime/ingester/`
- Contains: Git discovery, snapshot parsing, `.gitignore` handling, fact marshaling
- Depends on: `parsers/`, `facts/emission/`
- Used by: `facts/` work queue, `resolution/` projection

**Facts Layer:**
- Purpose: Store and queue source observations durably for replay and multi-collector support
- Location: `src/eshu/facts/`
- Contains: Typed fact models (`models/`), Postgres persistence (`storage/`), work queue (`work_queue/`), fact emission (`emission/`)
- Depends on: `core/database` for Postgres connections
- Used by: `collectors/` for emission, `resolution/` for retrieval and projection

**Projection Layer:**
- Purpose: Convert facts into canonical graph state and content store entries
- Location: `src/eshu/resolution/`
- Contains: Queue orchestration (`orchestration/`), repository/file/entity/relationship/workload/platform projection (`projection/`), persisted decisions (`decisions/`), workload materialization (`workloads/`)
- Depends on: `facts/` for fact retrieval, `graph/` for writes, `content/` for dual-writes
- Used by: `runtime/ingester/` (inline cutover path), standalone `resolution-engine` service role

**Graph Layer:**
- Purpose: Canonical Neo4j schema and persistence helpers
- Location: `src/eshu/graph/`
- Contains: Schema definition and creation (`schema/`), write helpers and batch orchestration (`persistence/`)
- Depends on: Neo4j driver, `relationships/` for relationship persistence
- Used by: `resolution/` for canonical writes

**Content Store:**
- Purpose: Postgres-backed file and entity content cache dual-written with graph updates
- Location: `src/eshu/content/`
- Contains: Entity and file operations (`postgres_*.py`), Postgres queries, workspace handling
- Depends on: Postgres connections from `core/database`
- Used by: `query/` for reads, `resolution/` for dual-writes during projection

**Query Layer:**
- Purpose: Shared read surface over canonical graph and content store for CLI, MCP, and HTTP
- Location: `src/eshu/query/`
- Contains: Code analysis (`code*.py`), repository context (`repositories/`), impact analysis (`impact/`), infrastructure queries (`infra.py`), entity resolution (`entity_resolution*.py`), investigation support
- Depends on: Neo4j driver for graph reads, `content/` for content queries
- Used by: `api/`, `mcp/`, `cli/` for querying

**API / Transport:**
- Purpose: HTTP endpoints and FastAPI application wiring
- Location: `src/eshu/api/`
- Contains: OpenAPI assembly (`app.py`, `app_openapi.py`), routers by concern (`routers/`), auth (`http_auth.py`), router dependencies
- Depends on: `query/` for business logic
- Used by: HTTP clients

**MCP Transport:**
- Purpose: Model Context Protocol server for AI-oriented queries
- Location: `src/eshu/mcp/`
- Contains: MCP server and transport, tool registry, tool handlers (`tools/`)
- Depends on: `query/` for business logic
- Used by: AI clients via stdio or HTTP

**CLI / Local Runtime:**
- Purpose: Local indexing, analysis, and command-line interface
- Location: `src/eshu/cli/`
- Contains: Typer entrypoint (`main.py`), command registration (`commands/`), helpers, setup flows (`setup/`), visualization (`visualization/`)
- Depends on: All layers (local mode bypasses API/MCP and calls query directly)
- Used by: End users via `python -m eshu`

**Observability:**
- Purpose: OpenTelemetry metrics, traces, and structured logging across all runtimes
- Location: `src/eshu/observability/`
- Contains: OTEL bootstrap, runtime instrumentation, structured logging configuration
- Depends on: None (orthogonal concern)
- Used by: All layers for telemetry emission

**Parsers:**
- Purpose: Language-specific and IaC parsing, capability specs, and SCIP helpers
- Location: `src/eshu/parsers/`
- Contains: Parser registry (`registry.py`), language parsers (`languages/`), capabilities (`capabilities/`), SCIP helpers (`scip/`)
- Depends on: Core parsing libraries (tree-sitter, language-specific)
- Used by: `collectors/git/` during snapshot parsing

**Relationships:**
- Purpose: Evidence-backed repository relationship discovery (call graphs, dependencies)
- Location: `src/eshu/relationships/`
- Contains: Execution relationships, evidence models, Postgres persistence
- Depends on: `graph/` for persistence, `content/` for content access
- Used by: `resolution/projection/` for relationship materialization

**App / Service Entry:**
- Purpose: Service-role metadata and entrypoint contract
- Location: `src/eshu/app/`
- Contains: Service role constants (`roles.py`), entrypoint specifications (`service_entrypoints.py`)
- Depends on: None (pure metadata)
- Used by: Deployment and runtime startup

**Core / Plumbing:**
- Purpose: Database connections, job utilities, bundle export
- Location: `src/eshu/core/`
- Contains: Database manager, connection pooling, job orchestration, bundle export
- Depends on: Neo4j, Postgres drivers
- Used by: All layers

## Data Flow

**Local Request Path (CLI/MCP):**

1. CLI or MCP client invokes a command/tool
2. Request routes through `query/` layers (code analysis, repositories, impact, etc.)
3. Query layer reads from Neo4j and Postgres content store
4. Results returned to client

**Deployed Facts-First Path:**

1. **Ingester** discovers repositories and parses snapshots
2. **Git Collector** (`collectors/git/`) normalizes source observations
3. **Parsers** extract language-specific entities and relationships
4. **Fact Emission** (`facts/emission/`) persists typed facts to Postgres fact store
5. **Work Queue** creates one queued item per snapshot
6. **Resolution Engine** (or inline during cutover) claims the work item
7. **Fact Projection** loads facts and materializes:
   - Repository, file, entity state in Neo4j
   - Relationships and workload inferences
   - Platform family and platform inferences
8. **Content Dual-Write** persists file and entity content to Postgres
9. **Shared Follow-Up** emits reducer-intent items for cross-repo concerns (currently Phase 2 cutover)
10. **API/MCP/CLI** read canonical graph and content for queries

**State Management:**

- **Repository facts**: Postgres fact store (source of truth before projection)
- **Canonical graph**: Neo4j (read by API/MCP/CLI after projection)
- **Content cache**: Postgres content store (dual-written during projection, read by queries)
- **Work queue**: Postgres queues (coordinates ingester and resolution engine)
- **Decisions**: Postgres decisions table (stores projection reasoning and confidence)
- **Replay/Recovery**: Postgres replay events and backfill requests (audit trail and recovery workflows)

## Key Abstractions

**ServiceEntrypointSpec:**
- Purpose: Declares one Phase 1 service role boundary (API, Ingester, Resolution Engine)
- Examples: `src/eshu/app/service_entrypoints.py`
- Pattern: Metadata-driven service selection with import paths and implementation status

**Typed Facts Model:**
- Purpose: Repository, file, and entity observations with typed schemas
- Examples: `src/eshu/facts/models/`
- Pattern: Dataclass or Pydantic models for fact structure; emission layer handles conversion from parsed snapshots

**Fact Work Item:**
- Purpose: Represents one queued projection task (snapshot from one repository)
- Examples: `src/eshu/facts/work_queue/`
- Pattern: Durable Postgres record with state machine (pending → claimed → completed/failed), retry tracking, dead-letter handling

**Projection Pipeline:**
- Purpose: Orchestrates repo-local graph updates from claimed facts
- Examples: `src/eshu/resolution/projection/`
- Pattern: Stage-based pipeline (load facts → project repository → project files → project entities → project relationships → project workloads/platforms) with per-stage telemetry

**Query Context:**
- Purpose: Encapsulates Neo4j and content store read contracts for API/MCP/CLI
- Examples: `src/eshu/query/context/`
- Pattern: Modular query layers (code analysis, repository context, impact, etc.) with shared Neo4j session

**Router Dependency Injection:**
- Purpose: FastAPI router setup with shared query context and authentication
- Examples: `src/eshu/api/dependencies.py`
- Pattern: Dependency injection for query session, auth headers, request state

## Entry Points

**CLI Entry:**
- Location: `src/eshu/__main__.py`
- Triggers: `python -m eshu` or `eshu` command
- Responsibilities: Load config, initialize services, dispatch to registered commands in `cli/commands/`

**HTTP API Entry:**
- Location: `src/eshu/cli/main.py` (`start_http_api`)
- Triggers: `eshu serve start` or service deployment with `SERVICE_ROLE=api`
- Responsibilities: Start FastAPI app, wire routers, expose `/health`, `/metrics`, OpenAPI endpoints

**Ingester Entry:**
- Location: `src/eshu/runtime/ingester/__init__.py` (`run_repo_sync_loop`)
- Triggers: Service deployment with `SERVICE_ROLE=git-collector` or `eshu internal repo-sync-loop`
- Responsibilities: Discover repositories, parse snapshots, emit facts to queue

**Resolution Engine Entry:**
- Location: `src/eshu/resolution/orchestration/runtime.py` (`start_resolution_engine`)
- Triggers: Service deployment with `SERVICE_ROLE=resolution-engine` or `eshu internal resolution-engine`
- Responsibilities: Claim fact work items, orchestrate projection, dual-write graph and content

**Bootstrap Index Entry:**
- Location: `src/eshu/runtime/ingester/__init__.py` (`run_bootstrap_index`)
- Triggers: One-shot initial indexing or `eshu internal bootstrap-index`
- Responsibilities: Full repository sync and inline projection for deterministic initial load

## Error Handling

**Strategy:** Work-item lease-based error tracking with durable state in Postgres

**Patterns:**

- **Fact Emission**: Emit facts atomically; on error, emit error fact and fail the snapshot record
- **Projection**: Claim work item, project stages in sequence; record per-stage failures with error class (retryable vs. terminal)
- **Dead-Letter**: Work items with terminal errors moved to dead-letter table; accessible via admin API and CLI for investigation and replay
- **Retry Policy**: Failed work items returned to queue after backoff; retry count and age tracked durably
- **Observability**: Structured logs record work item ID, stage, error class, and recovery action for incident response

## Cross-Cutting Concerns

**Logging:** OTEL structured logging with component, runtime-role, and request context; JSON or text format configurable via `ESHU_LOG_FORMAT`

**Validation:** Fact models validated at emission; query inputs validated by FastAPI/Pydantic; Neo4j constraints at schema level

**Authentication:** HTTP auth via `http_auth.py` (basic auth, bearer tokens); MCP auth via transport layer; CLI local mode bypasses auth

**Rate Limiting:** Not yet implemented; expected future concern for multi-tenant API deployments

**Tracing:** OTEL distributed tracing across ingester→facts→resolution→graph path; trace context propagated through work item and decision records for end-to-end visibility
