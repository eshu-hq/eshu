# Service Atlas Story Drilldown Design

## Purpose

Service Atlas should explain service evidence, not merely display graph data.
The dashboard first screen is a story surface: it gives a readable summary of a
service, then every visible claim opens the evidence behind it.

This design captures the interaction contract for the next Console slice. It is
intentionally data-shape focused and does not include private service fixtures
or captured company data.

## Product Principle

Every visible number, node, edge, and summary is a drilldown door.

Each drilldown answers four questions in this order:

1. What is this?
2. Why does it matter?
3. What proof backs it?
4. What should the user inspect next?

Raw graph verbs and source identifiers remain available, but the user sees a
plain-language explanation first. The UI should not force users to reverse
engineer operational meaning from relationship names, counts, or file paths.

## First Screen Shape

The first screen should stay story-first:

- A selected service summary.
- A flow map that shows deployment lanes, the service, runtime entrypoints, API
  behavior, and consumers.
- Summary widgets for API surface, deployment evidence, runtime lanes, and
  impact review.
- A translated story panel that states what is known, missing, inferred, stale,
  or slow to resolve.

The page is not a static card grid. It is a progressive explanation surface.
Clicking a tile, graph node, graph edge, lane, hostname, artifact, or consumer
opens a focused drilldown beside the map or in a dedicated detail view.

## Drilldown Contract

All drilldowns share the same structure:

- **Translated story:** one or two sentences that explain the operational
  meaning in plain language.
- **Evidence rows:** the rows that make up the number, node, or relationship.
- **Proof metadata:** source repository, source path, extractor or evidence
  kind, confidence, freshness, truncation, and raw relationship verb when
  available.
- **Truth state:** known, inferred, missing, stale, truncated, or unavailable.
- **Next actions:** open source proof, filter related rows, inspect sibling
  nodes, or run a bounded follow-up query.

The right rail or detail view must avoid generic key/value dumps. Rows should be
grouped and labeled by the question they answer.

## Widget Drilldowns

### API Surface

The API surface tile opens endpoint rows.

Required fields:

- HTTP method or method group.
- Endpoint path.
- Operation name or purpose when available.
- Source kind and source path.
- Workload or service identifier.
- Inferred-purpose marker when purpose comes from the path or operation id.
- Next drilldown to request/response shape, handler proof, or consumers when
  available.

### Deployment Evidence

The deployment evidence tile opens artifact rows.

Required fields:

- Artifact family such as ArgoCD, Kustomize, Helm, or Terraform.
- Translated relationship meaning.
- Raw relationship verb.
- Evidence kind.
- Source repository and source path.
- Target repository, workload, service, or runtime target.
- Environment, confidence, and resolved relationship id when available.

Deployment evidence must preserve tool-family differences. ArgoCD, Kustomize,
Helm, and Terraform answer different questions and should not be flattened into
one generic deployment bucket.

### Runtime Lanes

The runtime lanes tile opens lane cards plus an environment-level table.

Required fields:

- Lane name and translated meaning.
- Environments included in the lane.
- Platform targets claimed by the evidence.
- Source repositories and relationship verbs that created the lane.
- Confidence and resolved relationship ids.
- Related evidence that should remain separate from the selected lane.

The UI must distinguish deployment lanes from provisioning and configuration
influence. A runtime lane can be related to Terraform evidence without implying
that Terraform and GitOps artifacts mean the same thing.

### Impact Review

The impact review tile opens an explicit status panel.

Required fields:

- Query or tool attempted.
- Scope used for the bounded call.
- Timeout, unavailable, stale, partial, or successful state.
- Evidence returned, if any.
- Next safe bounded probe.

If a call is slow or unavailable, the panel should say so. Empty panels are not
acceptable because they hide operational truth.

## Flow Map Drilldowns

The flow map is an explanation engine, not a picture of boxes.

Every node and edge opens the same four-part explanation:

- What this node or relationship means.
- Why it matters to operators, support, engineers, or leadership.
- Which evidence created it.
- What to inspect next.

Example graph objects that must be drillable:

- Deployment lane nodes.
- Service node.
- Hostname or entrypoint nodes.
- API behavior nodes.
- Consumer or dependent repository nodes.
- Edges between deployment evidence and the service.
- Edges between the service and consumers.
- Edges that represent configuration influence or provisioning.

Graph interaction should support selection, filtering by relationship mode,
search, and a persistent inspector. Modals should not be the first interaction
choice; inline drilldown or a side inspector keeps context visible.

## Truth And Translation Rules

The UI should translate raw evidence before exposing raw labels:

- `DEPLOYS_FROM` becomes a deployment-source explanation.
- `PROVISIONS_DEPENDENCY_FOR` becomes a provisioning or runtime-dependency
  explanation.
- `READS_CONFIG_FROM` becomes a configuration-influence explanation.

These translations are not cosmetic. They prevent the UI from implying that all
relationships are the same type of deployment truth.

When the system does not have enough evidence to translate safely, it should say
that the meaning is inferred or unresolved and show the raw evidence.

## Data Handling

Development can use local scratch fixtures captured from a private environment,
but those fixtures must stay out of git.

Repo-committed artifacts should contain only:

- Generic data-shape contracts.
- Sanitized screenshots or diagrams with no private service data.
- Implementation notes that do not include private hostnames, repository names,
  tokens, paths from private machines, or captured payloads.

## Testing And Verification

The implementation plan should include:

- Unit tests for drilldown data adapters.
- Component tests for each widget drilldown state.
- Empty, partial, truncated, unavailable, and inferred truth states.
- Keyboard-accessible node and row selection.
- Browser checks for desktop and mobile layout.
- Proof that scratch fixtures remain ignored and uncommitted.

Runtime or query changes are out of scope for this design spec. If later work
changes API or MCP query behavior, it must include bounded-call proof and
performance evidence appropriate to that surface.
