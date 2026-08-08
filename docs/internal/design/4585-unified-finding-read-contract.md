# Unified finding read contract (#4585)

Status: proposed, awaiting owner sign-off. Design only — this document
authorizes no implementation.

Surfaced by the July 2026 architecture review, section A.5.

## The problem, stated as cost

Eshu has four finding models with no common shape:

| Domain | Model | MCP tool |
| --- | --- | --- |
| Supply chain | `SupplyChainImpactFinding` (`go/internal/reducer/supply_chain_impact_finding.go`) | `list_supply_chain_impact_findings` |
| AWS runtime drift | `AWSCloudRuntimeDriftFindingWriter` (`go/internal/reducer/aws_cloud_runtime_drift.go`) | `list_aws_runtime_drift_findings` |
| Multi-cloud drift | `MultiCloudRuntimeDriftFindingWriter` (`go/internal/reducer/multi_cloud_runtime_drift.go`) | `list_cloud_runtime_drift_findings` |
| Documentation truth | `VerificationFinding` (`go/internal/doctruth/verifier.go`) | `list_documentation_findings` |

A consumer asking "what is wrong in this repository?" must know four shapes,
call four tools, and reconcile four status vocabularies. The cost grows per
domain, and GCP and Azure drift are landing now.

No `Finding` node label exists among the labels in
`go/internal/graph/schema_tables.go`; findings persist as reducer-emitted
facts. That is a constraint on the design, not a gap to fix here.

## What this is, and what it deliberately is not

A **read-model envelope on the query/MCP surface only**: a union view over the
existing per-domain stores, with per-domain registration mirroring the
fact-kind registry pattern.

It is **not** a graph node, **not** a new persistence layer, and **not** a merge
of the domain truth models. Per-domain rigor — suppression provenance,
candidate admission states, drift-specific evidence — stays domain-owned and
reachable through drilldown. The envelope answers "what is wrong, where, how
badly"; the domain answers "why, on what evidence, and under what caveats".

The distinction matters because the failure mode of a unifying layer is
flattening a domain's carefully-earned distinctions into a lowest common
denominator and then having consumers trust the flattened value.

## Envelope fields

| Field | Type | Notes |
| --- | --- | --- |
| `finding_id` | string | Stable, domain-prefixed (`supply_chain:…`, `aws_drift:…`) so ids never collide and the domain is readable without a lookup |
| `domain` | enum | The registration key |
| `severity_normalized` | enum\|null | `critical` / `high` / `medium` / `low` / `informational`, or null |
| `severity_native` | string\|null | The domain's own value, verbatim, or null |
| `status_normalized` | enum | `open` / `resolved` / `suppressed` / `not_applicable` / `indeterminate` |
| `status_native` | string | The domain's own value, verbatim |
| `scope` | object | Repository / workload / cloud-resource refs as the domain provides |
| `evidence_fact_ids` | []string | Handles, not payloads |
| `truth` | `TruthEnvelope` | The canonical wire type, not a bespoke shape |
| `suppression` | object\|null | Presence and domain-owned reason; provenance stays domain-side |
| `drilldown` | object | The domain tool and arguments that return full truth |

**Where a domain has severity, both normalized and native are mandatory.** A
consumer filtering on `severity_normalized` gets a cross-domain answer; one
that must not lose domain meaning reads `severity_native`. Dropping native
would make the envelope lossy in exactly the way that erodes trust in it.

**Severity is nullable, because one domain genuinely has none.**
`VerificationFinding` carries no severity field at all, so any mandatory
severity would force the envelope to invent one for every documentation
finding. An invented severity on a security-facing surface is worse than an
absent one: a consumer sorting by severity would rank fabricated values against
real ones. `null` means "this domain does not grade severity", which is
different from `informational`, and a consumer filtering by severity must
decide explicitly whether to include null rather than have that choice made for
it silently.

`indeterminate` exists so a domain is never forced to claim a finding is open
or resolved when its own model says neither. It is the honest bucket, and its
presence is what keeps the other four meaningful.

## Normalization tables

Verified against source:

**Supply chain** (`SupplyChainImpactStatus`):

