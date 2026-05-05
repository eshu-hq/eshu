# Codebase Structure

**Analysis Date:** 2026-04-13

## Directory Layout

```
eshu/
├── src/eshu/       # Main Python package (importable)
│   ├── __main__.py                   # CLI entrypoint for `python -m`
│   ├── app/                          # Service-role metadata and entrypoints
│   ├── api/                          # FastAPI HTTP application and routers
│   ├── cli/                          # Typer CLI entrypoint and command registry
│   ├── collectors/                   # Source-specific collectors (Git, etc.)
│   ├── content/                      # Postgres content store (files, entities)
│   ├── core/                         # Database, connections, job utilities
│   ├── data_intelligence/            # Data analysis and DBT SQL support
│   ├── domain/                       # Shared typed response and entity models
│   ├── facts/                        # Typed facts, storage, work queue
│   ├── graph/                        # Neo4j canonical schema and persistence
│   ├── indexing/                     # Indexing coordinator and orchestration
│   ├── mcp/                          # Model Context Protocol server and tools
│   ├── observability/                # OpenTelemetry metrics, traces, logs
│   ├── parsers/                      # Language and IaC parsing registry
│   ├── platform/                     # Platform/workload identity helpers
│   ├── query/                        # Shared read-side graph queries
│   ├── relationships/                # Relationship discovery and evidence
│   ├── resolution/                   # Fact projection and materialization
│   ├── runtime/                      # Background runtime orchestration
│   ├── tools/                        # GraphBuilder facade and query surfaces
│   ├── utils/                        # Cross-cutting utility helpers
│   ├── viz/                          # Graph visualization utilities
│   ├── paths.py                      # Workspace path helpers
│   ├── postgres_schema.py            # Postgres schema definitions
│   ├── repository_identity.py        # Repository identity and normalization
│   └── README.md                     # Package overview
├── tests/                            # Test suite (unit, integration)
├── docs/                             # Documentation (mkdocs)
├── deploy/                           # Deployment configs (Helm, ArgoCD)
├── scripts/                          # Utility scripts
├── .github/                          # GitHub Actions workflows
└── README.md                         # Project README
```

## Directory Purposes

**`src/eshu/app/`:**
- Purpose: Service-role wiring and entrypoint contracts for deployment
- Contains: `SERVICE_ROLE_*` constants, `ServiceEntrypointSpec` class, runtime role mappings
- Key files: `roles.py`, `service_entrypoints.py`

**`src/eshu/api/`:**
- Purpose: HTTP API application assembly and routing
- Contains: FastAPI app setup, OpenAPI spec, auth middleware, routers for all endpoints
- Key files: `app.py` (main app), `app_openapi.py`, `dependencies.py`, `http_auth.py`, `routers/` subdirectory

**`src/eshu/cli/`:**
- Purpose: Command-line interface and local runtime entry
- Contains: Typer entrypoint, command registration, setup flows, visualization
- Key files: `main.py` (Typer app entrypoint), `commands/` (command modules), `config_manager.py`, `setup/`

**`src/eshu/collectors/`:**
- Purpose: Source-specific discovery and collection logic
- Contains: Git collector implementation
- Key files: `git/` subdirectory with source discovery and snapshot parsing

**`src/eshu/content/`:**
- Purpose: Postgres-backed file and entity content cache
- Contains: Content persistence, query helpers, workspace management
- Key files: `service.py` (main content service), `postgres*.py` (persistence), `workspace.py`

**`src/eshu/core/`:**
- Purpose: Low-level database, connection, and job utilities
- Contains: Database manager, connection pooling, bundle export
- Key files: `database*.py` (Neo4j/Postgres), `jobs/` (job orchestration)

**`src/eshu/domain/`:**
- Purpose: Shared typed models for responses and entities
- Contains: Pydantic/dataclass response models used across API, MCP, CLI
- Key files: Various model files organized by domain concern

**`src/eshu/facts/`:**
- Purpose: Typed facts storage, work queue, and fact emission
- Contains: Fact models, Postgres persistence, work queue state machine, emission logic
- Key files: `models/` (fact schemas), `storage/` (persistence), `work_queue/` (queue), `emission/` (git snapshot emission)

**`src/eshu/graph/`:**
- Purpose: Neo4j canonical graph schema and write helpers
- Contains: Schema creation, graph constraints, batch write orchestration
- Key files: `schema/` (schema definitions), `persistence/` (write helpers)

**`src/eshu/indexing/`:**
- Purpose: Indexing coordinator and end-to-end orchestration
- Contains: Coordinator pipeline, storage helpers, repository queue management
- Key files: `coordinator.py`, `coordinator_pipeline.py`, `coordinator_storage.py`

**`src/eshu/mcp/`:**
- Purpose: Model Context Protocol server and tool registry
- Contains: MCP transport, tool handlers, tool definitions
- Key files: `__init__.py` (MCPServer), `tools/` (tool registry and handlers)

