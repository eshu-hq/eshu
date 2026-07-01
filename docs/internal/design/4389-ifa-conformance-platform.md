# Ifá — Eshu's Unified Conformance Platform (issue #4389)

Status: Proposed. Naming and scope decision. This document records what Ifá
**is** and — just as bindingly — what it **is not**, before any implementation.
It is the anti-rewrite receipt: the scope is deliberately narrow so the platform
is not torn down and rebuilt after the service decomposition completes.

Issues: #4389 (epic). This ADR is the parent design.

Related prior art (Ifá unifies these; it does not replace them):

- `#1225` — Full E2E integration suite (`docs/internal/design/1225-e2e-integration-suite.md`).
- B-7 golden corpus gate (`go/cmd/golden-corpus-gate/`, `go/internal/goldengate/`).
- B-12 snapshot (`testdata/golden/e2e-20repo-snapshot.json`).
- Cassette record/replay framework (`go/internal/replay/`).
- Performance contract (`go/internal/perfcontract/`).
- CI gate registry (`specs/ci-gates.v1.yaml`, `scripts/dev/select-gates.sh`).

Owners: runtime, collector, reducer, API/MCP, and release-gate maintainers.

## 1. Decision

Adopt **Ifá** as the single, named conformance platform for Eshu. Ifá is one
platform with layers — not several competing test products. Concretely this
decision:

1. **Names and unifies** the test/proof mechanisms that already exist but are
   scattered (E2E suite `#1225`, golden corpus B-7, snapshot B-12, cassettes,
   perf contract) under one identity and **one contributor command**.
2. **Adds exactly one new capability** — determinism-under-concurrency (`replay`
   layer): replay a frozen input N times under N workers and assert an identical
   canonical graph. This is the only genuinely net-new engine work.
3. **Promotes the contract layer to first priority**, because the measured
   coupling (below) makes the fact-envelope contract the dominant cascade risk
   in this repository.
4. **Makes fixture contribution trivial** — a scenario (an "Odù") is a folder
   plus a short `expect.yaml`, auto-discovered.
5. **Shelves** the obfuscation / real-source-capture ambition with an explicit
   trigger condition (§9). It is a beautiful design that serves the narrowest
   goal and fights the open-source goal; it is not funded by this ADR.

Naming is drawn from Yoruba Ifá divination — the same mythology Eshu belongs to.
Eshu (the graph messenger) and Orunmila/Orula (the diviner who knows truth) are
mythological partners; the product and its truth-oracle are companions in the
source material. Naming from one coherent myth is also a scope guardrail: a
component that has no role in the divination metaphor is a signal it does not
belong in this platform.

## 2. Problem

### 2.1 The cascade is measured, not felt

Eshu has decomposed its **runtime** (≈42 deployables in `go/cmd/`, ≈21 of them
collectors; the reducer is now the separately deployed Resolution Engine). It
has **not** decomposed its **code**: one Go module
(`github.com/eshu-hq/eshu/go`), 9,212 Go files, 648 packages, shared internal
kernels. Import fan-in (files importing each shared package):

| Shared package | Files importing it |
| --- | --- |
| `internal/facts` | 1,486 |
| `internal/telemetry` | 608 |
| `internal/reducer` | 297 |
| `internal/query` | 198 |
| `internal/storage/postgres` | 149 |
| `internal/collector` | 109 |
| `internal/projector` | 101 |
| `internal/runtime` | 92 |
| `internal/replay` | 13 |

`internal/facts` is imported by roughly one file in six. A change to the fact
envelope can disturb a sixth of the codebase and every deployable. This coupling
is **intentional** — `facts` is the documented shared-kernel contract
(`docs/internal/agent-guide.md:39-63`), and fact schemas are already versioned
(`specs/fact-kind-registry.v1.yaml`,
`specs/capability-matrix/fact-schema-version.v1.yaml`). The gap is that a change
to that contract is **not guarded**, so it ripples silently. That is a testing
gap, not an architecture failure.

### 2.2 Scattered proof reads as many frameworks

The E2E suite, golden corpus, cassettes, and perf contract have no shared
identity or entry point. That is what makes the proof surface feel like several
frameworks and what keeps external contributors out: they cannot tell which of a
dozen gates to run to know they did not break the core.

### 2.3 The one true gap