| Native | `status_normalized` |
| --- | --- |
| `affected_exact` | `open` |
| `affected_derived` | `open` |
| `possibly_affected` | `indeterminate` |
| `not_affected_known_fixed` | `not_applicable` |
| `unknown_impact` | `indeterminate` |

`possibly_affected` and `unknown_impact` both map to `indeterminate` but must
keep distinct natives: the first is a real correlation with weak evidence, the
second is absence of evidence. Collapsing them would let a consumer read
"unknown" as "possible".

**Documentation truth** (`VerificationStatus`):

| Native | `status_normalized` |
| --- | --- |
| `valid` | `resolved` |
| `contradicted` | `open` |
| `missing_evidence` | `indeterminate` |
| `unsupported_claim_type` | `indeterminate` |

`unsupported_claim_type` is `indeterminate`, not `not_applicable`. The
verifier's own contract says the claim family is *not checked yet*, which is a
statement about coverage, not about the claim. `not_applicable` would read as
"this finding does not apply", quietly converting an unverified claim into a
dismissed one — the reading most likely to hide a real gap.

**AWS and multi-cloud drift: to be confirmed before implementation.** I did not
find status/severity constants in those two files by direct read, so their
tables are deliberately left unwritten rather than guessed. Whoever implements
this must read the two writers and their result structs and complete the tables
here first. A wrong mapping in a security-facing surface is worse than a
missing one, and this document should not be the place a guess enters the
record.

## Bounded-read semantics

Per the API/MCP bounded-read rules, non-negotiable:

- **A canonical input scope is required.** A `list_findings` call with no
  selector would otherwise permit an all-repository, all-cloud scan, which the
  MCP bounded-read contract forbids. The caller resolves a scope first
  (repository, workload, or cloud account) and the tool rejects an unscoped
  call rather than serving one. The `scope` field in the output describes what
  a finding belongs to; it does not constrain what was read, and the two must
  not be confused.
- `limit` required, with a documented maximum; `truncated` always returned.
- Deterministic ordering. Proposed: `severity_normalized` descending, then
  `domain`, then `finding_id`. All three are needed — severity alone ties
  heavily, and an unstable tail makes pagination silently lossy.
- Cursor pagination over the composite ordering key, not offset. A union over
  several stores cannot offset correctly when one store changes underneath.
- `truth` is the canonical `TruthEnvelope` from `go/internal/query/contract.go`
  — `level`, `capability`, `profile`, `basis`, `backend`, `freshness`, `reason`
  — not a bespoke freshness/completeness shape. An envelope that invents its
  own truth fields would not match Eshu's wire contract, and an implementer
  following this document would build a divergent one.
- The union reports the **weakest** contributing domain, not an average: it is
  exactly as fresh as its stalest member, and `reason` names which domain set
  the level so the weakness is attributable rather than anonymous.
- A domain that fails to answer is reported as a named partial, never silently
  dropped. Omission would read as "no findings there", which is the most
  dangerous wrong answer this surface can give.

## One tool or several

**Recommendation: one `list_findings` tool plus retained per-domain tools.**

The union tool answers the cross-domain question. The per-domain tools stay
because they return domain truth the envelope deliberately does not carry, and
the `drilldown` field points at them by name and arguments.

A union tool alone would force the envelope to grow every domain-specific field
over time, which is how the flattening failure arrives by increments.

## Migration

The four existing tools are **retained, not deprecated**. They are the
drilldown targets, so removing them would break the design's own contract.

If a future change does deprecate one, it follows the API versioning policy:
announce, overlap, then remove — and the drilldown mapping must move first.

## Open questions for sign-off

1. Are the drift-domain normalization tables acceptable as an implementation
   prerequisite rather than a blocker on this ADR?
2. Is `indeterminate` acceptable as a first-class normalized status, or should
   an unmappable finding be excluded from normalized filtering entirely?
3. Should `suppression` presence be visible cross-domain, given suppression
   provenance stays domain-owned? Visible presence with domain-owned reason is
   proposed here.
4. Does the weakest-contributor truth rule match how other union reads in the
   codebase report freshness?