**`src/eshu/observability/`:**
- Purpose: OpenTelemetry instrumentation and structured logging
- Contains: OTEL runtime bootstrap, metrics, traces, structured logging
- Key files: `runtime.py`, `structured_logging.py`, `otel.py`, `state.py`

**`src/eshu/parsers/`:**
- Purpose: Language-specific and IaC parsing registry
- Contains: Parser registry, language parsers (Go, TypeScript, Python, Rust, etc.), capabilities, SCIP helpers
- Key files: `registry.py` (parser registry), `languages/` (per-language parsers), `capabilities/` (capability specs)

**`src/eshu/platform/`:**
- Purpose: Platform and workload identity helpers
- Contains: Platform family inference, package resolver
- Key files: `package_resolver.py`

**`src/eshu/query/`:**
- Purpose: Shared read-side queries over canonical graph and content
- Contains: Code analysis queries, repository context, impact analysis, entity resolution, investigation support
- Key files: `code*.py` (code analysis), `repositories/` (repo context), `impact/` (impact analysis), `entity_resolution*.py`, `context/` (shared query context)

**`src/eshu/relationships/`:**
- Purpose: Evidence-backed relationship discovery
- Contains: Call graphs, dependency relationships, evidence models
- Key files: `execution.py`, `postgres.py` (persistence), `file_evidence_support.py`

**`src/eshu/resolution/`:**
- Purpose: Fact projection and canonical graph materialization
- Contains: Queue orchestration, projection pipeline, decision persistence, workload/platform materialization
- Key files: `orchestration/` (queue loop), `projection/` (fact→graph stages), `decisions/` (decision persistence), `workloads/` (workload inference), `platforms.py`

**`src/eshu/runtime/`:**
- Purpose: Background runtime services (ingester, resolution engine)
- Contains: Repository sync loop, fact emission, status tracking
- Key files: `ingester/` (repo sync and parse), `status_store*.py` (runtime status tracking)

**`src/eshu/tools/`:**
- Purpose: GraphBuilder facade and query tool surfaces
- Contains: Shared tool implementations and query wrappers for tools
- Key files: `scip_pb2.py` (SCIP protocol buffer), query tool implementations

**`src/eshu/utils/`:**
- Purpose: Cross-cutting utility helpers
- Contains: String utilities, file helpers, common patterns
- Key files: Various utility modules organized by concern

**`src/eshu/viz/`:**
- Purpose: Graph visualization support
- Contains: Visualization generation, format converters
- Key files: Various visualization modules

## Key File Locations

**Entry Points:**
- `src/eshu/__main__.py`: Entrypoint for `python -m eshu`
- `src/eshu/cli/main.py`: Typer app definition; dispatches to commands and services
- `src/eshu/api/app.py`: FastAPI application factory; defines routers
- `src/eshu/resolution/orchestration/runtime.py`: Resolution Engine service entry

**Configuration:**
- `src/eshu/cli/config_manager.py`: Runtime config loading and management
- `src/eshu/paths.py`: Workspace path resolution
- `src/eshu/postgres_schema.py`: Postgres table and index definitions
- `src/eshu/app/roles.py`: Service role constants

**Core Logic:**
- `src/eshu/collectors/git/`: Git discovery and parsing
- `src/eshu/facts/emission/git_snapshot.py`: Fact emission from parsed snapshot
- `src/eshu/resolution/projection/`: Repository/file/entity/relationship/workload projection
- `src/eshu/query/code*.py`: Code analysis queries
- `src/eshu/graph/persistence/`: Neo4j write orchestration

**Testing:**
- `tests/integration/deployment/`: Deployment config tests
- `tests/integration/cli/`: CLI command tests
- `tests/integration/indexing/`: End-to-end indexing tests
- `tests/unit/observability/`: Observability and telemetry tests

## Naming Conventions

**Files:**
- `*_support.py`: Helper functions for a specific domain (e.g., `hcl_terraform_support.py`)
- `*_operations.py` or `*_ops.py`: Operational or state-mutating functions (e.g., `git_sync_ops.py`)
- `*_models.py`: Typed models and schemas
- `*_database.py`: Postgres/Neo4j query and persistence (e.g., `entity_resolution_database.py`)
- `*_query.py` or just logic in module: Query-side read operations
- `postgres_*.py`: Postgres-specific implementations within content store

**Directories:**
- Lowercase with underscores (e.g., `data_intelligence`, `eshu`)
- Plural for collections of related items (e.g., `collectors/`, `parsers/languages/`, `api/routers/`)
- Singular for singular concepts (e.g., `content/`, `graph/`, `domain/`)
- Prefixed by concern for major domains (e.g., `query/context/`, `resolution/decisions/`)

