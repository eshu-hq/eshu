# ADR: Eshu Console Read-Only Product Surface

**Date:** 2026-05-13
**Status:** Proposed
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

**Related:**

- `../reference/http-api.md`
- `../guides/mcp-guide.md`
- `../reference/truth-label-protocol.md`
- `2026-05-09-documentation-truth-collectors-and-actuators.md`
- `2026-05-10-component-package-manager-and-optional-collector-activation.md`
- Issue `#12` - vulnerability intelligence collector
- Issue `#16` - SBOM and attestation collector
- Issue `#19` - observability collector
- Issue `#24` - package registry collector
- Issue `#51` - AWS cloud scanner implementation epic
- Issue `#123` - IaC re-platforming planner
- Issue `#211` - webhook-triggered repository refresh
- Issue `#222` - registry runtime collection
- Issue `#223` - registry graph promotion and query surfaces

---

## Context

Eshu is mainly experienced through MCP today, but the platform already exposes
more read-side truth than a chat client can comfortably browse: repository and
service stories, canonical entity resolution, deployment traces, relationship
evidence, documentation findings, runtime status, indexed repository coverage,
code-search, dead-code, dead-IaC, and operator telemetry.

Near-term work expands that read-side surface further. Webhook-triggered
refresh adds freshness state. Registry, SBOM, vulnerability, observability, AWS,
and IaC re-platforming work add artifact, runtime, security, and management
posture evidence. Directors, engineers, support staff, knowledge-base owners,
security users, and platform operators need a browsable way to inspect that
truth without learning every MCP tool name or API route first.

The existing root React application is the public Cloudflare Pages homepage. It
must stay separate from a private console that can point at real Eshu data.

## Decision

Add a new read-only frontend package at `apps/console`.

The console will be one shared Eshu Console with role lenses, not separate apps
per audience. The home screen is role-neutral and search-first. Users can search
or browse for a repository, service, or workload, then land in an entity-centered
workspace.

The first workspace supports repositories and services/workloads. It renders:

- story narrative first
- structured evidence and limitations beside the narrative
- freshness and coverage state
- deployment-path context
- drilldowns into evidence, code, and findings

The first top-level product areas are:

- **Story** - search to repo/service/workload story pages
- **Dashboard** - read-only runtime, index, collector, and freshness state
- **Catalog** - repositories, services, workloads, and coverage
- **Findings** - dead-code first, later dead-IaC, docs drift, tfstate drift,
  artifact gaps, vulnerabilities, SBOM gaps, and observability gaps

## Runtime And Security Posture

The console is read-only until Eshu has a real auth and authorization system.

Real-data deployments are for local or private-network use only. The local
developer console defaults to the local Eshu API proxy so it can show real
workspace data. Public-facing builds must use explicit fixture-backed demo mode
and must not imply that arbitrary remote Eshu API endpoints are safe to expose
without auth.

Users configure the Eshu API base URL. The console keeps recent environments and
shows the connected API profile, health, truth/freshness state, and demo-vs-real
mode clearly in the shell.

No PR in this console line may add mutating controls such as reindex, replay,
dead-letter, skip, refinalize, component enablement, or workflow activation
until auth, audit, confirmation, and role policy exist.

## API Contract

The console treats the Eshu response envelope as the product contract:

- `data` is the payload
- `truth.level` shows exact, derived, or fallback authority
- `truth.profile` shows the active runtime profile
- `truth.freshness.state` shows fresh, stale, building, or unavailable state
- `error` carries structured codes such as unsupported capability or backend
  unavailability

Client code should be contract-first. TypeScript models and screen adapters must
preserve truth labels, freshness states, structured errors, limits, and
truncation signals instead of flattening them into generic success/failure
states.

## Visualization And Data Density

D3 is for contextual maps of selected paths, not for the default front door.
Examples include deployment paths, dependency paths, blast-radius paths, and
evidence chains for one selected entity.

uiGrid is for dense, evidence-heavy tables such as catalog rows, findings,
evidence drilldowns, dead-code results, collector status, and future artifact or
vulnerability surfaces.

The first implementation may use simple table components while preserving the
adapter boundary needed to introduce uiGrid without changing page contracts.

## Consequences

Positive:

- gives non-MCP users a browsable product surface over the same truth graph
- keeps the public homepage and private real-data console separate
- creates one workspace model shared by engineers, operators, security,
  support, knowledge-base owners, and directors
- lets future registry, SBOM, vulnerability, observability, and cloud collector
  surfaces plug into the same entity workspace

Tradeoffs:

- the first slice is read-only and cannot replace operator repair commands yet
- configurable endpoints require clear demo/private-mode affordances until auth
  exists
- story-first UX requires careful handling of incomplete, stale, and unsupported
  capability envelopes so the UI does not overclaim

## Acceptance

The first implementation is accepted when:

- `apps/console` builds independently with React, Vite, and strict TypeScript
- the console has endpoint configuration, recent environments, demo mode, and a
  visible runtime/status strip
- the API client models the Eshu envelope and structured errors
- repository story pages render from live API responses by default, with typed
  fixture fallback only in explicit demo mode
- the workspace exposes story, evidence, deployment, code, findings, and
  freshness placeholders with real contracts where available
- the first Findings page includes dead-code results
- tests cover envelope handling, endpoint configuration, demo fallback, search
  routing, and story rendering
- `apps/console/README.md` documents local/private real-data mode, public demo
  mode, and verification commands
