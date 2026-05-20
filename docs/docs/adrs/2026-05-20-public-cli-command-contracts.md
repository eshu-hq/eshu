# ADR: Public CLI Command Contracts

**Date:** 2026-05-20
**Status:** Proposed
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering
**Related:**

- Issue #475: align public-site claims and examples with shipped surfaces
- Issue #476: implement `eshu scan`
- Issue #477: implement `eshu trace service`
- Issue #478: implement `eshu map --from`
- Issue #479: implement `eshu docs verify`
- Issue #480: define public CLI contracts for site-advertised commands
- `../reference/truth-label-protocol.md`
- `2026-05-09-documentation-truth-collectors-and-actuators.md`
- `2026-05-14-service-story-dossier-contract.md`
- `2026-05-14-mcp-tool-contract-performance-audit.md`

---

## Context

The public site needs a simple command story that matches how people talk
about Eshu:

```bash
eshu scan
eshu trace service checkout
eshu map --from terraform/aws_lb.main
eshu docs verify
```

Those commands are good product language, but they are not allowed to become
demo-only aliases. Eshu is a code-to-cloud truth platform. Public commands must
preserve the same accuracy, performance, and reliability rules as the API and
MCP surfaces.

The current CLI already has strong lower-level surfaces:

- `eshu index`, `eshu workspace index`, `eshu watch`, and admin reindex flows
  drive repository ingestion.
- `eshu find`, `eshu analyze`, API routes, and MCP tools read code, content,
  relationship, and impact data.
- `trace_deployment_chain`, `trace_resource_to_code`, `get_service_story`,
  and `investigate_service` already model parts of the code-to-runtime story.
- Documentation findings and evidence packets are designed in the query layer
  and the documentation-truth ADR, but a public CLI verification workflow is
  not yet complete.

This ADR turns the site-facing command names into contracts that future PRs can
implement without creating a second truth path.

---

## Decision

Eshu will treat the four site-facing commands as first-class public CLI
contracts. They must be implemented over the same canonical data-plane,
query-layer, API, and MCP contracts that already power the platform.

All four commands must provide:

- human-readable output for operators;
- `--json` output with stable fields for automation;
- explicit truth metadata: truth level, profile, freshness, evidence IDs,
  warnings, and partial or unsupported state;
- deterministic exit codes for success, partial, ambiguous, stale,
  unsupported, and failed outcomes;
- bounded execution: scope first, default limits, explicit timeouts,
  deterministic ordering, and truncation or continuation metadata;
- evidence-first wording that distinguishes exact, derived, ambiguous, stale,
  unsupported, and incomplete results;
- performance evidence before site or docs examples present the command as
  generally available.

CLI commands remain consumers or orchestrators. They must not introduce a
separate source of truth from the API, MCP, graph, content store, fact store,
or runtime status surfaces.

---

## Shared Output Contract

Human output should lead with the operational answer, then evidence and
warnings. JSON output must follow the existing canonical envelope from the
[Truth Label Protocol](../reference/truth-label-protocol.md): top-level
`data`, `truth`, and `error` only. Command status, scope, warnings, evidence
handles, and truncation metadata belong inside `data`.

```json
{
  "data": {
    "status": "success",
    "command": "trace_service",
    "scope": {},
    "result": {},
    "evidence": {
      "ids": [],
      "packets": [],
      "truncated": false
    },
    "warnings": []
  },
  "truth": {
    "level": "exact",
    "profile": "production",
    "freshness": {"state": "fresh"},
    "capability": "platform_impact.deployment_chain"
  },
  "error": null
}
```

The exact fields can be refined by implementation, but every command must keep
these concepts visible inside the canonical envelope. This ADR does not
supersede the truth-label protocol. A command must not return a confident empty
result when the real state is ambiguous, stale, unsupported, incomplete, or
unreadable.

## Exit Codes

Implementation PRs should keep exit codes stable across the four commands:

| Code | Meaning |
| --- | --- |
| `0` | Complete success for the requested scope. |
| `1` | Runtime or unexpected command failure. |
| `2` | Invalid input, missing required scope, or unsupported flag combination. |
| `3` | Ambiguous input; caller must provide a narrower selector. |
| `4` | Stale, building, or incomplete index state prevents a definitive answer. |
| `5` | Partial result was produced but requested `--fail-on` policy rejects it. |
| `6` | Capability unsupported in the active profile or runtime. |

Commands may add more specific internal error codes in JSON, but shell scripts
should be able to rely on the broad exit-code classes above.

---

## Command Contracts

### `eshu scan`

`eshu scan [path]` means: make the requested source queryable, then prove it is
queryable.

The command must:

- detect a repository, workspace, or configured source root;
- preflight graph backend, Postgres/content store, schema readiness, runtime
  owner state, and discovery root;
- run the existing ingestion/indexing path rather than a new scanner;
- wait for collection complete, source-local projection complete, reducer and
  shared projection queue drain, and zero failed or dead-lettered work;
- print collector-complete, projection-complete, and queue-zero timings as
  separate values;
- surface backend, profile, schema/bootstrap state, retrying work, failed work,
  and dead letters;
- exit non-zero on partial or failed states unless the caller explicitly allows
  partial output.

Accuracy requirement: collection completion is not query readiness. The command
may not report success until the graph and content query surfaces are ready for
the requested scope.

Performance requirement: implementation must capture small, medium, and
representative large-run timing. Full-corpus proof must report collector,
projection, and queue-zero timing separately.

### `eshu trace service <name>`

`eshu trace service <name>` means: explain how a service gets from code to
deployed runtime.

