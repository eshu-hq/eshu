# Visualization Packet Contract

A **visualization packet** is a compact, bounded, derived view of an existing
query response that a client can render as an explainable subgraph **without raw
Cypher or a Neo4j-browser-only workflow**. It does not query the graph; it
transforms a story, evidence-citation, or incident-context response the caller
already received into stable nodes and edges, with truth/freshness metadata,
payload limits, and explicit truncation.

The implementation lives in `go/internal/query/visualization_packet.go`
(types and limits), `go/internal/query/visualization_packet_story.go`
(service-story builder), and `go/internal/query/visualization_packet_evidence.go`
(evidence-citation and incident-context builders). It is a sibling of the
[Answer Packet Contract](answer-packets.md): it reuses the same `TruthEnvelope`
and freshness language and the same
[evidence citation handle](evidence-citation-handles.md) shape, so a rendered
node maps back to a citation handle rather than inventing a new one.

## Why this contract exists

Story and incident answers already carry graph-shaped evidence — the service
story dossier's `evidence_graph`, `upstream_dependencies`, and
`downstream_consumers`; the incident context response's `evidence_path`. To draw
that subgraph today, a client would otherwise re-query the graph or open a
Neo4j browser. The visualization packet exposes the subgraph that is **already
present in the authorized response** as a bounded, deterministic node/edge set
so any client can render it directly.

## Non-goals

- The packet does **not** query the graph, run Cypher, or perform any privileged
  read. It is a pure transformation of a response the caller already received.
- The packet does **not** surface any field the source response did not already
  contain. It carries only labels, identities, and handle fields that were
  present in the story, citation, or incident response.
- The packet does **not** introduce a new truth taxonomy. It copies the source
  `TruthEnvelope` and carries per-node/edge truth labels straight from the
  source (for example an incident evidence-path truth label).

## Privacy and authorization invariant

A visualization packet is built **only** from a response the caller already
received from an authorized query. The builders take that response value as
their sole input and perform no new reads. They never open content, never widen
scope, and never populate a field beyond what the source carried. This invariant
is covered by tests that assert no fabricated labels or truth labels appear and
that a citation excerpt never leaks into a node.

## Stable identifiers

Node IDs are derived deterministically from the underlying entity or handle
identity, **never from iteration order**:

| Source | Identity used for the node ID |
| --- | --- |
| Service story service node | `service_id` (falling back to `service_name`) |
| Service story repository node | privacy-safe `canonical_key` when the source proves one; otherwise the repository observation `id` |
| Evidence citation node | `entity_id`, else `repo_id` + `relative_path` |
| Incident anchor node | `provider` + `provider_incident_id` |
| Incident evidence slot node | incident anchor + slot name |

The identity is hashed into a short opaque token (`viznode:…`). Equal identities
always produce the same ID, so the same response always yields the same nodes
and edges regardless of input order. Edge IDs (`vizedge:…`) are derived from the
source node ID, target node ID, and relationship label. Tests shuffle the input
rows and assert the output node IDs and ordering are identical.

## Payload limits and truncation

Nodes and edges are sorted by stable ID and bounded by `VisualizationMaxNodes`
(60) and `VisualizationMaxEdges` (120). When a packet exceeds the bound:

- nodes beyond `VisualizationMaxNodes` are dropped, sorted by ID, and recorded in
  `truncation.dropped_node_ids` with `truncation.dropped_node_count`;
- any edge whose endpoint was dropped is itself dropped (no edge dangles), and
  edges beyond `VisualizationMaxEdges` are dropped, both counted in
  `truncation.dropped_edge_count`;
- `truncation.truncated` is set and a human-readable limitation is appended.

When the **source** response was already truncated (the story
`evidence_graph.truncated`, `downstream_consumers.truncated`, the citation
`coverage.truncated`, or the incident `truncated` flag), the packet also marks
truncation and notes that it is a bounded subset of an already-bounded response.

## The VisualizationPacket

```jsonc
{
  "view": "service_story",
  "title": "payments",
  "supported": true,
  "nodes": [
    {
      "id": "viznode:…",
      "type": "service",
      "label": "payments",
      "category": "service",
      "role": "workload",
      "evidence_handle": { "kind": "entity", "repo_id": "svc-repo", "entity_id": "svc-1" }
    },
    {
      "id": "viznode:…",
      "type": "repository",
      "label": "billing",
      "category": "deployment",
      "role": "deployment_configuration",
      "canonical_key": "repository:r_…",
      "scope_key": "scope:s_…",
      "evidence_handle": { "kind": "entity", "repo_id": "up-1", "entity_id": "up-1", "evidence_family": "repository" }
    }
  ],
  "edges": [
    {
      "id": "vizedge:…",
      "source": "viznode:…",
      "target": "viznode:…",
      "relationship": "DEPENDS_ON",
      "truth_label": "exact"
    }
  ],
  "truth": { "level": "exact", "basis": "authoritative_graph", "freshness": { "state": "fresh" } },
  "limits": { "max_nodes": 60, "max_edges": 120, "ordering": "stable_id", "node_count": 2, "edge_count": 1 },
  "truncation": { "truncated": false, "dropped_node_count": 0, "dropped_edge_count": 0 },
  "limitations": [],
  "recommended_next_calls": []
}
```

### Field contract

