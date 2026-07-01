# Eshu monorepo-to-microservices split

Status: design for discussion
Audience: Eshu maintainers
Scope: whether and how to split the `go/` module into extracted services, what stays central, and how Ifá guards the seams

## Reader's guide

This document is deliberately skeptical of its own premise. The measured
coupling data supports a *versioned shared kernel* and a *few* real module
boundaries, but it does **not** support a wholesale monorepo-to-polyrepo split.
Read the [problem](#problem-the-measured-coupling) and
[non-goals](#explicit-non-goals) sections before the extraction plan. The plan
is contingent on a real second consumer existing; if there is only one team
shipping one release train, most of it should not be executed.

## Recommended path (short version)

Do not split the module on ambition. Sequence it:

1. **Enforce boundaries inside the one module first.** Encode the allowed
   dependency directions with an import-lint gate (`depguard` / `go-arch-lint`)
   that fails CI on a violation. This kills the spaghetti risk with zero
   decomposition cost and no fact-schema-drift exposure — it is the highest-value
   step and it is worth doing regardless of whether any module is ever extracted.
2. **Freeze the shared kernel as a versioned contract** (facts split into
   models + registry, truth, telemetry, scope, redact, storage ports) with
   conformance gates — but only as the prerequisite for step 3, not on its own.
3. **Extract clean leaves only, and only when a concrete out-of-tree consumer
   exists** — the collector/SDK family and parser first; defer the entangled
   core (reducer, projector, storage, query) behind proof.

The detailed extraction order later in this document is the "a consumer exists"
plan. Absent that consumer, stop after step 1: enforced boundaries in one module
give clean, navigable code without the coordination cost and distributed-monolith
risk of a polyrepo.

## Problem: the measured coupling

Eshu today is a single Go module under `go/` with many `cmd/*` binaries. The
runtime boundaries are already real and enforced (see
[Runtime shape](#principle-runtime-decomposition-is-not-code-decomposition)),
but the *code* has a small number of very heavy hubs. The numbers below are
from a non-test import-graph measurement of `go/internal/*`.

### First-order hubs (shared kernel candidates)

| Package | Fan-in | Fan-out | Notes |
| --- | --- | --- | --- |
| `internal/facts` | 1465 | ~1 (imports `semanticpolicy`) | Durable fact contract; **not** zero fan-out — see risk below |
| `internal/telemetry` | 499 | 2 | OTEL bootstrap/metrics/spans, properly scoped |
| `internal/scope` | 395 | — | Authorization context, cross-cutting |
| `internal/redact` | 134 | — | Secret masking, cross-cutting |
| `internal/status` | 116 | — | Status/completeness reads |
| `internal/truth` | 59 | 0 | Unified `Evidence` + four-layer materialization contract; clean leaf |

Top `facts` importers: `reducer` (465), `projector` (104),
`storage/postgres` (72), `collector` (71), `collector/terraformstate` (32),
`relationships` (30). The collector tree drives **536** `facts` imports in
aggregate, all one-directional.

### Second-order hub

`internal/reducer` carries **874** internal package references. Breakdown:
`facts` (465, 47%), `telemetry` (95, 11%), `parser` (66), `codeprovenance`
(56), `correlation` (52), `truth` (50), plus smaller edges. The reducer is the
projection multiplier: everything downstream of it transitively depends on
`facts`.

### Structural health

- **No import cycles** exist among `{facts, truth, telemetry, storage, reducer,
  projector, collector, parser, scope}`. The dependency graph is a clean
  acyclic DAG with one-directional flow.
- `internal/facts` conflates **two concerns**: durable envelope models
  (`Envelope`, `Ref`, `FactID`) and queue/registry contracts (`FactKindRegistry`,
  `SchemaVersion`, lifecycle/admission hooks). The generated
  `fact_kind_registry.generated.go` is ~60.5K and spans 40+ fact families
  (AWS, Azure, GCP, Terraform, Kubernetes, CI/CD, SBOM, vulnerability, incident,
  Jira, PagerDuty, Grafana, semantic, service-catalog).
- Clean leaves already exist: `codeprovenance` (77 fan-in, 0 fan-out),
  `exposure` (5, 0), `iacreachability` (6, 0), `parser` (imports only
  `parser`/`telemetry`/`terraformschema`).

### Corrections to earlier coupling claims

Several "blocker" claims in the original proposal were measured at the wrong
altitude (`cmd/` wiring instead of `internal/` domain code) and are false:

- **`reducer` → `query`**: `internal/reducer` imports `query` **zero** times in
  non-test code. The dependency lives only in `cmd/reducer` composition-root
    wiring and uses a small, fixed set of read-path symbols (a `GraphQuery` port
  plus one or two query-profile/config constructors — pin the exact set from
  non-test imports before extraction). A narrow interface extraction dissolves
  it. Reducer extraction is **not** gated on read-path extraction.
- **`reducer` → `postgres.FactStore`/`RelationshipStore` concretes**:
  `internal/reducer` imports `storage/postgres` and `storage/cypher` **zero**
  times in non-test code. `reducer/doc.go` states it does not call graph drivers
  directly; writes go through narrow writer interfaces wired by `cmd/reducer`.
  The concrete coupling is composition-root wiring, exactly where it belongs.
- **`query` → `collector`**: `internal/query` imports `internal/collector`
  **zero** times in non-test code. The "34 read-path packages" fan-out figure
  is inflated by test-only edges and must be re-derived from non-test imports
  before it is used to size or order any extraction.

The correct rule for the rest of this document: **separate composition-root
coupling (cheap, expected, lives in `cmd/`) from domain coupling (expensive, the
real extraction cost).** Every ordering and risk claim below is derived from
non-test `internal/`-package imports only.

## Principle: runtime decomposition is not code decomposition

Eshu already has runtime decomposition. Every runtime is its own binary and its
own Kubernetes object:

| Runtime | Binary | K8s shape |
| --- | --- | --- |
| Schema Bootstrap | `eshu-bootstrap-data-plane` | `Job` |
| API | `eshu api start` | `Deployment` |
| MCP Server | `eshu mcp start` / `eshu-mcp-server` | optional `Deployment` |
| Ingester | `eshu-ingester` | `StatefulSet` |
| Webhook Listener | `eshu-webhook-listener` | optional `Deployment` |
| Workflow Coordinator | `eshu-workflow-coordinator` | optional `Deployment` |
| Resolution Engine | `eshu-reducer` | `Deployment` (lane-capable) |
| Hosted Collectors (19+) | `eshu-collector-*` | optional `Deployment` |
| Scanner Worker | `eshu-scanner-worker` | optional `Deployment` |
| Bootstrap Index | `eshu-bootstrap-index` | one-shot helper |

These map 1:1 to the intake / projection / read boundaries in
`docs/public/architecture.md`. Independent **deployability**, **scaling**, and
**failure isolation** are already achieved today, from one module, enforced by
package ownership + ports + conformance gates.

Therefore the split is **not** about deployability. Splitting the module buys
exactly one thing that separate binaries in one module do not already provide:
**independent versioning for an out-of-tree consumer.** If no such consumer
exists, `go.mod`-level extraction adds coordination cost with near-zero
marginal isolation.

The distributed-monolith trap is explicit here. The pipeline
(`sync -> discover -> parse -> emit facts -> enqueue work -> reducer ->
graph/content projection -> query`) is **synchronous and lockstep-deployed**.
The only place an async boundary is even contemplated is the ingester fact-flow
redesign, deferred to last. Splitting synchronous, lockstep stages into
version-pinned modules is the textbook definition of a distributed monolith:
full polyrepo coordination cost, full monolith deploy-coupling, no isolation win.

**Decision gate before any extraction:** name the concrete second consumer that
must pin a different version than core Eshu ships. If the honest answer is "an
out-of-tree collector/component SDK", extraction of `facts` + the collector SDK
contract is justified. If the honest answer is "our own services that always
deploy together", stop here and enforce boundaries with `internal/` layout plus
an import-lint gate (`depguard` / `go-arch-lint`) instead of splitting the
module.

## The shared kernel that stays central and versioned

These packages are imported by every tier. They stay in `go/` and, if any
extraction proceeds, they are the **first and only things frozen as a versioned
contract module.** No service extraction is safe before these have module
versions and conformance gates.

| Kernel | Role | Freeze action |
| --- | --- | --- |
| `facts/models` (split out) | Durable `Envelope`, `Ref`, `FactID` | Version as the linchpin module |
| `facts/registry` (split out) | `FactKindRegistry`, `SchemaVersion`, lifecycle/admission hooks | Version in lockstep with models; owns the policy gate metadata |
| `specs/fact-kind-registry.v1.yaml` + generated registry | Canonical schema-version/compatibility registry | Version with `facts`; consider domain-sharding (cloud/app/observability/security) so a collector pulls only its family |
| `truth` | Unified `Evidence` (Confidence + Citation + Provenance), four-layer `Contract` | Version alongside `facts`; verified 0 fan-out |
| `telemetry` | OTEL bootstrap/metrics/spans | Central kernel; 2 fan-out |
| `app`, `runtime`, `buildinfo` | Process bootstrap (`HostedWithStatusServer`, `OpenPostgres`, `PprofServer`, `LoadGraphBackend`, version) | Central infra, non-negotiable |
| `scope`, `redact` | Authorization context, secret masking | Central and versioned; cross-cutting |
| Storage **port** interfaces (`GraphQuery`, `GraphWrite`/`cypher.Executor`, `ContentStore`, `FactStore`) | The seams, **not** the drivers | Formalize as a stable contract module so read-path and projection tiers compile against ports |

### Mandatory kernel cleanup before freeze

- **Split `facts`** into `facts/models` (durable envelope) and `facts/registry`
  (kind registry + schema version + admission hooks) so collectors import only
  the models they need.
- **Sever `facts` → `semanticpolicy`.** `facts` is not fan-out-free; it imports
  `internal/semanticpolicy`. A kernel contract that depends on a policy package
  drags `semanticpolicy` into every module pinning `facts`, and churns the
  frozen kernel on every policy change. Before freezing, either move the policy
  gate metadata into `facts/registry` (facts owns it) or invert the dependency
  (define a policy-hook interface in `facts` that `semanticpolicy` implements).
  Verify `facts` has literally zero internal fan-out, then freeze. If the edge
  cannot be cut, `semanticpolicy` must be added to the frozen kernel set —
  which the original proposal did not list.

## Services to extract

Extraction here means "own `go.mod`, pinned to the kernel module." Each is
justified only under the [decision gate](#principle-runtime-decomposition-is-not-code-decomposition):
a real out-of-tree consumer must exist.

### Hosted Collectors (19+ collector binaries + scanner-worker)

- **Kind:** service group (intake tier leaf).
- **Rationale:** highest-value, lowest-risk extraction and the only one with a
  plausible external consumer (an out-of-tree collector/component SDK). Each
  `cmd/collector-*` is already its own binary/Deployment with a uniform, narrow
  dependency surface: kernel + `facts` + 0–8 domain packages. No cross-collector
  imports. They are DAG leaves: they emit facts and enqueue work; nothing
  downstream imports them. Maps 1:1 to the intake runtime boundary.
- **Coupling:** narrow and uniform. Each imports `collector`, `facts`,
  `telemetry`, `scope`, `workflow`, `redact` plus ≤8 domain packages (e.g.
  `awscloud` → `guardset`/`partitionguard`/`relguard`; `terraformstate` →
  `terraformschema`; `jira` → `repositoryidentity`; `packageregistry` →
  `packageidentity`). The blank-import bindings pattern
  (`cmd/collector-aws-cloud/main.go`) already isolates scanner registries for
  clean injection.
- **Risk:** low. The real risk is fact-schema drift: collectors emit
  `facts.Envelope` and must stay pinned to the versioned `facts` +
  `fact-kind-registry` module. Mitigation: version `facts` as a module and gate
  every publish with `backendconformance` + `extensionconformance` corpora.

### Read-Path Hub (API query binary + `internal/query` + search/semantic/answer)

- **Kind:** service (API runtime boundary).
- **Rationale:** read-only by contract — all access goes through
  `GraphQuery`/`ContentStore` ports (`internal/query/ports.go`), never driver
  imports in handlers. Clean DAG leaf, no back-references from `cmd/`. It should
  move as a **cohesive hub** with its tightly-coupled search
  (`searchembed`/`hybrid`/`vector`/`rerank`/`retrieval`), semantic
  (`semanticpolicy`/`profile`/`queue`), and answer (`ask`/`askwiring`/
  `answerguardrail`) neighbors — not split apart, which would create a chatty
  cross-module boundary on the read hot path.
- **Coupling:** high fan-out but **must be re-measured from non-test imports
  before sizing**; the "~34 packages" figure includes at least one stale
  test-only edge (`collector`). Zero write coupling, no cycles. Depends on
  `facts`/`truth` only for citation/evidence read APIs.
- **Risk:** medium, contingent on the re-measured fan-out. The kernel +
  storage-port module boundary must be solid first, or the hub drags half the
  monorepo. If the non-test fan-out is materially smaller than 34, the hub is
  cleaner than claimed and may not need to wait behind all kernel work.

### MCP Server (`cmd/mcp-server` + `internal/mcp`)

- **Kind:** service.
- **Rationale:** pure adapter over `query` — dispatches into the same HTTP query
  handlers so tool and HTTP responses share truth (`internal/mcp/doc.go`).
  Imports ~9 packages, none on the write path (no `reducer`, `projector`, or
  driver imports). Trivial to lift once `query` is a module.
- **Coupling:** thin, all read-path (`query`, `askwiring`, `component`,
  `scopedtoken`, `searchembedruntime`, `semanticpolicy`/`profile`,
  `serviceintelhttp`, `status`, `storage`, `telemetry`). Nothing imports `mcp`.
- **Risk:** low, strictly ordered after the Read-Path Hub — MCP is a leaf on top
  of `query` and cannot move before it.

## What stays in the eshu repo

| Component | Why it stays |
| --- | --- |
| **Resolution Engine / Reducer** (`internal/reducer` + `cmd/reducer`) | 874-import projection multiplier. It does **not** couple to `query` or storage concretes in domain code (that lives in `cmd/reducer` wiring), so the earlier "cut reducer→query first" blocker is false. But it is still the highest blast-radius package; keep it in-repo until the kernel + storage ports are stable modules. Because the coupling is wiring-only, reducer and read-hub extraction can proceed **in parallel**, not strictly ordered. |
| **Projector** (`internal/projector` + `cmd/projector`) | Couples `storage.Cypher`/`Postgres` via narrow ports; mid-pipeline; shares storage ports with reducer. No independent value before the storage-port module exists. Do it as part of projection-tier work. |
| **Ingester** (`cmd/ingester`) | Mid-pipeline glue: couples `collector`, `projector`, `reducer`, `content`, `recovery` in one binary. Not a leaf or a source. Blocked until projector+reducer are modules, and would need a pub/sub fact-flow redesign to decouple. |
| **Storage layer** (`internal/storage`: postgres, cypher, neo4j) | Central write hub, 27 dependents. The `cypher.Executor` seam is real and the reducer/projector domain code **already programs to interfaces** — so the port module mostly needs to *formalize* the writer interfaces `cmd/reducer` injects, not refactor domain code off concretes. Driver code and DDL stay central: it is infrastructure, not a service. |
| **Correlation** (`internal/correlation`) | Semantic-rule engine coupling graph schema, parser, relationships, storage, truth (23 imports). Runs Cypher to derive correlation edges. Stays with the projection tier until the storage abstraction exists. |
| **Schema Bootstrap / Bootstrap Index** | One-shot DDL/seeding utilities, already independent binaries. No module-extraction value; cheapest kept in-repo alongside the schema they own. |
| **Webhook Listener & Workflow Coordinator** | Already independent optional Deployments, tightly bound to the queue/claim/workflow tables in `storage/postgres`. No coupling problem to solve; extracting them buys nothing until a queue/workflow module boundary is drawn. |

## Safe extraction order

This order assumes the [decision gate](#principle-runtime-decomposition-is-not-code-decomposition)
passed. Every step is a stop point with a rollback plan (see
[risks](#risks-and-distributed-monolith-traps)).

0. **Freeze contracts as versioned modules first.** Shared kernel (`facts` split
   into `models`+`registry`, `truth`, `telemetry`, `app`, `runtime`, `scope`,
   `redact`) and the storage **port** interfaces
   (`GraphQuery`/`GraphWrite`/`ContentStore`/`FactStore`). Run the
   [Ifá seam-coverage audit](#how-ifá-guards-the-seams) *before* this step — a
   gate you cannot show catches a break is not a safety net. No service
   extraction is safe until these have module versions and conformance gates.
1. **Extract `facts` + `fact-kind-registry` as a versioned module.** Mechanically
   trivial after the `semanticpolicy` edge is cut. Wire `backendconformance` +
   `extensionconformance` + golden-corpus (B-7/B-12) as the publish gate. This is
   the linchpin every other step depends on.
2. **Extract `parser` as a module.** Imports only `parser`/`telemetry`/
   `terraformschema`; 462 read-only reverse refs. Unblocks collector modules.
3. **Extract the Hosted Collector family + scanner-worker**, one collector at a
   time, each pinned to `facts`/`parser`/kernel via `go.mod`. Highest
   parallelism, lowest risk, real runtime boundary, only tier with a plausible
   external consumer.
4. **Define and extract the storage abstraction module:** port interfaces +
   Postgres/Cypher/Neo4j driver split. Because reducer/projector domain code
   already programs to interfaces, this is mostly *formalizing* the injected
   writer interfaces and re-pointing `cmd/` wiring — **not** a 27-dependent
   domain refactor. Rescope effort accordingly.
5. **Extract Projector** onto the storage-ports module.
6. **Extract Reducer** (+ `correlation` + relationship/semantic-admission rules).
   Extract `query.GraphQuery` and the two config constructors into the kernel/
   storage-ports module so `cmd/reducer` depends on the port, not on read-path
   `query`. This can run **in parallel** with step 7, since the reducer→query
   coupling is a three-symbol wiring edge, not domain entanglement.
7. **Extract the Read-Path Hub** (api/query + search + semantic + answer) once
   kernel and storage ports are stable, and only after the fan-out is
   re-measured from non-test imports.
8. **Extract MCP Server** as a thin module on top of the read-path hub.
9. **Redesign Ingester fact-flow (pub/sub) and extract it last.** This is the
   only step that introduces a real async boundary. If the split is going to
   earn its keep, this is the step that does it — treat it as the thing that
   justifies the split, not an afterthought.

## Risks and distributed-monolith traps

Ordered by severity.

1. **Solution in search of a problem (highest).** Independent deployability,
   scaling, and failure isolation already exist from one module. If module
   extraction has no out-of-tree consumer, it is pure coordination overhead. The
   decision gate must be answered honestly before step 0. **Mitigation:** if no
   real second consumer exists, do not split — enforce boundaries with
   `internal/` layout + `depguard`/`go-arch-lint` + the existing conformance
   gates.
2. **Distributed monolith.** The pipeline is synchronous and lockstep-deployed.
   Version-pinned modules that still deploy together pay full polyrepo cost for
   zero isolation. **Mitigation:** either justify synchronous-but-modular with a
   named consumer, or commit to the pub/sub fact-flow (step 9) up front as the
   thing that earns the split.
3. **Fact-schema drift across module boundaries.** Once collectors, reducer, and
   query pin different `facts` versions, a non-additive envelope or fact-kind
   change silently breaks projection or reads. **Mitigation:** additive-only
   fact evolution; `SchemaVersion` compatibility classes (major = reject with no
   fallback; minor/patch-ahead = not-yet-authoritative; unknown = rejected);
   gate every publish with `backendconformance` + golden-corpus (B-7/B-12).
4. **Premature extraction ahead of the storage-port module** relocates
   entanglement instead of removing it. **Mitigation:** step 0 first; and note
   the reducer/projector domain already programs to interfaces, so the real cost
   is lower than "refactor 27 dependents off concretes" implied.
5. **Splitting the read-path hub apart** creates a chatty cross-module boundary
   on the read hot path. **Mitigation:** move query+search+semantic+answer as
   one hub or not at all.
6. **`cmd/` vs `internal/` conflation** (the trap that produced three false
   blockers above). **Mitigation:** re-derive every risk and ordering claim from
   non-test `internal/` import graphs only. Test-only edges belong behind build
   tags or in a test module and must not inflate the coupling budget.
7. **Performance regression on hot paths.** Module boundaries adding
   serialization, extra copies, or network hops on
   `sync -> reduce -> project -> query` violate the repo performance contract.
   **Mitigation:** every extracted seam needs before/after projection-tail and
   query-latency measurement, not just green tests. Do **not** "fix" a
   concurrency/idempotency regression by single-threading.
8. **60.5K generated registry as a monolithic dependency.** Every collector
   transitively pulls the whole registry unless it is domain-sharded.
   **Mitigation:** shard by fact family (cloud/app/observability/security).
9. **Docs/OpenAPI/wire-contract lockstep breaks across repos.** Changes that are
   one PR today (`openapi*.go` + handler tests + `http-api.md`) become
   multi-repo coordination. **Mitigation:** a shared contract module + a
   conformance gate that fails on drift.
10. **Release-train and rollback gaps.** No point-of-no-return or per-step
    rollback plan existed in the source proposal. **Mitigation:** each step is a
    reversible stop point — a module extraction can be un-vendored back into the
    monorepo until its first external consumer pins it (that pin is the
    point of no return for that module). The security-fix-touches-`facts`-plus-N-
    collectors re-pin/re-release cadence must be a written policy before step 1,
    not discovered in an incident.
11. **Conway/ownership (unaddressed and decisive).** Module and repo splits
    succeed or fail on whether **distinct teams** own the results. If one team
    owns all of Eshu, the split adds process overhead with no organizational
    payoff. **Mitigation:** treat single-team ownership as a strong argument
    against extraction; revisit only when a second team or external SDK consumer
    is real.

## How Ifá guards the seams

Ifá is the conformance and seam-testing platform for exactly the contracts a
split depends on. It is what makes decomposition safe rather than aspirational.
It is **not** a single `ifa` package and it is **not** itself extracted; it
stays central as the contract-test harness that every extracted module is
validated against.

Ifá is realized today by two read-only, corpus-driven packages plus their specs:

- **`internal/backendconformance`** — the graph-adapter conformance matrix and
  reusable read/write corpora, driven by `specs/capability-matrix.v1.yaml` and
  `specs/backend-conformance.v1.yaml`. It gates NornicDB/Neo4j behind identical
  read/write/traversal/dead-code/performance contracts
  (`docs/public/reference/backend-conformance.md`).
- **`internal/extensionconformance`** — validates optional component fixtures
  against the manifest and collector-SDK result contract, without installing
  components or writing graph truth.

In the split, Ifá tests the **seams**, not the internals, of every proposed
module boundary:

- the storage `GraphQuery`/`GraphWrite`/`ContentStore` ports — a backend adapter
  must pass the same corpus regardless of which module it lives in;
- the `facts`/`fact-kind-registry` versioned contract — `SchemaVersion`
  compatibility classes (major = reject; minor/patch-ahead = not-yet-
  authoritative; unknown = rejected);
- the collector/scanner SDK contract via `extensionconformance`.

Ifá is the executable definition of the shared kernel: any module claiming to
implement a seam must pass the conformance corpus before publish. Wire Ifá
(`backendconformance` + `extensionconformance` + golden-corpus B-7/B-12) as the
**mandatory publish gate at extraction-order step 1**, so each module version is
proven against the seam contract rather than trusted.

### Mandatory seam-coverage audit (before step 0)

The safety argument is only as good as the corpus coverage, and today that
coverage is asserted, not evidenced. B-7/B-12 assert projected-truth *shape*;
they do not obviously assert `facts` `SchemaVersion` **cross-module**
compatibility, nor the `GraphQuery`/`ContentStore` **port contract under a
different module version.** Before step 0:

1. Enumerate each seam: `facts` envelope, fact-kind `SchemaVersion` classes,
   `GraphQuery`, `GraphWrite`/`cypher.Executor`, `ContentStore`, collector SDK
   result contract.
2. For each seam, cite the specific conformance test that would **fail** on a
   non-additive break of that seam.
3. Fill every gap in the corpus **first.** A gate that cannot be shown to catch
   the break is not a safety net, and the whole "Ifá makes it safe" claim is
   void without this audit.

## Explicit non-goals

- **Not** a goal to achieve independent deployability, scaling, or failure
  isolation — those already exist from one module and one release of binaries.
  This split does not improve them.
- **Not** a goal to split runtimes that are already independent binaries
  (Ingester, Webhook Listener, Workflow Coordinator, Bootstrap, Schema
  Bootstrap) into separate modules absent a concrete versioning need.
- **Not** a goal to extract the Reducer, Projector, Correlation, or Storage
  drivers as standalone services. Storage stays central infrastructure; the
  projection tier stays in-repo until the storage-port module is stable, and
  even then only if a consumer requires it.
- **Not** a goal to convert the synchronous pipeline into a distributed monolith:
  version-pinned modules that still deploy in lockstep are explicitly rejected.
  Any module boundary that adds a network hop or serialization on the hot path
  without measured justification is out of scope.
- **Not** a goal to break the one-PR docs/OpenAPI/wire-contract lockstep without
  a replacement shared-contract module plus a drift-catching conformance gate.
- **Not** a goal to "fix" concurrency, idempotency, or MERGE-race issues by
  reducing worker count, single-threading drains, or batch size 1 as part of any
  extraction — serialization is not a fix.
- **Not** a goal to extract or relocate Ifá. It stays central as the seam
  test harness.
- **Not** a decision to proceed at all. This document defines *how* to split
  *if* a real out-of-tree consumer exists. Absent that consumer, and absent a
  second owning team, the recommended action is to enforce boundaries with
  `internal/` layout + import-lint + existing conformance gates and **not**
  split the module.

## Referenced sources

- `docs/public/architecture.md` — runtime boundaries, write/read paths, backend
  seam.
- `docs/public/deployment/service-runtimes.md` — runtime map and independent
  Kubernetes shapes.
- `internal/facts/doc.go`, `internal/truth/doc.go`, `internal/reducer/doc.go`,
  `internal/query/ports.go`, `internal/storage/cypher/`, `internal/mcp/doc.go` —
  contract and coupling evidence cited inline.
- `internal/backendconformance/doc.go`, `internal/extensionconformance/doc.go`,
  `specs/fact-kind-registry.v1.yaml`, `specs/capability-matrix.v1.yaml`,
  `specs/backend-conformance.v1.yaml` — Ifá seam contracts.