The command must:

- resolve `<name>` across service, workload, repository, deployable-unit, and
  runtime identifiers;
- return disambiguation candidates when more than one entity matches;
- require selectors such as `--repo`, `--env`, or `--service-id` when the name
  alone is not enough;
- use a bounded service trace or service dossier API contract;
- return owning repo, code entry points where known, CI/CD path, image or
  package path, deployment config, runtime workload/resource, cloud
  dependencies, and evidence citations;
- mark missing, stale, derived, and ambiguous links explicitly.

Accuracy requirement: runtime truth cannot be inferred from repository name
alone. Deployment evidence, config evidence, and dependency evidence must stay
separate unless a reducer-owned relationship proves the connection.

Performance requirement: implementation must resolve the smallest service or
workload scope before graph traversal, cap section sizes, expose truncation, and
benchmark against representative whole-organization data.

### `eshu map --from <thing>`

`eshu map --from <thing>` means: start from one known entity and show its
bounded code/cloud neighborhood with evidence.

The command must:

- accept supported handles such as Terraform addresses, ARNs, Kubernetes object
  references, image refs, package refs, repo IDs, file paths, workload IDs,
  service IDs, and graph entity handles;
- normalize the input to one canonical entity handle or return
  disambiguation choices;
- prefer typed relationship, resource, and impact routes before any generic
  graph traversal;
- group output into sections such as `defined_by`, `deployed_by`, `runs_as`,
  `depends_on`, `consumed_by`, and `evidence`;
- support `--depth`, `--limit`, `--relationship`, `--env`, `--repo`, and
  `--json`;
- include truncation and freshness metadata.

Accuracy requirement: a string match is not a resolved entity. Cloud-only,
config-only, derived, candidate, and exact evidence must be labeled.

Performance requirement: input resolution must happen before relationship
expansion. Default traversal depth and result sizes must be bounded.

### `eshu docs verify`

`eshu docs verify [path]` means: extract documentation claims, compare them
against code/API/deployment/cloud truth, and produce durable findings with
evidence packets.

The command must:

- inventory supported documentation sources such as Markdown, docs site
  content, READMEs, runbooks, OpenAPI docs, and later external documentation
  collectors;
- extract checkable claims, including CLI commands, environment variables,
  service names, endpoints, deployment paths, Terraform/Kubernetes/cloud
  references, package/image names, ownership claims, and runbook instructions;
- normalize those claims into durable documentation facts;
- compare claims against canonical truth from the CLI command tree, OpenAPI and
  query routes, graph relationships, collector/runtime facts, and content
  store;
- produce findings such as `valid`, `stale`, `missing_evidence`,
  `contradicted`, `ambiguous`, `unsupported_claim_type`, and
  `inaccessible_evidence`;
- write evidence packets so CLI, API, and MCP consumers read the same truth;
- support `--fail-on`, `--scope`, `--repo`, `--limit`, and `--json`.

Accuracy requirement: a document is not valid merely because it was parsed. A
specific claim is valid only when checked against a matching truth source.
Unsupported claim types must be labeled unsupported.

Performance requirement: claim extraction must be bounded by scope, file count,
content size, fingerprints, and batching. Large documentation sets need
progress/status and stop thresholds.

---

## Implementation Order

1. Land this ADR and update the public issue tracker.
2. Implement `eshu scan` as the readiness contract for issue #476.
3. Implement `eshu trace service` on top of the service story/investigation
   contract for issue #477.
4. Implement `eshu map --from` using typed entity resolution and bounded
   relationship routes for issue #478.
5. Implement `eshu docs verify` as a documentation-truth pipeline for issue
   #479.
6. Update `geteshu.com` examples only after the matching command exists and
   passes focused plus representative runtime proof.

The command PRs may be separate, but they should inherit this ADR's output,
exit-code, truth, freshness, evidence, performance, and observability rules.

---

## Rejected Options

### Ship Cosmetic Aliases First

Rejected. A command that looks polished but does not prove readiness or truth
would make Eshu less trustworthy.

### Put Product Logic In CLI Only

Rejected. CLI-only orchestration would drift from HTTP, MCP, and Console. The
query layer owns read contracts. The data plane owns ingestion and projection.

### Let `docs verify` Read Existing Findings Only

Rejected as the final command contract. Reading existing findings is useful for
drill-downs, but `docs verify` promises active verification. It must extract or
refresh claims, compare them against truth, and persist findings/evidence.

### Allow Whole-Graph Defaults For Convenience

Rejected. If a command needs a service, repo, resource, environment, or entity
scope, it must resolve or request that scope before running expensive reads.

---

## Bounds And Observability

This ADR is design-only. It does not add new runtime paths, graph queries, or
collectors.

Implementation PRs that add graph-backed reads, documentation verification
stages, queue-backed work, or runtime orchestration must include a performance
impact declaration and tracked evidence note. At minimum, that note must name:

- affected stage;
- expected cardinality;
- scope and limit;
- backend/profile;
- timeout;
- result count and truncation behavior;
- small/medium/large proof ladder;
- stop threshold;
- metrics, spans, logs, status fields, or pprof output used for diagnosis.

No-Observability-Change is allowed only when an implementation PR names the
existing signals that already diagnose that path.

---

## Acceptance Criteria

This ADR is accepted when:

- issue #480 references this ADR as the shared command contract;
- the CLI reference marks the four commands as planned, not shipped;
- each implementation issue references this ADR;
- future command PRs follow the shared output, exit-code, truth, freshness,
  performance, and observability contract;
- public-site examples are not updated until the matching command is real,
  tested, and measured.
