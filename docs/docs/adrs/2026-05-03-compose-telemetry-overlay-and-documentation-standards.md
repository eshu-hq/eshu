# ADR: Compose Telemetry Overlay And Documentation Standards

**Date:** 2026-05-03
**Status:** Accepted
**Authors:** Platform Engineering
**Deciders:** Platform Engineering

## Status Review (2026-05-03)

**Current disposition:** Accepted.

This branch implements the decision: default Compose stays focused on the
runtime stack, the telemetry overlay owns Jaeger and the OTEL collector, docs
routes are public, README/docs/OpenAPI surfaces are aligned, and docs standards
are enforced by tests and CI.

**Remaining work:** none for the overlay split. Keep running the ADR's stated
verification gates before merge.

## Context

Eshu now has two local full-stack Compose lanes:

- `docker-compose.yaml` for the default NornicDB graph backend
- `docker-compose.neo4j.yml` for the explicit Neo4j backend

Those files had grown into a mixed developer, operator, and telemetry story. A
new user who wanted "run Eshu locally" also got Jaeger and an OTEL collector,
even when they were not testing traces. That made the quick start heavier than
it needed to be and blurred the difference between the default runtime path and
developer observability experiments.

The documentation had a second problem: the project standards were written down,
but too much depended on an agent remembering them. That is not good enough for a
runtime repo. Agent instructions, Claude instructions, OpenAPI docs, README
coverage, Go comments, and lint gates need to reinforce each other.

## Decision

Eshu keeps the default local stack small:

- NornicDB remains the default graph backend in `docker-compose.yaml`.
- Neo4j remains the official compatibility graph backend in
  `docker-compose.neo4j.yml`.
- Postgres remains the relational store for facts, queue state, content, status,
  and recovery data.
- Jaeger and the OTEL collector move to `docker-compose.telemetry.yml`.

To run local telemetry, compose the overlay with the graph backend you are
testing:

```bash
docker compose -f docker-compose.yaml -f docker-compose.telemetry.yml up --build
docker compose -f docker-compose.neo4j.yml -f docker-compose.telemetry.yml up --build
```

Repository standards now have a stricter contract:

- `AGENTS.md` and `CLAUDE.md` must stay byte-for-byte in lockstep.
- New and touched exported Go identifiers need useful Go doc comments.
- New and touched unexported Go helpers need comments when they encode a
  contract, storage/query assumption, concurrency rule, retry rule, or
  regression purpose.
- README files are required at ownership roots. Go leaf packages may use
  `doc.go`; docs folders may use `index.md`; generated, vendor, build, cache,
  and fixture leaf directories are exempt.
- OpenAPI changes must update the Go OpenAPI fragments, handler tests, and HTTP
  API reference together.
- The public API router serves `/api/v0/openapi.json`, `/api/v0/docs`, and
  `/api/v0/redoc`; all three documentation routes intentionally bypass API auth.
- CI must run `golangci-lint run ./...` for Go changes.

## Consequences

The default Docker Compose path is easier to start and easier to explain. People
who want telemetry still get it, but they ask for it explicitly.

Agent behavior is less dependent on memory. The instructions now say what must
happen, CI checks the Go lint gate, and repository tests verify the main docs
standards that have hurt us before: AGENTS/Claude drift, missing ownership-root
README files, stale OpenAPI route docs, and a missing `golangci-lint` workflow
step.

This ADR does not claim the historical Go tree is perfectly documented. The
branch sets the bar for new and touched code, fixes the current lint blockers,
and makes future drift visible. A broader cleanup of historical exported comments
can be handled in focused package slices without hiding runtime behavior changes
inside a documentation-only branch.

## Validation

This branch must prove:

- default NornicDB and Neo4j Compose files do not start Jaeger or the OTEL
  collector
- the telemetry overlay defines Jaeger, the OTEL collector, and OTEL env wiring
  for the Eshu runtimes
- automation that uses the telemetry overlay includes an explicit base Compose
  file
- `AGENTS.md` and `CLAUDE.md` are identical
- ownership-root README files exist
- HTTP docs only advertise served OpenAPI routes and mark documentation routes
  public
- `.github/workflows/test.yml` runs `golangci-lint run ./...`
- `golangci-lint run ./...` passes locally
- the MkDocs build passes in strict mode

## Non-Goals

- No production chart telemetry redesign in this ADR.
- No move from `deploy/` to `infra/`, `k8s/`, or `deployment/`; `deploy/`
  remains the best name because it contains Helm, manifests, Argo CD examples,
  Grafana dashboards, and observability assets.
- No vendored Swagger UI or ReDoc asset bundle; the lightweight browser pages
  load their UI libraries from a CDN.
