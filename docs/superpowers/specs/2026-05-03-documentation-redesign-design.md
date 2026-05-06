# Documentation Redesign Design

## Goal

Make Eshu documentation easier to enter, easier to follow, and
easier to trust. The docs should lead humans through the work they are trying to
do before sending them into reference material.

## Primary readers

The docs should optimize for these readers, in this order:

1. Local developer or MCP user.
   They want Eshu running locally so their assistant and CLI can answer useful
   questions.
2. DevOps or platform engineer deploying to Kubernetes.
   They need storage requirements, Helm or manifests, runtime order,
   configuration, health checks, and operating guidance.
3. Contributor or maintainer.
   They need architecture, contracts, internals, ADRs, test gates, and extension
   points.

This order matters. A first-time reader should not have to understand reducer
ownership, graph schema dialects, or telemetry contracts before they can decide
which setup path fits them.

## Current problems

The current docs are accurate enough after the storage and quickstart cleanup,
but the reader journey is muddled.

- There is no single "start here" decision point.
- Local binaries, Docker Compose, MCP, and Kubernetes deployment are mixed
  across several pages.
- `Reference` is too large and contains concepts, runbooks, service docs,
  language support, local-host internals, and operator material.
- Some pages are doing too much. `reference/local-testing.md` is over 1,000
  lines, and `reference/cli-reference.md` is nearly 500 lines.
- The tone is often contract-heavy in places where a human needs guidance.
  Reference pages can stay terse and exact, but entry pages should read like an
  experienced engineer walking another engineer through the system.

## Documentation model

Use a task-first structure. The top-level navigation should reflect what the
reader came to do:

```text
Home
Start Here
Run Locally
Use Eshu
Connect MCP
Deploy to Kubernetes
Operate Eshu
Understand Eshu
Extend Eshu
Reference
Project
```

The home page should be short. It should explain what Eshu is, then send the
reader to one of three clear paths:

- Run Eshu locally.
- Deploy Eshu to Kubernetes.
- Connect an assistant through MCP.

The docs should avoid making "architecture" the first path. Architecture is
important, but it belongs after the user knows what they are trying to run.

## Local documentation lanes

Local setup needs two visibly separate lanes.

### Local binaries

Use this lane for Eshu development, single-workspace local ownership, and the
`eshu graph start` workflow.

This page should include:

- the required Go build commands for `eshu`, `eshu-mcp-server`,
  `eshu-bootstrap-index`, `eshu-ingester`, and `eshu-reducer`
- `eshu install nornicdb`
- `eshu graph start --workspace-root <repo>`
- what runs in this mode: embedded Postgres, managed NornicDB, ingester, and
  reducer
- how local MCP attaches to the workspace owner
- which commands still need an API process

### Docker Compose

Use this lane for the full laptop service stack.

This page should show the two official Compose choices up front:

```bash
docker compose up --build
docker compose -f docker-compose.neo4j.yml up --build
```

It should explain that the default stack runs NornicDB and Postgres, while the
Neo4j stack is the explicit compatibility path. It should also list the
services Compose starts: API, MCP server, ingester, reducer, bootstrap indexer,
Postgres, graph backend, OpenTelemetry collector, and Jaeger.

## Kubernetes deployment lane

DevOps and platform engineers need a paved road, not a scavenger hunt.

The Kubernetes section should have this shape:

```text
Deploy to Kubernetes
- Overview
- Prerequisites
- Storage: NornicDB, Neo4j, and Postgres
- Helm quickstart
- Helm values
- Minimal manifests
- Argo CD / GitOps
- Production checklist
- Upgrade and rollback
```

The Kubernetes docs should answer, in order:

1. What gets deployed?
2. What storage do I need?
3. Which graph backend is default?
4. How do I configure Postgres?
5. How do I install with Helm?
6. How do I check that it is healthy?
7. How do I operate, upgrade, or roll back?

The deployment pages should link to architecture only when the reader needs
deeper reasoning about runtime boundaries.

## Use and MCP lanes

The user-facing docs should separate "operate the platform" from "use the
platform."

`Use Eshu` should cover:

- indexing repositories
- asking code questions
- tracing infrastructure
- understanding result truth and freshness
- starter prompt patterns

`Connect MCP` should cover:

- client setup
- local MCP versus Compose MCP service
- common MCP workflows
- MCP reference links

MCP deserves its own top-level path because many users will meet Eshu through an
assistant, not through the HTTP API.

## Operate, understand, and extend

`Operate Eshu` should hold runtime and support material:

- runtime model
- health checks
- telemetry
- troubleshooting
- local and cloud validation
- upgrade and rollback

`Understand Eshu` should hold conceptual material:

- how Eshu works
- architecture
- graph model
- modes and truth levels
- service workflows

`Extend Eshu` should hold contributor-facing extension paths:

- collector authoring
- fact contracts
- language support
- plugin trust and schema versioning
- source layout

`Reference` should be narrower than it is today. It should hold precise lookup
material: CLI commands, HTTP API, MCP reference, environment variables, schemas,
capability contracts, and file formats.

## Voice and writing rules

Apply the local `humanizer` skill to all entry pages, guide pages, and overview
pages. The goal is not casual marketing copy. The goal is clear, direct writing
that sounds like an engineer helping another engineer.

Guide pages should:

- start with the reader's goal
- say when to use the path and when not to
- show the next command quickly
- explain only the context needed for the current step
- link to deeper reference instead of embedding every detail
- use active voice
- avoid inflated claims and generic conclusions

Reference pages should stay exact. They can be denser, but they should still
avoid filler and chatbot-like phrasing.

## Migration strategy

Refactor in small chunks:

1. Add the new navigation skeleton and landing pages.
2. Split local setup into local binaries and Docker Compose pages.
3. Build the Kubernetes deployment lane.
4. Move usage and MCP guides into task-first paths.
5. Split oversized reference pages.
6. Move architecture and extension material into clearer buckets.
7. Run strict docs build and link/stale-term scans after each chunk.

Avoid rewriting everything at once. Preserve accurate content first, then
humanize the pages that humans read first.

## Acceptance criteria

- A new reader can pick a path from the home page in under one minute.
- Local binaries and Docker Compose are separate setup lanes.
- Kubernetes deployment is a first-class path for DevOps engineers.
- NornicDB, Neo4j, and Postgres storage responsibilities are stated in the
  relevant local and Kubernetes entry pages.
- The top-level navigation no longer hides beginner material inside a huge
  reference section.
- Oversized pages have clear split targets.
- `mkdocs build --strict` passes.
- `git diff --check` passes.
- A stale-doc scan finds no references to removed Compose files or unsupported
  graph backends except intentional negative tests or historical ADR context.
