# Technology Stack

**Analysis Date:** 2026-04-13

## Languages

**Primary:**
- Python 3.10+ - Core platform, API, CLI, MCP server, parsers, and runtime orchestration
- Go 1.26.0 - Data plane services (collector, projector, bootstrap)

**Secondary:**
- TypeScript/JavaScript - Web visualization frontend and client integrations
- Protocol Buffers (protobuf) - SCIP interchange format for semantic indexing

## Runtime

**Environment:**
- Python: CPython 3.10+ (Docker image: `python:3.12-slim`)
- Go: Go 1.26.0

**Package Managers:**
- Python: `uv` (universal Python package manager)
  - Lockfile: `uv.lock` (present, version 3)
  - Project config: `pyproject.toml`
- Go: Go modules via `go.mod`

## Frameworks

**Core:**
- FastAPI 0.100.0+ - HTTP API and MCP over HTTP transport
- Uvicorn 0.22.0+ - ASGI application server for FastAPI
- Typer 0.9.0+ - CLI command framework

**Graph Databases (pluggable):**
- Neo4j 5.15.0+ - Primary production graph store (supports 2026-community image)
- FalkorDB 0.1.0+ - Alternative in-process graph backend
- KuzuDB - Supported via pluggable backend interface (reference: `core/database_kuzu.py`)

**Relational/Content Storage:**
- PostgreSQL 18-alpine - Content store, facts queue, runtime metadata
- psycopg 3.2.0+ - PostgreSQL async driver with connection pooling
- psycopg_pool 3.2.0+ - Connection pool for concurrent access

**Observability & Tracing:**
- OpenTelemetry SDK 1.34.1+ - Metrics and trace collection
- OpenTelemetry Exporter OTLP (gRPC) 1.34.1+ - Protocol Buffers trace export
- OpenTelemetry Exporter Prometheus 0.55b1 - Prometheus metrics endpoint
- OpenTelemetry FastAPI Instrumentation 0.55b1 - Auto-instrumentation of HTTP routes
- Jaeger 1.76.0 - Trace backend (docker-compose UI on port 16686)
- OTEL Collector 0.143.0 - OpenTelemetry collector for trace aggregation

**Parsing & AST Analysis:**
- tree-sitter 0.21.0+ - Language parser library
- tree-sitter-language-pack 0.13.0 - Pre-built language grammars
- tree-sitter-c-sharp 0.23.1+ - C# grammar

**Testing:**
- pytest 7.4.0+ - Test runner
- pytest-asyncio 0.21.0+ - Async test support
- pytest-xdist 3.6.1+ - Parallel test execution

**Build & Dev:**
- black 23.11.0+ - Python code formatter
- setuptools & wheel - Package building

**Authentication & Security:**
- PyJWT[crypto] 2.10.1+ - JWT token handling (GitHub App tokens)

**Utilities:**
- requests 2.32.0+ - HTTP client (GitHub API, Git operations)
- httpx 0.27.0+ - Async HTTP client
- python-multipart 0.0.20+ - Multipart form parsing for file uploads
- PyYAML - YAML parsing (configs, manifests)
- watchdog 3.0.0+ - File system event monitoring
- rich 13.7.0+ - Terminal UI formatting
- inquirerpy 0.3.4+ - Interactive CLI prompts
- python-dotenv 1.0.0+ - `.env` file loading
- pathspec 0.12.1+ - gitignore-style path matching
- nbformat 5.x - Jupyter notebook format (Jupyter analysis support)
- nbconvert 7.16.6+ - Notebook conversion
- stdlibs 2023.11.18 - Standard library detection

**Go Data Plane Dependencies:**
- github.com/hashicorp/hcl/v2 - HCL (Terraform) parsing
- github.com/jackc/pgx/v5 - PostgreSQL driver
- github.com/neo4j/neo4j-go-driver/v5 - Neo4j driver
- github.com/tree-sitter/go-tree-sitter - Go bindings for tree-sitter
- github.com/tree-sitter/tree-sitter-* (go, javascript, python, typescript) - Language grammars
- gopkg.in/yaml.v3 - YAML parsing
- golang.org/x/crypto - Cryptography utilities

## Configuration

**Environment:**
- Primary: `.env` files (`.env.example` provided)
- CLI config store: `~/.eshu/.env`
- Docker Compose: `docker-compose.yaml`, `docker-compose.template.yml`, `docker-compose.nornicdb.yml`
- App config manager: `src/eshu/cli/config_manager.py`

**Key Environment Variables:**
- `DEFAULT_DATABASE` - Graph backend selection (neo4j, falkordb, kuzudb)
- `NEO4J_URI`, `NEO4J_USERNAME`, `NEO4J_PASSWORD` - Neo4j connection
- `ESHU_CONTENT_STORE_DSN`, `ESHU_POSTGRES_DSN` - PostgreSQL connection strings
- `ESHU_API_KEY` - HTTP bearer token (auto-generated if `ESHU_AUTO_GENERATE_API_KEY=true`)
- `ESHU_LOG_FORMAT` - Log output (json for production, text for dev)
- `OTEL_EXPORTER_OTLP_ENDPOINT` - OpenTelemetry collector endpoint
- `ESHU_PARSE_WORKERS` - Concurrent parser count (default: 4)
- `ESHU_REPO_FILE_PARSE_MULTIPROCESS` - Process-pool parsing (default: false)

**Build:**
- Dockerfile: Multi-stage build (builder + runtime stages)
- Build requires: gcc, g++, make, git (build stage only)
- Runtime requires: git, curl, gh (GitHub CLI)

## Platform Requirements

**Development:**
- Python 3.10+ with uv
- Go 1.26.0 (for data plane development)
- Docker & Docker Compose
- Git

**Production:**
- Docker container runtime or Kubernetes
- Neo4j database (5.15.0+) or alternative graph backend
- PostgreSQL database (13+)
- OpenTelemetry collector (optional but recommended for observability)

**Kubernetes Deployment:**
- API service: `Deployment` (stateless HTTP/MCP)
- Ingester service: `StatefulSet` with `PersistentVolumeClaim` (for repo checkout state)
- Resolution Engine: `Deployment` (stateless fact projection)
- Bootstrap Index: One-shot `Job` for initial sync

## Package Organization

**Python Main Package:** `eshu`
- Located: `src/eshu/`
- Entry point: `eshu.cli.main:app` (Typer CLI)
- Command: `eshu` (installed via `pyproject.toml` scripts)

**Go Module:** `github.com/eshu-hq/eshu/go`
- Located: `go/`
- Binaries:
  - `bootstrap-data-plane` - Initial data sync
  - `projector` - Fact resolution and graph projection
  - `collector-git` - Git repository collection
  - `admin-status` - Admin operations

---

*Stack analysis: 2026-04-13*
