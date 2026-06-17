# Correlation Admission Decisions

Use this page when the operator question is "why does this correlation edge
exist, why did that candidate stay provenance-only, what evidence was missing,
and which read proves it?". Eshu's reducers make a correlation **admission
decision** for every candidate before any canonical graph edge is written. This
page explains how to read those decisions, what each state means, and how to
prove that reducer truth, graph truth, and the API/MCP readback all agree.

For the route-level request and response reference, see
[Evidence and supply-chain routes](../reference/http-api/evidence-and-supply-chain.md#admission-decisions).
For the standardized payload contract, see the design gate
[Correlation Admission Decision Payload](https://github.com/eshu-hq/eshu/blob/main/docs/internal/design/2693-correlation-admission-decision-payload.md).

## Decision explanations are not canonical graph edges

An admission decision **explains** a candidate. A canonical graph edge **is**
product truth. The two are deliberately separate:

- A decision row exists for admitted, rejected, ambiguous, stale,
  missing-evidence, permission-hidden, unsupported, and unsafe candidates.
- A canonical graph edge exists **only** when a decision is `admitted`, its
  `canonical_write.eligible` is true, and the domain that owns the write
  recorded `canonical_write.written`.

Reading an admission decision never creates graph truth. The explanation layer
(`go/internal/query` and `go/internal/mcp`) is read-only. A rejected, ambiguous,
stale, missing-evidence, permission-hidden, unsupported, or unsafe candidate must
never be promoted into a graph edge by the explanation surface. If you see a
non-admitted decision whose `canonical_write.written` is true, that is a bug, not
a display quirk — the golden audit (below) fails on exactly that condition.

## Reading decisions

Decisions are read through one bounded route and one MCP tool:

- HTTP: `GET /api/v0/evidence/admission-decisions`
- MCP: `list_admission_decisions`

Every read is bounded to one source generation. The `domain`, `scope_id`, and
`generation_id` filters are required so a read can never sweep the whole graph.
Optional `state`, `anchor_kind`, and `anchor_id` filters narrow the page to one
service, repository, workload, cloud resource, package, incident, or other
domain-owned anchor. `limit` defaults to 50 and is capped at 200; the response
sets `truncated` when more rows exist, and `include_evidence=true` attaches a
bounded set of evidence rows per decision.

Admission decisions require the local-authoritative profile or higher. On a
read-only profile the route returns `501 Not Implemented` with the unsupported
capability code rather than a partial answer.

### Recommended next calls

Each response carries `recommended_next_calls`: bounded follow-up routes — never
a whole-graph search — that move from a decision to its evidence, to the
canonical edge it explains, or to a narrower decision page. Follow these instead
of constructing unbounded queries; they keep an investigation inside the same
scope and generation.

## State meanings

The `state` field is a closed vocabulary shared across every reducer domain.
Domain-native states (for example `exact`, `derived`, `unresolved`, or
`partial`) are preserved in `domain_state`, so you can filter uniformly on
`state` without losing domain detail.

| State | Meaning | Canonical edge |
| --- | --- | --- |
| `admitted` | Evidence satisfies the domain's canonical truth rule. | Written only when `canonical_write.eligible` and `canonical_write.written` are both true. |
| `rejected` | Input is invalid, too weak, out of scope, or policy-denied. | Never. |
| `ambiguous` | More than one candidate could satisfy the selector; no winner is chosen by order. | Never. |
| `stale` | Evidence matched only superseded, tombstoned, or older-than-window state. | Never as current truth; retracts or avoids it. |
| `missing_evidence` | A required source, anchor, endpoint, or corroborating fact is absent. | Never. |
| `permission_hidden` | Evidence may exist but a source ACL, scoped token, or policy hides it from this read. | Never from hidden data. |
| `unsupported` | The provider, source family, relationship type, or language is outside the modeled contract. | Never. |
| `unsafe` | The payload or relationship token is unsafe to materialize or expose without redaction. | Never; only safe reason classes and handles are surfaced. |

A non-admitted decision is not a failure to investigate away — it is the honest,
visible record that Eshu declined to invent a graph edge. Operators use these
rows to find missing or conflicting evidence without fabricating progress.

## Source-handle redaction

Decisions never carry raw provider payloads or secrets. Evidence is referenced by
redaction-safe `source_handles` (fact ids, stable fact keys, content handles, or
citation handles), and each decision records its `redaction_state`:

| `redaction_state` | Meaning |
| --- | --- |
| `safe` | Handles and reason classes are safe to surface as-is. |
| `redacted` | Sensitive candidate values were omitted or fingerprinted before exposure. |
| `permission_hidden` | The viewer may learn a decision exists but not its hidden evidence detail. |
| `unsafe` | Only the reason class and safe handles are surfaced; the payload is withheld. |

Hydrate a handle into excerpt-level evidence through the bounded citation route
rather than expecting raw values inline. Excerpts are out of scope for the core
decision payload.

## Freshness

Every decision reports freshness so a stale candidate is never mistaken for
current truth:

- `freshness_state` — for example current, stale, or building.
- `freshness_observed_at` — the observed time when it is safe to expose.
- `freshness_cause` — the proven reason a row is stale or still building.
- `generation_id` — the source generation the read is bound to.

A `stale` decision stays visible as stale; it does not present as current
admitted truth. See [Freshness and Convergence](freshness-convergence.md) for how
generation and convergence interact across the platform.

## Permission-hidden behavior

Scoped tokens may read only decisions attributable to granted scopes or
repository anchors. An empty or out-of-grant scoped request returns a bounded
empty page **without reading the store**, so a caller never learns whether
another tenant's decision exists. When a caller is allowed to know that hidden
evidence exists, a `permission_hidden` decision exposes counts and reason classes
only — never the hidden payload.

## Verification ladder

A correlation admission change is correct only when four layers tell the same
story. Prove them in order; if any layer disagrees, the change is not done.

1. **Reducer fact intent.** The reducer classifies a candidate and records the
   decision with a stable `decision_id`, the shared `state`, the `domain_state`,
   the `canonical_write` posture, and replay/idempotency behavior. Duplicate
   delivery and stale-generation replay must converge to the same decision.
2. **Graph truth.** Inspect the canonical nodes and edges directly. Only
   `admitted` decisions with an eligible, written canonical block may have a
   corresponding graph edge. Non-admitted candidates must remain provenance-only.
3. **API and MCP parity.** `GET /api/v0/evidence/admission-decisions` and the
   `list_admission_decisions` MCP tool must return the same bounded shape for the
   same scope and generation — same state, same truncation, same source-handle
   counts. MCP adds no data-access logic; it dispatches to the HTTP handler.
4. **Golden audit output.** The cross-domain golden audit compares fixture
   intent against reducer decisions, graph facts, and both readbacks. It fails on
   graph/query disagreement, missing decision explanation, accidental canonical
   writes for non-admitted candidates, duplicate delivery, and stale-generation
   replay.

Run the golden audit locally:

```bash
scripts/verify_correlation_admission_audit.sh
```

The audit suite lives in `go/internal/admissionaudit`, its fixture intent in
`tests/fixtures/product_truth/expected/correlation_admission_golden.json`, and it
is registered in `tests/fixtures/product_truth/manifest.json` for CI and dogfood
runs. The fixture covers admitted, rejected, ambiguous, stale-replay, and
missing-evidence cases across service/deployment, package/supply-chain, and
cloud/resource correlation paths, and every fixture case names the exact
public-safe intent it proves.

## Related references

- [Evidence and supply-chain routes](../reference/http-api/evidence-and-supply-chain.md#admission-decisions)
- [MCP Tool Contract Matrix](../reference/mcp-tool-contract-matrix.md)
- [Freshness and Convergence](freshness-convergence.md)
- [Correlation Admission Decision Payload (design gate)](https://github.com/eshu-hq/eshu/blob/main/docs/internal/design/2693-correlation-admission-decision-payload.md)