| Field | Contract |
| --- | --- |
| `view` | The derived-view family: `service_story`, `evidence_citation`, `incident_context`, or `unsupported`. |
| `title` | Short human-readable subject for the subgraph. Optional. |
| `supported` | `false` when no subgraph could be derived; the packet then carries `limitations` and `recommended_next_calls` instead of nodes/edges. |
| `nodes` | Bounded, deterministically ordered nodes. Each has a stable `id`, a `type`, a `label`, optional `category` and source-proven `role`, optional privacy-safe repository `canonical_key` / observation `scope_key`, an optional `truth_label`, the compatibility `evidence_handle`, and optional `evidence_handles` preserving every reconciled observation. |
| `edges` | Bounded, deterministically ordered edges. Each has a stable `id`, `source`/`target` node IDs, a `relationship` label, an optional `truth_label`, and an optional `evidence_handle`. |
| `truth` | A copy of the source response's `TruthEnvelope`. Canonical truth metadata for the subgraph. |
| `limits` | Payload bounds (`max_nodes`, `max_edges`), the `ordering` contract (`stable_id`), and the retained `node_count`/`edge_count`. |
| `truncation` | What was dropped to stay within bounds: `truncated`, `dropped_node_count`, `dropped_edge_count`, `dropped_node_ids`. |
| `limitations` | Bounded human-readable caveats (truncation, missing source fields, unsupported view). |
| `recommended_next_calls` | Bounded follow-up calls, in the same shape as the evidence-citation `recommended_next_calls`. |

## How the views relate to story evidence and citation handles

- **`service_story`** is derived from a service story dossier response. The
  service node comes from `service_identity`; repository nodes and their edges
  come from the dossier `evidence_graph`, `upstream_dependencies`, and
  `downstream_consumers`. The workload is the only service anchor. Source-code,
  deployment-configuration, runtime-instance, and downstream-consumer nodes
  carry distinct roles and layout categories. Repository observations reconcile
  only when the source provides the same privacy-safe `canonical_key`; labels
  never establish identity. Without canonical evidence, distinct observations
  retain privacy-safe `scope_key` disambiguation. A reconciled node carries all
  observation handles in `evidence_handles` so the collapsed provenance remains
  inspectable. Each node also carries an `evidence_handle` in the
  `evidence_citation` handle shape so a rendered repository or service maps back
  to the citation that hydrates it. Relationship confidence in the source row is
  folded into a truth label using the existing `exact`/`derived`/`fallback`
  vocabulary; no confidence means no label.
- **`evidence_citation`** is derived from an evidence-citation response. Each
  resolved citation becomes one node whose `evidence_handle` is rebuilt from the
  citation's own handle fields (`kind`, `repo_id`, `relative_path`, `entity_id`,
  `evidence_family`, line range). The citation packet has no relationships, so
  this view carries nodes with no synthetic edges, and never carries the citation
  excerpt body.
- **`incident_context`** is derived from an incident context response. The
  incident anchor is one node; each `evidence_path` slot becomes a node, and
  consecutive slots are joined by an `EVIDENCE_PATH` edge. The per-slot
  `truth_label` is carried straight from the source evidence edge.

So a node in any view maps back to the same `evidence_citation` handle a client
would send to `build_evidence_citation_packet` to hydrate the underlying
content, keeping the visualization, story evidence, and citation handles
aligned.

## Unsupported views

When the source response carries no subgraph to render — an empty story
response, a citation response that resolved nothing, an incident response with no
evidence path — the builder returns an explicit packet with `view:
"unsupported"`, `supported: false`, a `limitations` entry naming the gap, and
`recommended_next_calls` pointing at the call that would produce a renderable
response (for example `get_service_story`, `build_evidence_citation_packet`, or
`get_incident_context`). It never errors opaquely.

## API and MCP usage

Clients can derive a packet without re-querying graph or content state through:

- HTTP: `POST /api/v0/visualizations/derive`
- MCP: `derive_visualization_packet`

Both surfaces take the same body:

```json
{
  "view": "service_story",
  "source_response": {
    "service_identity": {
      "service_id": "svc-1",
      "service_name": "payments",
      "repo_id": "svc-repo"
    },
    "upstream_dependencies": [],
    "downstream_consumers": {}
  },
  "source_truth": {
    "level": "exact",
    "basis": "authoritative_graph",
    "freshness": { "state": "fresh" }
  }
}
```

The response data wraps the packet under `visualization_packet`:

```json
{
  "visualization_packet": {
    "view": "service_story",
    "supported": true,
    "nodes": [
      { "id": "viznode:...", "type": "service", "label": "payments", "category": "service", "role": "workload" }
    ],
    "edges": [],
    "limits": { "max_nodes": 60, "max_edges": 120, "ordering": "stable_id", "node_count": 1, "edge_count": 0 },
    "truncation": { "truncated": false, "dropped_node_count": 0, "dropped_edge_count": 0 }
  }
}
```

When the client requests the canonical Eshu envelope, the route copies
`source_truth` to the envelope truth and the packet truth. The MCP tool returns
that same envelope in `structuredContent` and in the
`application/eshu.envelope+json` resource block; the text summary is only a
bounded convenience string.

## Reused contracts

The visualization packet does not duplicate existing shapes. It reuses:

- `TruthEnvelope`, `TruthLevel`, and `TruthFreshness` from `contract.go`.
- The `evidence_citation` handle shape and `recommended_next_calls` convention
  from `evidence_citation.go` and the
  [Evidence Citation Handle Contract](evidence-citation-handles.md).
- The service-story dossier response shape from `service_story_dossier.go` and
  the incident `evidence_path` from `incident_context_types.go`.
