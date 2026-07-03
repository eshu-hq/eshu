# Architecture Review 2026-07: Direction, Contracts, OSS Readiness

Part 2 of the July 2026 architecture review; start at [Architecture Review
2026-07](architecture-review-2026-07.md).

---

## Part B — Where the codebase is actually headed

From ~300 recent commits and the design docs, the intentional direction is
clear and consistent: **provider breadth + contract hardening**. Roughly 50
commits are the GCP typed-depth extractor series; ~35 are the replay/cassette
epic (R-layers); ~20 each go to reducer domain-splitting/perf and CI contract
gates; Epic X (telemetry coverage discipline) and Epic V (API
versioning/deprecation headers) are in flight per `CHANGELOG.md` Unreleased.
The extraction policy, component package manager, trust model, and SDK are
all groundwork for out-of-tree collectors — deliberate, documented, and
partially proven (PagerDuty).

Three drifts looked **undecided rather than chosen** at review time:

1. **Three collector authoring patterns.** GCP has a per-asset-type extractor
   registry (146 `extractor_*.go` files, one file per asset type); AWS is a
   pre-registry constants-heavy monolith; Azure (39 files) matches neither.
   Every new AWS scanner deepens the divergence. **(inferred from structure
   comparison; no ADR found.)**
2. **Two IDL directions.** The YAML registry + generated Go + SDK JSON Schema
   is live and enforced; the `proto/eshu/data_plane/*/v1` tree is checked in,
   ungenerated, unimported. Resolved by the
   [Contract System v1 design](design/contract-system-v1.md), which demotes
   the proto tree.
3. **Finding-model fragmentation** ([Part
   A.5](architecture-review-2026-07.md#a5-how-findings-actually-work-today))
   grows one bespoke model per new domain — this conflicts with a coherent
   public query surface and with third-party collectors that will want to
   contribute findings.

One conflict with the contract/OSS goals worth naming plainly: the codebase's
strongest habit — moving fast by keying reducer joins on raw payload map
lookups — is exactly the thing that cannot survive a public boundary
(Part C.5, addressed by the contract-system design).

---

## Part C — The contract that should exist

Superseded by the [Contract System v1 design](design/contract-system-v1.md);
kept here in summary for the record of what the review found.

**What exists and is good:** the envelope
(`go/internal/facts/models.go:28-42`, mirrored publicly in
`sdk/go/collector/types.go`) is stable and complete. Admission classifies
versions (`facts.ClassifySchemaVersion`) and the projector rejects
unsupported majors before projection
(`go/internal/projector/schema_version_admission.go:19-24`). The fact-kind
registry ties every kind to its consumers.

**The hole:** `Payload` is `map[string]any`, persisted as unvalidated JSONB,
and consumed by reducers through `payloadString(...)`-style lookups that
return `""` on a missing key (`go/internal/reducer/aws_relationship_join.go:64-84`).
Rename a payload key in a collector and the reducer silently builds malformed
graph identities — no error, no dead-letter, wrong graph. Six-plus reducer
files duplicate collector string constants specifically to avoid importing
the collector package (`ec2_uses_profile_edge_rows.go`,
`iam_can_assume_edge_rows.go`, `iam_can_perform_catalog.go`,
`iam_escalation.go`, `s3_logs_to_edge_rows.go`,
`secrets_iam_trust_chain_iam_role.go`).

**Resolution:** typed payload structs in a public contracts module, generated
JSON Schemas, a decode seam with version shims, a schema-diff CI gate, a
payload-usage manifest gate, consumer-driven fixture packs, and a written
guarantees doc. Go types + JSON Schema over protobuf/gRPC: the boundary is
store-and-forward through Postgres (no RPC hop), proto3's optional-everything
semantics would reproduce the silent-zero-value bug, and the whole
wire/storage/fixture ecosystem is JSON end to end. Details and the change
matrix live in the design doc.

---

## Part D — What "finalize for mainstream open source" requires

The repo is already public (MIT, SECURITY.md, CONTRIBUTING.md, strict-built
docs site), so this is a maturity question. Missing, in priority order:

1. **The payload contract** (now the contract-system design). Third-party
   collectors cannot be invited onto a boundary where a renamed key silently
   corrupts the graph.
2. **A released, tagged SDK.** `sdk/go/collector` is v0.1.x in-tree; no
   signals of tagged module releases external `go get` can pin were found
   (**inferred** — verify tag history). Needs semver tags, an SDK CHANGELOG,
   and a compatibility table (SDK version ↔ core release ↔ protocol version).
3. **Contributor-tier CI.** The gate floor is excellent and enormous. Define
   the contributor subset (`make pre-pr` is close) and document which gates a
   fork can run without credentials/remote proof.
4. **Human-first contributor docs.** One "write your first collector in an
   afternoon" path: scaffold (`eshu component init collector` exists) →
   fixture → conformance → PR. Most material exists in
   `community-extension-authoring.md`; it needs sequencing and a worked
   end-to-end example repo (the scorecard reference extension is the seed).
5. **The two gaps the performance map names itself:** a published
   SLO/performance contract (the scale-corpus spec defines metrics but no
   targets — `docs/internal/performance-map.md`) and a Postgres tuning doc.
6. **Security posture for third-party code:** trust model designed and mostly
   implemented, but strict Sigstore verification is not wired into default
   runtime activation and there is no live community index. Also needed: a
   vulnerability-response commitment for third-party packages.
7. **Governance basics:** CODEOWNERS/maintainer ladder, a public roadmap
   surface, issue/PR templates aligned with the actual gates.

Not missing (do not re-do): license hygiene, security reporting channel, docs
build gate, API versioning discipline (Epic V), release engineering,
telemetry contract (Epic X).
