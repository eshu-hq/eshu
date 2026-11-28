# Documentation Layout

This repository keeps durable documentation in three clear homes:

| Home | Purpose |
| --- | --- |
| `docs/public/` | Public MkDocs site. Every `.md` file here must appear in `docs/mkdocs.yml` (`scripts/verify-docs-catalog.sh` blocks orphaned pages). |
| `docs/internal/` | Maintainer handbooks, code-level correlation tables, and the living ADR / active-issue design core. Never commit PR proof logs, shims, probes, or arbiter rulings (`scripts/verify-no-ephemeral-docs.sh`). |
| `go/<pkg>/{doc.go,README.md,AGENTS.md}` | Package-local contracts, architecture, telemetry, and contributor rules. |

## Directory Schema

```text
docs/
├── README.md                          # This schema & documentation rules
├── mkdocs.yml                         # Public MkDocs navigation and strict build config
├── openapi/                           # OpenAPI specs (runtime admin and HTTP contracts)
├── dashboards/                        # Observability dashboard JSON
├── public/                            # Human-first public documentation site
│   ├── index.md & start-here.md       # Docs home and goal-based router
│   ├── getting-started/               # Demo mode, first successful run, Console quickstart
│   ├── run-locally/                   # Local binaries, Compose overview, local MCP
│   ├── tutorials/                     # Step-by-step end-to-end learning walkthroughs
│   ├── use/ & guides/                 # Task-focused how-to guides
│   ├── mcp/                           # Connect MCP (Claude, Codex, Cursor, VS Code)
│   ├── architecture.md                # System architecture & backend seam
│   ├── understand/                    # System concepts overview
│   ├── concepts/
│   │   └── flows/                     # Single unified Diagrams & Flows Atlas (all Mermaid flows)
│   ├── deployment/                    # Dedicated Deployment hub (topologies, runtimes, onboarding)
│   ├── operate/                       # Support, symptom-first troubleshooting, health, runbooks
│   ├── reference/
│   │   ├── http-api.md & http-api/    # HTTP API map and focused route family pages
│   │   ├── mcp-reference.md           # MCP tool reference
│   │   ├── cli-reference.md           # CLI commands and flags
│   │   ├── env-registry/              # Generated environment variable registry
│   │   └── telemetry/                 # Metrics, traces, logs, and safety contracts
│   ├── languages/                     # Supported languages, feature matrix, admitted files
│   ├── services/                      # Service-specific deep dives (resolution engine, collectors)
│   ├── decisions/                     # Public architecture decision index
│   └── extend/                        # Extension authoring and SDK compatibility
└── internal/                          # Maintainer-only documentation
    ├── README.md                      # Maintainer doc index and living ADR catalog
    ├── agent-guide.md                 # Engineering contracts for agents and contributors
    ├── writing-for-agents.md          # Unified plain-English communication standard
    ├── correlation-atlas/             # Code-level correlation domain & re-open symbol tables
    └── design/                        # Active OPEN-issue designs, lockstep specs, living ADRs
```

Supporting directories:

- **Deployment manifests & Helm charts** live in the companion
  [eshu-deploy](https://github.com/eshu-hq/eshu-deploy) repository.
- **Diagrams** follow the **one picture, one home** rule: user- and
  operator-facing diagrams live inline in
  [Eshu Flow Atlas](public/concepts/flows/index.md); package-internal function
  diagrams stay in their owning package `README.md`.

## Required Documentation Skills

Good hygiene alone does not make documentation readable. Before creating or
editing pages in this tree, load the matching skills:

- **Public pages (`docs/public/`):** load `publish` for the plain-English,
  anti-slop writing standard, and follow
  [Documentation Style Guide](public/documentation-style-guide.md) (choose the
  Diátaxis page type and wire `docs/mkdocs.yml`).
- **Maintainer pages (`docs/internal/`):** load `publish` and follow
  [Writing For Agents](internal/writing-for-agents.md). Keep only durable
  handbooks and active designs — never commit PR proof notes or arbiter rulings.
- **Package docs (`go/**/{doc.go,README.md,AGENTS.md}`):** load
  `folder-doc-keeper` and `publish`.

Historical plans and proof notes are not stable documentation. Extract durable
decisions, workflows, diagrams, and performance learnings into `docs/public/`,
`docs/internal/`, or package-local docs, then delete stale working notes.

Build the public docs with:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```