Determinism-under-concurrency is not tested. The existing gate compares one run
against a golden snapshot; nothing replays the same frozen input under heavy
parallelism and asserts the graph is identical every time. That is precisely the
class of bug the `Serialization Is Not A Fix` rule cares about (MERGE races,
non-idempotent writes; the #3624 reducer pain).

## 3. Current state and prior art

Ifá is mostly consolidation. What already exists:

| Layer | Exists today as | Ifá action |
| --- | --- | --- |
| Component tests | per-package Go tests | leave in place |
| Fixture replay | `cassette` package (`go/internal/replay/cassette`), 16 collectors | reuse as the `tapes` input |
| E2E flow proof | `#1225` suite design; B-7 gate | fold under Ifá as the E2E layer |
| Structural oracle | B-12 snapshot (`e2e-20repo-snapshot.json`) | reuse; extend with cross-run axis |
| Canonicalization | `go/internal/replay/canonical.go` | reuse verbatim |
| Contract shapes | B-12 `query_shapes` (HTTP/MCP/CLI) | promote to first-class contract layer |
| Perf budgets | `go/internal/perfcontract` (absolute thresholds) | reuse; keep absolute + operator-gated |
| Gate selection | `specs/ci-gates.v1.yaml` + `select-gates.sh` | register Ifá gates; power `make prove` |
| **Determinism under load** | **does not exist** | **build (the one new layer)** |

The git collector is live-only (no replay); it is the one intake with no tape
support today.

## 4. What Ifá is — and is not

Ifá is: a **correctness, contract, and determinism** conformance platform that
proves a change did not break the core, offline, credential-free, on demand.

Ifá is **not**:

- Not a production load or latency test. It measures CPU, lock contention, and
  projection cost under frozen input — never network/IO throughput or a prod SLO.
- Not a functional-correctness oracle by itself. The determinism layer asserts
  "same input → same graph," not "the graph is right." Functional truth stays
  with the golden corpus, which lives beside it.
- Not the live-fetch path. Replay replaces collector transport (auth,
  pagination, retries); bugs there surface only in live runs.
- Not the obfuscation / real-source-capture engine (§9).

## 5. Naming

- **Ifá** — the platform. The one product name. The whole practice of
  divination — corpus, casting, verdict — as one system.
- **Orula** (Orunmila) — the engine inside Ifá that renders the verdict. A
  component, not a second product.
- **Odù** — one scenario/fixture with a known-correct outcome. "Add an Odù."
- Lowercase functional parts under Ifá: `corpus`, `tapes`, `replay`,
  `contracts`, `budgets`. Parts, never peers.

Rule: lore names the **identity** (platform, packages, docs, branding). The
contributor-facing command stays plain English (`make prove`). ASCII everywhere
in code and identifiers; diacritics only in prose.

## 6. The layers

Priority order reflects the measured coupling, not convention.

1. **contracts (first priority).** Guard the `facts` seam and the API/MCP/CLI
   response shapes. Hook the existing versioning machinery
   (`fact-kind-registry.v1.yaml`, `fact-schema-version.v1.yaml`): on a fact-kind
   change, replay tapes and prove consumers still produce the true graph and the
   schema-version compatibility classification still holds. This is the direct
   antidote to §2.1 — it turns the 1,486-file blast radius from invisible to
   gated.
2. **replay + determinism (the one new layer).** Replay a frozen corpus at
   `workers=1` to establish the canonical truth, then at `workers=N` (repeated),
   asserting an identical canonical graph. A divergence is, by construction, a
   concurrency bug — the input cannot vary. Built on `canonical.go`.
3. **corpus.** The fixed reference repos plus expected truth (today's golden
   corpus / B-12).
4. **tapes.** Recorded, credential-free inputs (today's cassettes). One fixture
   source among several, not the platform's identity.
5. **contracts:readback.** API, MCP, CLI response-shape parity (today's B-12
   `query_shapes`; `#1225` readback parity gate).
6. **budgets.** Performance thresholds — absolute, `hermetic_gate` vs
   `operator_gated`, per the existing `perfcontract` pattern. Determinism is a
   separate equality assertion, never expressed as a perf budget.

## 7. Placement

- **Ifá lives in the eshu repo.** It is one Go module; there is nowhere else,
  and it is correct. Polyrepo is moot until modules split.
- **Follow the established gate shape** (`go/cmd/golden-corpus-gate/` +
  `go/internal/goldengate/`): Ifá is `go/cmd/ifa/` + `go/internal/ifa/`, or it
  absorbs the golden-corpus gate. Contributors already know this shape.
- **Ifá depends on contracts only** — `facts`, `truth`, `storage` ports, query
  shapes, the fact-kind registry. Never a service's internal logic (collector,
  parser, projector, runtime internals). This obeys the repo's existing
  port-discipline (`agent-guide.md:60`). If Ifá reaches into a service's guts,
  Ifá becomes new coupling; this rule is load-bearing and is what lets Ifá move
  unchanged if `facts` is later extracted into its own module.

## 8. Contributor contract

- **One door:** `make prove`. It selects the layers the change touched via the
  existing `ci-gates` path triggers and returns one verdict: green, or what
  broke and where. No tribal knowledge, no credentials.
- **Add an Odù = a folder plus a short file:**

  ```text
  testdata/odu/<name>/
    input/          a tiny repo or a recorded tape
    expect.yaml     e.g. "graph has 3 CALLS edges from func A"
  ```

  Auto-discovered and run in the right layer. No framework knowledge required.
  The ease of adding a case is treated as sacred: if a contributor must read
  docs to add a scenario, the design has failed.

## 9. Shelved scope and trigger

Not funded by this ADR: the obfuscation subsystem, real-source capture, git
working-tree capture, keyed pseudonymization, and any private real-data corpus.

Reasons: multi-quarter and security-sensitive; a permanent per-fact-field and
per-language maintenance tax; serves the narrowest goal (realistic-scale load);
and fights the open-source goal because that data must stay private-tier, so the
community can never use it.

**Trigger to revisit:** only if the shipped layers (§6) demonstrably fail to
catch a real bug that is reproducible **only** with real-estate topology, and
crafted synthetic pathological corpora cannot reproduce it either. Until then
the design stays on the shelf, referenced, unbuilt.

## 10. Anti-rewrite rules

Each rule is also a scope cut — the way to avoid a rewrite is to refuse to build
the parts that rot.

1. **Assemble, don't invent.** Ifá is a thin layer over gates that already
   exist. Large new engine code is a smell we are doing it wrong.
2. **Determinism rides `canonical.go`.** Build the cross-run assertion on the
   already-deterministic canonicalizer, and prove N-run stability on existing
   tapes before it is allowed to block anything. A flaky gate is muted, then
   deleted; trust is earned once.
3. **Ease of adding an Odù is sacred** (§8).
4. **Contract-only dependencies** (§7). Ifá tests seams and artifacts, never
   internal layout — so package churn during decomposition does not touch it.
5. **Honest scope** (§4). Never sold as a prod load test. The moment someone
   expects throughput numbers and does not get them, the platform is replaced.
6. **Extend, never parallel.** Grow B-7/B-12/`#1225`; do not stand up a second
   overlapping system.

## 11. Phasing

- **P0 — Determinism on existing tapes.** Build the cross-run + concurrency
  assertion on the 16 existing cassettes, reusing `canonical.go` and B-12. No
  obfuscation. Wins trust; attacks #3624.
- **P1 — Name the platform and the door.** Register Ifá gates in
  `ci-gates.v1.yaml`; wire `make prove`; fold `#1225` and the golden corpus
  under the Ifá identity.
- **P2 — Contract layer.** Guard the `facts` seam and query shapes against the
  fact-kind-registry + schema-version machinery. (Highest-leverage for §2.1.)
- **P3 — Drop-an-Odù ergonomics.** Folder + `expect.yaml` auto-discovery.
- **P4 — Coverage view.** Surface what is and is not covered (grammar node-type
  universe for parser layers; fact-kind coverage for the contract layer).

## 12. Evidence and verification

- Each layer registers as a gate in `specs/ci-gates.v1.yaml` with path triggers,
  so `make pre-pr` / `make prove` select only what a change touches.
- Determinism proof: N-run identical canonical graph at `workers=1` vs
  `workers=N`, clean state per run.
- Contract proof: a fact-kind change replays tapes and shows consumer graph
  truth unchanged plus schema-version compatibility preserved.
- Budgets: absolute thresholds mirrored doc↔code↔lockstep test, per
  `perfcontract`.
- Determinism is asserted as equality, distinct from any perf budget.

## 13. Open decisions

1. Resolved: epic is #4389 and this file is `4389-ifa-conformance-platform.md`.
2. Does Ifá **absorb** `go/cmd/golden-corpus-gate` or **wrap** it? (Lean:
   absorb over time; wrap first to avoid churn.)
3. Confirm the relationship to `#1225` — fold its children under the Ifá epic,
   or keep `#1225` as the E2E layer sub-epic beneath Ifá.