**Python Functions & Classes:**
- `snake_case` for functions and module-level helpers
- `PascalCase` for classes
- `UPPER_SNAKE_CASE` for constants (especially in `app/roles.py`)
- Leading underscore for internal/private helpers (e.g., `_initialize_services`)

## Where to Add New Code

**New Feature (Graph Query):**
- Primary code: `src/eshu/query/` (add new module or extend existing query module)
- API exposure: `src/eshu/api/routers/` (add router or extend existing)
- MCP exposure: `src/eshu/mcp/tools/handlers/` (add tool handler)
- Tests: `tests/integration/query/` or `tests/unit/query/`

**New Collector (e.g., AWS, Kubernetes):**
- Implementation: `src/eshu/collectors/{source}/` (mirror Git collector structure)
- Fact emission: `src/eshu/facts/emission/{source}_snapshot.py`
- Entry point: Update `src/eshu/app/service_entrypoints.py` with new service role
- Tests: `tests/integration/collectors/{source}/`

**New Parser Language:**
- Implementation: `src/eshu/parsers/languages/{language}/` (follow existing language structure)
- Registry: Update `src/eshu/parsers/registry.py` to register new parser
- Capabilities: `src/eshu/parsers/capabilities/{language}/`
- Tests: `tests/unit/parsers/languages/{language}/`

**CLI Command:**
- Implementation: `src/eshu/cli/commands/{command_name}.py`
- Registration: Add to command module registry in `src/eshu/cli/commands/__init__.py`
- Helpers: `src/eshu/cli/helpers/` for shared command logic
- Tests: `tests/integration/cli/test_cli_commands.py`

**New Resolution Projection Stage:**
- Implementation: `src/eshu/resolution/projection/{stage_name}.py`
- Orchestration: Register in `src/eshu/resolution/orchestration/` pipeline
- Decision tracking: Store decisions in `src/eshu/resolution/decisions/`
- Tests: `tests/integration/indexing/test_git_facts_projection*.py`

**Utilities & Helpers:**
- Shared helpers: `src/eshu/utils/` (organize by concern)
- Cross-package: Place in most specific parent package before utils (e.g., `graph/`, `content/`)

## Special Directories

**`.planning/codebase/`:**
- Purpose: Generated architecture and structure documentation
- Generated: Yes (by `/gsd:map-codebase`)
- Committed: Yes (reference for future development)

**`tests/fixtures/sample_projects/`:**
- Purpose: Multi-language test repositories for integration testing
- Contains: Go, TypeScript, Python, Rust, Java, C#, PHP, C++, Swift, and misc language samples
- Committed: Yes (source data for reproducible tests)

**`deploy/helm/`:**
- Purpose: Kubernetes Helm charts for deployment
- Contains: Chart templates for API, Ingester, Resolution Engine, Bootstrap Index
- Committed: Yes (deployment as code)

**`docs/docs/`:**
- Purpose: User and operator documentation (mkdocs)
- Contains: Architecture guides, deployment guides, local testing, telemetry docs
- Committed: Yes (reference documentation)

**`.github/workflows/`:**
- Purpose: GitHub Actions CI/CD pipelines
- Committed: Yes (CI configuration)

**`proto/`:**
- Purpose: Protocol Buffer definitions (if used for SCIP or gRPC)
- Committed: Yes (schema definitions)

**`schema/`:**
- Purpose: Database schema migrations or snapshots
- Committed: Yes (version-controlled schema)

## Typical Development Workflow

1. **Understand the concern**: Is it a query, a new source, a parsing feature, or observability?
2. **Choose the package**: Map to appropriate package from the directory layout above
3. **Follow the pattern**: Look at existing code in that package for structure and naming
4. **Add tests first**: Place test file in `tests/` mirroring source structure
5. **Write implementation**: Follow naming conventions and import organization
6. **Update entry points if needed**: Modify `app/service_entrypoints.py`, CLI command registry, or MCP tool registry
7. **Add observability**: Use `observability/` patterns for tracing, metrics, logging
8. **Verify cross-package contracts**: Respect layer boundaries (collectors don't write graph, resolution doesn't emit facts)

## Import Organization

**Standard order within files:**
1. `__future__` annotations import
2. Standard library (`os`, `sys`, `json`, `dataclasses`, `typing`, etc.)
3. Third-party (`neo4j`, `psycopg2`, `pydantic`, `typer`, `fastapi`, etc.)
4. Local package imports from parent/sibling modules
5. Local package imports from current package

**Path aliases:**
- Use relative imports within same package: `from .sibling import something`
- Use absolute imports across packages: `from eshu.query import code_finder`
- Avoid deep relative imports (more than 2 levels up)

**Example:**
```python
from __future__ import annotations

import json
import logging
from dataclasses import dataclass
from typing import Callable

import psycopg2
from neo4j import Driver
from pydantic import BaseModel

from eshu.core.database import DatabaseManager
from eshu.observability import trace_query

from .models import RepositoryFact
from .storage import FactStore
```
