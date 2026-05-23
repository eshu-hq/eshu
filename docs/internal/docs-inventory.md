# Docs Inventory

This inventory groups the repository's Markdown files by function. Use it to
decide whether a document should be public product guidance, maintainer-only
guidance, package-local orientation, historical evidence, or deleted.

Current inventory snapshot after removing historical working-plan,
decision-log, misplaced console, and stale internal working-note docs: 548
Markdown files.

## Functional Groups

| Group | Count | Paths | Function | Default action |
| --- | ---: | --- | --- | --- |
| Public product, concepts, and contribution docs | 15 | `docs/public/*.md`, `docs/public/concepts/`, `docs/public/understand/`, `docs/public/extend/`, `docs/public/releases/` | Explain what Eshu is and how the product fits together. | Keep, but remove overlap with the root `README.md`. |
| Public onboarding and local run docs | 5 | `docs/public/start-here.md`, `docs/public/getting-started/`, `docs/public/run-locally/` | Help a new user install, run, and connect Eshu locally. | Keep and make this the main beginner path. |
| Public deployment, operations, and service docs | 29 | `docs/public/deployment/`, `docs/public/deploy/`, `docs/public/operate/`, `docs/public/services/` | Explain Compose, Helm, Kubernetes, service runtimes, and operations. | Keep, but merge duplicated Docker Compose, Helm, and service-runtime guidance. |
| Public workflow guides | 18 | `docs/public/guides/`, `docs/public/use/`, `docs/public/mcp/` | Explain user tasks such as indexing, MCP use, relationships, fixture ecosystems, and Terraform providers. | Keep only task-oriented pages. Merge or delete pages that repeat reference material. |
| Public reference docs | 74 | `docs/public/reference/` | Authoritative CLI, API, config, telemetry, backend, and protocol reference. | Keep as canonical reference. Remove tutorial prose that belongs in guides. |
| Public language support docs | 32 | `docs/public/languages/` | Explain parser and language-family support. | Keep, but drive from code and matrices. |
| Maintainer docs | 9 | `docs/internal/` | Maintainer-only workflow guidance, cleanup tracking, and generated file indexes for this docs PR. | Keep only active workflow guidance. Delete stale investigation notes or move durable facts into reference docs. |
| Removed historical plans | 0 | `docs/plans/`, `docs/superpowers/` | Former working plans, specs, and proof notes. | Deleted from the stable docs tree. Durable learnings must live in current reference, workflow, architecture, or package-local docs. |
| Removed historical decision records | 0 | `docs/public/adrs/` | Former decision logs and proof trails. | Deleted from the stable docs tree. Durable decisions, workflows, performance lessons, and backend rules must live in current reference, workflow, architecture, or package-local docs. |
| Root governance and entrypoints | 8 | root `*.md` files | Repo entrypoints, contributor guidance, security, testing, agent rules, and Cloudflare Pages setup. | Keep root `README.md`, `CONTRIBUTING.md`, `SECURITY.md`, `TESTING.md`, `DEVELOPING.md`, `AGENTS.md`, `CLAUDE.md`, and `CLOUDFLARE_PAGES.md`. |
| Deployment artifact docs | 4 | `deploy/**/README.md` | Explain deploy artifacts near their manifests. | Keep when artifact-specific. Link to public deployment docs instead of duplicating procedures. |
| Go command package docs | 35 | `go/cmd/**/README.md`, `go/cmd/**/AGENTS.md`, `go/cmd/README.md` | Orient maintainers and coding agents around command binaries and command-specific rules. | Keep command READMEs for humans and command-local `AGENTS.md` for scoped agent rules. |
| Go internal package docs | 273 | `go/internal/**/*.md` | Package-local ownership, invariants, implementation orientation, and scoped agent instructions. | Keep package READMEs, package `AGENTS.md`, and focused change guides. Delete only generated prose that adds no package-specific information. |
| Fixture and test docs | 40 | `tests/fixtures/**/README.md` | Explain fixture intent and expected behavior. | Keep if tests rely on intent. Merge tiny repeated fixture docs where the parent README can cover them. |
| Script, app, spec, and Go module docs | 4 | `scripts/README.md`, `apps/console/README.md`, `specs/README.md`, `go/README.md` | Local entrypoints for smaller repo areas. | Keep only if they name commands or contracts not covered elsewhere. |
| Support artifacts | 2 | `docs/dashboards/`, `docs/diagrams/`, `docs/openapi/`, `docs/mkdocs.yml`, `docs/README.md` | Site config, diagrams, dashboards, and OpenAPI support files. | Keep. These are artifacts, not narrative docs. |

## Delete-Or-Archive Candidates

Start with documents that are both outside the public navigation and either
stale, duplicated, or contradicted by the current code.

| Candidate set | Why it is a candidate | Action before delete |
| --- | --- | --- |
| Deleted `docs/superpowers/**` | Historical working plans carried stale planned routes and commands, such as the planned graph neighborhood route and `analyze impact` / `analyze neighborhood` commands. | Done. Current references point at public docs, package docs, or this inventory. |
| Deleted `docs/plans/**` | These were implementation notes, not user or maintainer guidance. At least one documented an invalid command shape. | Done. Current references point at public docs, package docs, or this inventory. |
| Deleted `docs/public/adrs/**` | Public ADRs were long historical implementation logs and were not part of normal docs navigation. They buried the current answer under old proof trails. | Durable lessons were moved into current architecture, workflow, performance, backend, MCP, and collector-readiness docs. Repair any future reference to point at those current docs. |
| Deleted `docs/internal/2026-04-*` investigation notes | Maintainer-only notes from older workstreams. The current code and public docs now supersede them. | Durable AWS collector, MCP, reducer, and architecture facts live in current reference and package-local docs. |
| Deleted root `PRODUCT.md` and `DESIGN.md` | These were console-only docs at the repo root. They competed with public product docs and the console app README. | Durable console product and design contracts now live in `apps/console/README.md`. |
| Package-local generated boilerplate | Some package-local docs still repeat parent guidance without adding local invariants. | Keep docs that explain package invariants or cross-cutting workflow rules. Delete only boilerplate that adds no package-specific information and is not harness-loaded scoped guidance. |

## Review Order

1. Confirm the docs estate and deletion policy in this inventory.
2. Keep durable decision-record lessons in current architecture, workflow, performance,
   backend, MCP, and collector-readiness docs.
3. Sweep package-local `README.md`, `AGENTS.md`, and focused change-guide files
   by subsystem; keep scoped agent files where they carry package rules.
4. Update `docs/internal/docs-change-tally.md` after each pass so the PR keeps
   a running created/modified/deleted record.

## Verification

Use the broad verifier to find stale claims outside the public docs tree:

```bash
cd go
go run ./cmd/eshu docs verify .. --limit 2000 \
  --fail-on contradicted,missing_evidence
```

Use the public-doc verifier for the published site:

```bash
cd go
go run ./cmd/eshu docs verify ../docs/public --limit 1000 \
  --fail-on contradicted,missing_evidence
```
