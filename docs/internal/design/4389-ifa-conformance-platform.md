# Ifá — validated conformance platform design

Status: draft design (validated current-state inventory)
Audience: Eshu maintainers
Companion issue: #4389

Every claim in this document is grounded in a validation pass against the live
tree. Where a prior assumption was refuted or corrected by measurement, the
correction is called out inline with a "Correction" note so a future reader can
see what we believed, what the code actually says, and how the design changed as
a result. If a section makes a claim with no `file:line` or command behind it,
treat that claim as unvalidated and do not build on it.

---

## What Ifá is and is not

Ifá is a **validated conformance platform**: a single named identity that binds
Eshu's already-existing record/replay, canonicalization, golden-corpus, perf,
gate-registry, and coverage machinery into one contract surface, and broadens
what those cover only narrowly. The target capability is an **end-to-end
offline tester, replayer, and load tester** that needs no provider
infrastructure or credentials — a contributor with no Azure, AWS, or GCP access
can prove conformance, determinism, throughput, and failure behavior locally.
The name follows the repository's Yoruba naming convention (Eshu, Odù); an
*Odù* in Ifá is a validated conformance case a contributor drops in. The
containment is lore-correct — the Odù are the units of the Ifá system — which
is why the platform is named for the system, not the diviner. *Orunmila* (the
orisha who reads the Odù) is deliberately reserved: if the verdict-rendering
component (the judge/comparator or the `prove` report) ever becomes its own
named artifact, that name is Orunmila. It is not used for anything today.

Ifá **is**:

- A **contract layer** over the fact fan-in seam. Facts already flow through a
  single canonical intermediate type and a single load gateway (see
  [Contract layer](#layer-1-contract-layer-first-given-fact-fan-in)), so a
  conformance platform can assert on that seam without re-running collectors.
- A **determinism-under-load harness** — generalizing the existing
  `schedulereplay` invariance pattern — that replays the same recorded input
  through the full pipeline at varied worker counts and asserts an identical
  canonical graph.
- A **load and saturation harness** — adopting the scale-lab corpus taxonomy
  (`specs/scale-lab-corpus.v1.yaml`, #3170) — that amplifies one Odù across N
  synthetic scopes and asserts perfcontract thresholds and backpressure
  failure shape. See [Layer 3](#layer-3-load-and-saturation-adopts-the-scale-lab-taxonomy).
- A **deterministic fault-injection harness** — extending the schedulereplay
  script vocabulary from orders to faults — that proves lease reclaim,
  retry, and idempotent replay actually converge. See
  [Layer 4](#layer-4-deterministic-fault-injection).
- A **public-corpus policy**: synthetic, seeded, schema-valid provider corpora
  are the shareable path; recorded provider cassettes stay maintainer-private.
  See [Public corpora without provider access](#public-corpora-without-provider-access).
- A **placement and contributor discipline**: where the new code lives, what it
  is allowed to depend on, and how a contributor proves an Odù.

Ifá **is not**:

- A new record/replay framework. The cassette codec, canonicalizer, and
  fact-emission seam already exist and are reused verbatim (see
  [Validated current-state inventory](#validated-current-state-inventory)).
- A source-obfuscation tool. A tree-sitter re-parse/byte-splice obfuscator is
  **technically feasible but explicitly shelved**; it is not part of the
  platform. See [Shelved: obfuscation and its trigger](#shelved-obfuscation-and-its-trigger).
- A unified test-platform *identity across all gates*. **Correction:** we
  assumed a prior ADR might already propose a unified test-platform identity
  binding every gate under one run context. That assumption was **refuted** —
  no such ADR was found (73 design docs under `docs/internal/design` searched);
  each gate (E2E #1225, shadow-read/write #1287/#1288,
  queue-substrate #1289, backup-restore #1290, secrets-IAM #1314) runs with its
  own run context, and #1225 mentions only a "synthetic run id" without a
  platform entity. Ifá therefore scopes itself to the conformance/determinism
  surface and does **not** claim to unify all gates. That remains an open
  question.
- A rewrite of the reducer, projector, or storage layer. Ifá consumes those
  through their existing seams.

---

## Validated current-state inventory

Legend: **exists** = already in the tree, reused as-is; **net-new** = does not
exist and must be built.

### Module and scale context

**Correction:** we assumed this repo was a single Go module. That was
**refuted** — it is a 5-module monorepo: the main module
`github.com/eshu-hq/eshu/go` plus four satellites
(`tools/golangci-lint-filelength/go.mod`, `sdk/go/collector/go.mod`,
`examples/collector-extensions/pagerduty/go.mod`,
`examples/collector-extensions/scorecard/go.mod`). Ifá lands in the **main
module**, so it shares the internal packages it needs without a new module
boundary.

Measured scale (all **exists**):

- 42 `cmd/` deployables, of which 21 are `collector-*` (confirmed).
- 9235 `.go` files (5218 non-test) across the module; `go/internal` has 103
  top-level package directories. Measured as of commit `5294fd1e` (2026-06-30)
  via `rg --files go -g '*.go'`; counts are point-in-time, not perpetual.

### Fact fan-in (exists — this is why the contract layer comes first)

- `facts.Envelope` is the single canonical intermediate type —
  `go/internal/facts/models.go:28-42` (fields `FactID`, `ScopeID`,
  `GenerationID`, `FactKind`, `StableFactKey`, `SchemaVersion`, `CollectorKind`,
  `FencingToken`, `SourceConfidence`, `ObservedAt`, `Payload`, `IsTombstone`,
  `SourceRef`), with a replay-safe `Clone()` at `models.go:49-57`.
- **Measurement basis (read this first).** Counts below are as of commit
  `5294fd1e` (2026-06-30) from `rg -l` import scans. Two bases matter and are
  reported explicitly: **exact** = files importing the package path itself;
  **tree** = also counting its subpackages. Non-test excludes `*_test.go`.
  Re-run to refresh; these are point-in-time, not perpetual.
- `facts.Envelope` is the single canonical intermediate type —
  `go/internal/facts/models.go:28-42`. Fan-in for `facts`: **1495 exact
  (651 non-test)** — one of the most-imported packages in the tree, which is why
  the contract layer comes first.
- **Correction (supersedes an earlier over-correction).** The `collector`
  fan-in was stated three inconsistent ways across drafts. Measured: `collector`
  is **109 exact (76 non-test)** but **1848 including its subpackage tree
  (1013 non-test)**. Both are real at different bases — the large figure is the
  tree count, the small one the exact-package count. Either way the conclusion
  holds: assert at the stable `facts.Envelope` seam, not inside collector code.
- Other fan-ins (exact / non-test): `telemetry` 608 / 510, `reducer` 297 / 124,
  `query` 198 / 77, `projector` 101 / 40; `replay` 13 exact / 69 tree.
- The companion `eshu-monorepo-split.md` uses the **non-test** basis above for
  extraction sizing; where a number there differs, prefer the non-test column.

### Fact-emission seam (exists — cleanly tappable)

- Collectors emit `<-chan facts.Envelope` — `go/internal/collector/service.go:52-56`.
- Ingestion batches the stream via `upsertStreamingFacts()` —
  `go/internal/storage/postgres/ingestion.go:239-241`.
- Single load gateway `FactStore.LoadFacts(ctx, ScopeGenerationWork) []facts.Envelope`
  — `go/internal/projector/service.go:44-46`, implemented over `fact_records`
  at `go/internal/storage/postgres/facts.go:98-103` (query at `facts.go:39-61`,
  ordered by `observed_at`).
- Facts can be **replayed into the reducer without re-running collectors**:
  `go/internal/recovery/replay.go:54-63` defines `StageProjector`,
  `StageReducer`, and `ReplayFilter` over `fact_work_items`.
- Work items are enqueued **in the same transaction** as facts —
  `ingestion.go:308-312` calls `queue.Enqueue()` inside the same
  `BeginTx/Commit` boundary that upserts facts (`ingestion.go:125-320`);
  `ProjectorQueue.Enqueue()` at `go/internal/storage/postgres/projector_queue.go:53-83`;
  INSERT into `fact_work_items` at
  `go/internal/storage/postgres/projector_queue_sql.go:6-30`.
- The reducer is a **separate deployable** (`go/cmd/reducer/main.go`) sharing
  the main module — confirmed. This is why Ifá can drive the reducer as a
  library dependency without recompiling collectors.

### Fact-kind and schema registry (exists — derive expectations, no manual want-list)

- `specs/fact-kind-registry.v1.yaml:1-20` declares families with
  `schema_version`, `reducer_domain`, `projection_hook`, `admission_hook`,
  `kinds`.
- Codegen: `scripts/generate-fact-kind-registry.sh:1-17` →
  `go run ./cmd/fact-kind-registry`.
- Schema-version dispatch: `specs/capability-matrix/fact-schema-version.v1.yaml:1-16`.

### Record/replay framework (exists — reused verbatim)

- **Versioned, fail-closed cassette codec.** `SchemaVersionV1 = "1"` is the
  only supported version — `go/internal/replay/cassette/format.go:19`;
  `ParseAndValidate` enforces it and returns an explicit
  `"unsupported schema_version %q (want %q)"` error on mismatch —
  `format.go:167-176`, `format.go:179-180`; fact-level validation requires a
  non-empty `schema_version` — `format.go:227-228`. Unknown versions are
  rejected, not defaulted or skipped.
- **Deterministic, idempotent canonicalizer.**
  `go/internal/replay/canonical.go` normalizes volatile keys
  (`observed_at` → sentinel), derives keys (`generation_id` from a `scope_id`
  hash), sorts arrays (`scopes`, `facts`) — `canonical.go:105-125`. Idempotent
  by contract — `canonical.go:180-181` — and proven so —
  `canonical_test.go:69-87` (byte-identical second pass),
  `canonical_test.go:115-151` (volatile → sentinel, derived-unique-per-scope, no
  collisions). `DerivedGenerationID(scopeID)` is exported and reusable for
  graph-level identity — `canonical.go:281-283`. This canonicalizer is the
  **basis for Ifá's cross-run graph comparator** — no new comparator is written.
- **Redaction is key-name based only; payloads are opaque.** `SecretKeys`
  replaces values by object-key match wherever the key appears —
  `canonical.go:49-66`; `OpaqueValueKeys` marks `payload` as verbatim so
  volatile/derived normalization does not descend into it, though secret
  redaction still does (still by key name) — `canonical.go:76-83`,
  `canonical_test.go:280-335`. There is no content/value-based obfuscation.
- **`cassette.Source` is single-threaded by design.**
  `go/internal/replay/cassette/source.go:29-30` documents "single-goroutine per
  `collector.Service`; not safe for concurrent `Next` calls"; no mutex/atomics,
  only unsynchronized `scopeIndex`/`drained` — `source.go:31-37`, `source.go:51-72`.
  Thread-safe alternatives exist elsewhere
  (`inputtape.RoundTripper` with a `sync.Mutex` — `roundtripper.go:59-62`;
  `schedulereplay.Source` "safe for concurrent use" — `source.go:22`), which
  confirms the lack of synchronization in `cassette.Source` is intentional.
  **A concurrent replay driver wrapping this is therefore net-new.**
- **Mode selection is CLI-flag, not env var.** `-mode` and `-cassette-file`
  parsed at startup across all collectors —
  `go/cmd/collector-kubernetes-live/main.go:72-95` (`launchModeCassette`,
  `launchModeRecord`, `launchModeLive`); same pattern in `collector-aws-cloud`,
  `collector-azure-cloud`, `collector-gcp-cloud`, `collector-grafana`,
  `collector-jira`. No env-var fallback.
- **Correction (git collector caveat):** the **git collector is live-only** —
  `go/cmd/collector-git/main.go` accepts only `-version` (lines 23-44) and
  imports no replay/cassette/inputtape package. So the "all collectors take
  `-mode`" statement is true for the recording-capable collectors but **not**
  git. Ifá's determinism harness must therefore source git-derived facts from
  the `fact_records`/`LoadFacts` replay path, not from a git cassette.

### Test/gate discipline (exists — templates Ifá reuses)

- **B-7 golden-corpus gate:** `go/cmd/golden-corpus-gate/main.go:1-88` with four
  acceptance buckets (drains, graph, query, timing);
  `go/internal/goldengate/snapshot.go:15-24`.
- **B-12 snapshot contract:** `testdata/golden/e2e-20repo-snapshot.json` —
  node/edge `CountRange` tolerances (lines 22-108), `required_correlations`
  rc-1..rc-33 with `EvidenceKinds`/`RequiredEdgeProperties`/
  `AllowedEdgePropertyValues` (lines 241-450+), drain assertions
  (`fact_work_items.residual_max:0`, `shared_projection_intents.nonterminal_max:0`),
  query shapes (MCP/HTTP/CLI).
- **Explicit, documented normalization** with a worked false-green fix:
  `EvidenceKinds` narrowing prevents an ArgoCD-only graph passing a
  "kustomize DEPLOYS_FROM" assertion — `snapshot.go:55-72`; rc-29 kustomize and
  rc-30 ansible pin evidence kinds and `source_tool` —
  snapshot lines 467-521; formatting at
  `go/internal/goldengate/evaluate.go:103-104`; semantics documented in
  `go/internal/goldengate/README.md:37-60`.
- **perfcontract with absolute thresholds and enforcement classes:**
  `EnforcementHermeticGate` vs `EnforcementOperatorGated` —
  `go/internal/perfcontract/contract.go:6-19`; `Threshold` binds doc phrase to
  numeric value/unit/enforcement — `contract.go:26-34`; `ContractThresholds()`
  — `contract.go:36-53`; B-5 context in `perfcontract/doc.go:1-31`.
- **Path-triggered gate registry:** `specs/ci-gates.v1.yaml:1-30` (single source
  of truth mapping changed paths → gates); selection via
  `scripts/dev/select-gates.sh:1-18` → `go run ./cmd/ci-gates select`;
  registry model in `go/internal/cigates/registry.go:1-100`
  (Tier/Category/Requirements).
- **Fail-closed coverage-gate template:**
  `scripts/verify-telemetry-coverage.sh:1-100` (X1-X4); registered in
  `specs/ci-gates.v1.yaml:543-560`; and the recorder/contract coverage gate
  already exists — `go/cmd/replay-coverage-gate/main.go:1-80` against
  `specs/replay-coverage-manifest.v1.yaml`, with reconciliation and
  advisory→blocking progression described in
  `go/internal/replaycoverage/README.md:1-161`.

### E2E design and the determinism gap (mixed)

- **Exists:** `docs/internal/design/1225-e2e-integration-suite.md` — parent E2E
  design with children #1230/#1227/#1226/#1229, covering proof architecture,
  corpus families, runtime/collector/reducer matrices, API/MCP/CLI readback
  parity, observability, evidence-packet schema, and failure policy.
- **Exists (cross-surface only):** `go/internal/mcp/answer_parity_test.go:6-31`
  proves HTTP/MCP/CLI agree on the canonical envelope for the **same** graph;
  `go/conformance/conformance_test.go` proves offline deterministic replay.
- **Correction — partial prior coverage exists (was overstated as a void).**
  `go/internal/replay/schedulereplay/scenario_test.go` already loads a committed
  cassette and asserts a **byte-identical canonical graph** across scripted
  orders (in-order/reverse/rotated/duplicate) *and* across **sequential
  `Workers:1` vs concurrent `Workers:4`** batch claims, with a teeth test that a
  deliberately order-sensitive applier must diverge (`scenario_test.go:39-153`);
  it is wired into the race gate (`specs/ci-gates.v1.yaml:1167-1172`).
  Determinism-under-concurrency is therefore **not** an empty void.
- **The actual gap Ifá fills.** That harness runs against an **in-memory graph +
  `ApplyCanonical`**, a single cassette, and a fixed 1-vs-4 pair. It does **not**
  exercise the **real reducer → graph/content projection pipeline**, an
  **N-worker matrix** (N ∈ {1,2,4,…}) over the **B-12 corpus and every Odù**, or
  a real graph backend. Ifá **generalizes the proven `schedulereplay` pattern**
  to the full pipeline and corpus — extending existing coverage, not inventing
  it. (Other worker tests such as `git_snapshot_scip_workers_test.go:21-290`
  assert only concurrency limits/ordering, not output identity.)

### Tree-sitter obfuscator feasibility (observed; tool is net-new and shelved)

The CST is fully walkable (`node.Kind()`/`IsNamed()`/`StartByte()`/`EndByte()` —
`go/internal/parser/shared/shared.go:92-130`; bindings `go-tree-sitter v0.25.0`)
but is discarded after one walk (`defer tree.Close()` — `engine.go:37-74`), and
there is **no source re-emitter** (`parser/README.md`). Across 24 tree-sitter +
11 other decoders (35 parsers — `registry_definitions.go:10-208`), a re-parse +
byte-splice + re-diff obfuscator is **feasible but PARTIAL** — it must ship its
own grammar bindings per language and re-parse from scratch. Shelved; see
[Shelved](#shelved-obfuscation-and-its-trigger).

---

## The layers

### Layer 1 — contract layer (first, given fact fan-in)

The contract layer comes first because the fact seam is the widest, most stable
surface in the tree: `facts.Envelope` has 1487 importers and `collector` has
1846 (corrected upward from 109). Asserting at `facts.Envelope` and
`FactStore.LoadFacts` lets Ifá make conformance claims without touching the
1846-file collector blast radius and without re-running collectors.

The contract layer defines an **Odù** as a conformance case whose expectations
are **derived, not hand-listed**:

- **Input:** a recorded cassette (versioned v1, fail-closed —
  `format.go:19`,`:179-180`) or a `fact_records` replay slice via
  `LoadFacts` (`facts.go:98-103`) for live-only sources such as the git
  collector (`collector-git/main.go`).
- **Canonical form:** produced by the existing canonicalizer
  (`canonical.go:105-125`, idempotent per `:180-181`). Ifá writes **no new
  canonicalizer**.
- **Expectations:** derived from the fact-kind registry
  (`specs/fact-kind-registry.v1.yaml`) and the B-12 snapshot contract
  (`testdata/golden/e2e-20repo-snapshot.json`) with the same explicit
  `EvidenceKinds`/`RequiredEdgeProperties` normalization that already prevents
  false greens (`snapshot.go:55-86`). No manual want-list.
- **Coverage:** enforced by the existing coverage-gate pattern
  (`replay-coverage-gate`, `replaycoverage/README.md:1-161`) so every
  conformance-relevant surface is either covered or explicitly exempt.

**Cross-wiring with the contract system (epic #4566).** The contract-system
work adds per-fact-kind typed payload structs and generated JSON Schemas
(#4567) and versioned fixture packs (#4572). Ifá does not build parallel
machinery for either:

- Expectation derivation validates Odù payloads against the #4567 JSON
  Schemas in addition to registry + B-12 normalization, so a schema-invalid
  payload fails conformance before it fails a reducer.
- **Two fixture tiers, one schema source — composition, not identity.**
  **Correction:** an earlier draft said "an Odù is a fixture-pack entry." What
  #4572 actually landed (`sdk/go/factschema/fixturepack`) is **kind-level**:
  one valid + one invalid payload per fact kind, proving payload-contract
  accept/fail-closed. An Odù is **scenario-level**: a replayable pipeline case
  spanning many kinds (cassette or `LoadFacts` slice) plus derived
  expectations and graph/query truth. The relationship is composition: an
  Odù's facts must validate against the fixture pack's schemas, an Odù may use
  pack payloads as building blocks, and both tiers derive from the single
  schema source in `sdk/go/factschema/schema/`. An external collector repo
  pins a fixture-pack version for payload conformance and runs Odù offline in
  its own CI. Two competing *schema sources* is the rejected outcome; two
  tiers with different granularity is the design.
- **The demo manifest is an Odù.** `specs/demo-first-answers.v1.yaml` — five
  questions with expected correlated answers, in flight under #4741 (first-run
  epic #4592), not yet in the tree — is structurally an Odù expectation set.
  Once it lands, P1 loads or validates it through the same derivation, so the
  public demo can never drift from conformance truth: the demo a newcomer sees
  is a permanently-green conformance case.

The contract layer is assertion + derivation only. It has no concurrency of its
own; it defines *what an identical graph means* so the determinism layer can
assert *that the graph stays identical under load*.

### Layer 2 — determinism under load (generalizes an existing pattern)

Ifá does not invent determinism-under-concurrency. `schedulereplay`
(`scenario_test.go:39-153`) already proves order-invariance and
`Workers:1`-vs-`Workers:4` invariance on a byte-identical canonical snapshot, in
the race gate. Ifá **generalizes that proven pattern** to what it does not
cover: the real reducer → graph/content projection pipeline, an N-worker matrix
(N ∈ {1,2,4,…}), and the full B-12 corpus / every Odù — rather than one cassette
against an in-memory graph.

Design:

1. **Concurrent replay driver (net-new).** `cassette.Source` is single-threaded
   by design and unsafe for concurrent `Next` (`source.go:29-37`). The driver
   is a new thread-safe wrapper — modeled on the existing safe wrappers
   (`inputtape.RoundTripper` mutex at `roundtripper.go:59-62`;
   `schedulereplay.Source` at `source.go:22`) — that feeds `facts.Envelope`
   into the ingestion → `fact_work_items` → reducer path
   (`ingestion.go:239-241`,`:308-312`; `reducer` as a library from the main
   module).
2. **Worker matrix.** Replay the same Odù at N ∈ {1, 2, 4, …}, each cell
   against a **fresh** Postgres + fresh graph backend (`docker compose down
   -v` between cells; mandatory, not a hygiene nicety — see below).
   **Correction (P3 measured; supersedes the collector `-mode` claim above).**
   The design's original text pointed at `-mode`/`-cassette-file` on
   recording-capable collectors (`collector-kubernetes-live/main.go:72-95`) as
   the matrix's own drive mechanism. P3 built something narrower that does
   not go through any collector binary at all: the matrix
   (`scripts/verify-ifa-determinism.sh`) is a loop over the `ifa drive
   -cassette <file> -workers N` verb (`go/cmd/ifa/drive.go`), which loads the
   cassette through `cassette.NewSource` (`drive.go:81-84`), wraps it in
   `concurrentreplay.NewSource`/`concurrentreplay.Driver{Workers: N}`
   (`drive.go:98-103`), and commits straight into a
   `postgres.IngestionStore` (`drive.go:94-96`) — the same durable commit
   boundary a live collector uses, reached without compiling or invoking
   collector `-mode` selection at all. Git-derived facts still come from the
   `LoadFacts` replay path where a live-only collector applies
   (`collector-git/main.go` is live-only); that half of the original claim is
   unaffected by the correction. **Fresh DB per cell is mandatory:** the
   ingestion upsert path resolves cross-run duplicates by fencing token via
   `ON CONFLICT (fact_id) DO UPDATE`
   (`go/internal/storage/postgres/facts_streaming.go:149,189`), so re-driving
   the identical cassette into a Postgres/graph pair that already holds that
   generation is a near-no-op — a determinism cell that reused a DB across N
   would pass vacuously, not because the write path is deterministic but
   because the second and third drives would have (almost) nothing left to
   do.
3. **Assertion.** After each N, canonicalize the projected graph and assert
   byte-identical output across all N. **Correction (P3 measured; the
   design's original claim here was FALSE).** The design said this step
   reuses "the existing canonicalizer" (`canonical.go:105-125`,`:180-181`)
   against "the projected graph." That canonicalizer never touched graph
   state — it normalizes fact-envelope/cassette JSON only — and no full-graph
   serializer existed anywhere in the tree before P3. P3 built one:
   `go/internal/ifa/graphdump` (`Canonicalize(ctx, Reader) ([]byte, error)`)
   reads every node and edge over a narrow `Reader` seam and produces a
   **content-addressed** byte form — edges reference their endpoints by the
   sha256 digest of each endpoint's own canonical node bytes
   (`go/internal/ifa/graphdump/README.md`), never by internal element ID or
   backend UID, so the comparison is genuinely order-independent and
   backend-ID-independent. It reuses rather than reimplements the
   canonicalization primitive: its only internal dependency is
   `internal/replay`'s shared canonical-JSON core
   (`CanonicalizeValue`/`CanonicalOptions`), not a second hand-rolled JSON
   walker (see anti-rewrite rule 1's amendment below). Measured proof this
   cannot pass vacuously (`go/internal/ifa/graphdump/README.md`'s Benchmark
   Evidence): an unmutated cassette produced byte-identical digests at
   N=1/N=4 on independent fresh stacks, and a single mutated payload value
   changed the digest.
3a. **Failure-path determinism.** The typed-decode contract (#4566) made
   malformed-fact dead-lettering a designed outcome, so the failure path can
   race invisibly under a graph-only comparison. **Correction (P3 measured;
   the design's `input_invalid` dead-letter illustration does not describe
   the mutation that actually reaches a comparable dead-letter row).** Two
   distinct malformed-fact mechanisms exist
   (`go/internal/ifa/mutate.go`'s `MutationKind`), and only one of them is
   durable and comparable:

   - A **missing required field** (`ifa mutate-cassette -kind
     missing-field`) passes every earlier admission gate untouched and is
     **PER-FACT QUARANTINED** once a canonical extractor or reducer handler
     decodes it — `go/internal/projector/factschema_quarantine.go` and its
     reducer twin (`go/internal/reducer/factschema_decode.go`) skip the one
     fact, increment a metric, and log a structured error, but the
     surrounding `fact_work_items` row still **succeeds**. This is
     metric-and-log-only: no dead-letter row is ever written, so there is
     nothing a `fact_work_items` query can compare across N. Measured
     empirically: driving a missing-field-mutated cassette produced 0
     `dead_letter` rows.
   - A **schema-major mismatch** (`ifa mutate-cassette -kind schema-major`)
     is caught earlier, at the **projector's own admission gate**
     (`go/internal/projector/schema_version_admission.go`'s
     `validateFactSchemaVersion`), before canonicalization even starts. That
     gate fails the **whole** projector work item for the scope/generation —
     not a per-fact skip — so the reducer's own follow-on materialization
     intents are never enqueued. The projector work item then dead-letters
     durably (`fact_work_items.status='dead_letter'`, `stage='projector'`),
     with `failure_class='projection_bug'` (measured empirically) — **not**
     the reducer's `input_invalid` this section originally illustrated.

   Step 3a's Odù therefore uses the schema-major mutation
   (`MutationSchemaMajor`, `go/internal/ifa/mutate.go:73-83`), the only one of
   the two that produces a durable, cross-N-comparable row. The matrix
   asserts the **identical dead-letter set**
   (`ifa.DeadLetterSetsEqual`, comparing `work_item_id`, `stage`, `domain`,
   and `failure_class` — `go/internal/ifa/dead_letters.go`) across all N,
   alongside the graph identity, scoped to the **durable** `fact_work_items`
   queue with its **own terminal condition**: `status NOT IN
   ('succeeded','superseded','dead_letter') = 0` — distinct from the B-12
   drain gate's `factWorkItemsResidualSQL`
   (`go/cmd/golden-corpus-gate/drains.go:27-29`), which deliberately counts a
   `dead_letter` row **as residual** ("a drained pipeline has no dead
   letters"). Polling the B-12 residual condition on a deliberately
   dead-lettering Odù would never reach zero; the failure-path leg needs its
   own terminal state that treats `dead_letter` as an expected outcome, not
   residual. Teeth test: a deliberately racy dead-letter path must be caught,
   mirroring the schedulereplay divergence test.
4. **Drain and timing** reuse the B-12 drain assertions
   (`fact_work_items.residual_max:0`) and perfcontract enforcement classes
   (`contract.go:6-19`) so a determinism run also proves the queue fully drained
   within the operator-gated or hermetic budget for its class. Confirmed:
   `scripts/verify-ifa-determinism.sh` already reports each cell's own
   wall-clock time in its PASS line (`N=<n> digest=<digest> wall=<seconds>s`),
   not only an aggregate — the per-cell instrumentation an operator-gated
   timing budget (P4) will need is already in place; P4 still owns setting an
   actual threshold from at least three measured runs (no number is added
   here).

This layer directly honors the repo's "Serialization Is Not A Fix" rule: it is
the gate that would *catch* a MERGE race being papered over by dropping worker
count, because a serialized workaround changes the worker-count matrix behavior
Ifá asserts on.

#### Layer 2 addendum — the single-generation finding and the tiered fixture strategy (P3 measured)

**Correction (P3 measured): varying N over the demo-org cassette alone proves
nothing about concurrency.** `testdata/cassettes/gcpcloud/supply-chain-demo.json`
is one scope, one generation, so `concurrentreplay.Driver` has exactly **one**
work unit for any `-workers N` — the worker-interleaving counter teeth
(`ifa_teeth_seq`, see below) was measured **byte-identical across every N** on
that fixture alone (digest
`48f30267f1c0773d137d14c64ae008e7fe0a5a39db481f524ac07d8ddcb09310` at
N=1/N=2/N=4, `go/internal/reducer/gcp_resource_materialization_teeth.go`'s doc
comment), meaning N was inert until a second work source was added. This
platform's original Layer 2 text (and the P3 phasing entry below) did not
anticipate this: a single-Odù determinism run proves **repeatability** (same
input, same output, every run), not that the matrix is actually sensitive to
worker-count interleaving.

P3 resolves this with a **tiered fixture strategy**, not a bigger single Odù:

- **Tier 1 — smoke.** The demo-org cassette alone, N ∈ {1, 2, 4}. Its
  single-generation nature is a **documented feature, not a bug**: it proves
  the matrix is repeatable and change-sensitive (a single mutated payload
  value changes the digest — the Benchmark Evidence Run A/B/C in
  `go/internal/ifa/graphdump/README.md`), cheaply and fast, before a
  contributor pays for a heavier multi-scope run.
- **Tier 2 — worker matrix.** The demo-org cassette **plus** a generated
  synth multi-scope cassette (`ifa synth-cassette`,
  `go/internal/synth/gcp.GenerateMultiScope`, K=8 distinct GCP projects,
  added in #4396 slice 6b) driven into the same cell. Each generated
  resource's `full_resource_name` embeds its own project's distinct
  `ProjectID`, so the K scopes are disjoint **by construction** at the
  payload-identity level — giving `concurrentreplay.Driver` 9 genuinely
  independent work units, so `-workers N` actually varies commit
  interleaving where Tier 1 cannot. This is the tier that is N-sensitive
  (measured: `ifa_teeth_seq` differs for 591/622, 558/622, and 488/622
  `CloudResource` nodes across the N=1/N=2, N=1/N=4, and N=2/N=4 pairs
  respectively under `--teeth`).
- **Tier 3 — load.** P5's load/saturation harness (Layer 3) reuses the same
  synth-multiscope seam at scale-lab corpus size rather than inventing a new
  amplification mechanism (see the P5 amplifier landmine in Layer 3 below).

**Rule:** P4's (#4397) advisory-to-blocking flip for the Ifá gate MUST require
the N-sensitive Tier 2 run, not Tier 1 alone — a gate that only ever runs the
single-generation demo-org cassette can stay green forever without ever
exercising a genuine worker-count race, which is exactly the false confidence
this platform exists to prevent.

**Teeth taxonomy (build tag `ifadeterminismteeth`).** The matrix's negative
control stamps two build-tag-gated properties onto each GCP `CloudResource`
row (`go/internal/reducer/gcp_resource_materialization_teeth.go`,
`go/internal/storage/cypher/cloud_resource_node_writer_teeth.go`), read by
`scripts/verify-ifa-determinism.sh --teeth`:

- `r.ifa_teeth_write_order` — wall-clock nanoseconds
  (`time.Now().UnixNano()`). Two independent process starts are vanishingly
  unlikely to collide, so this is the **guaranteed-red floor**: `--teeth` can
  never flake green for lack of a divergent signal, on any fixture.
- `r.ifa_teeth_seq` — a process-global monotonic counter. This is the
  **interleaving-sensitivity signal**, and it is meaningful only on a
  multi-generation/multi-scope fixture: on the single-generation demo-org
  cassette alone it was measured INERT (identical across every N); on the
  Tier 2 multi-scope fixture it is genuinely interleaving-sensitive (the
  591/622, 558/622, 488/622 measurement above,
  `go/internal/reducer/README.md`'s teeth section).

Both stamps compile to a zero-cost, zero-behavior no-op in every normal, CI,
and production build (`!ifadeterminismteeth`: empty-string const concat plus
an inlined no-op function, confirmed by
`TestCanonicalCloudResourceUpsertCypherExcludesTeethClauseByDefault`); only
`scripts/verify-ifa-determinism.sh --teeth` ever links the real stamp.

### Layer 3 — load and saturation (adopts the scale-lab taxonomy)

Layer 2 varies worker count to catch race defects; it never varies **load**.
Layer 3 closes that gap without inventing a taxonomy, because the taxonomy
already exists: `specs/scale-lab-corpus.v1.yaml` (issue #3170, now CLOSED;
the spec's own `gate_status: proposed` field is stale and gets reconciled
when Layer 3 adopts it) defines the smoke/small/medium/large/pathological
corpus slots and the measurement contract (fact rows/sec, queue-claim p95,
reducer drain, graph-write p95, API/MCP p95). Ifá **adopts** that spec as its
load vocabulary; accepting #3170 becomes a Layer 3 dependency, not a parallel
effort.

Design:

1. **Corpus amplifier (net-new, small).** Replay one Odù across N synthetic
   scopes by deterministically rewriting `scope_id` and `stable_fact_key`
   (seed-indexed), following the same derived-identity pattern the
   canonicalizer already exports (`DerivedGenerationID`,
   `canonical.go:281-283`). One recorded or synthetic 1-repo Odù becomes a
   500-scope load run with zero new recordings and zero credentials. Amplified
   scopes reuse the Layer 2 driver; the fan-out is data, not new concurrency
   machinery.

   **Landmine identified in P3 (correction before P5 builds this).** Generic
   `scope_id`/`stable_fact_key` rewriting, as described above, is
   **determinism-unsafe** for cloud-resource families and must not be built
   as-is. Graph nodes key on **payload identity**, not scope: the GCP
   `CloudResource` node UID is derived from the resource's own
   `full_resource_name` (`uid := cloudResourceUID(projectID, location,
   assetType, fullResourceName)`,
   `go/internal/reducer/gcp_resource_materialization.go:304`), and the row's
   `source_fact_id` is stamped from the incoming fact envelope
   (`go/internal/reducer/gcp_resource_materialization.go:321`) as a plain
   last-writer-wins `SET`. If an amplifier rewrites only `scope_id`/
   `stable_fact_key` and leaves the payload (`full_resource_name`, etc.)
   untouched, K amplified scopes carrying the same underlying payload MERGE
   onto the identical node UID, and whichever scope's write commits last wins
   `source_fact_id` — legitimately order-dependent, which the determinism
   matrix would then report as a **false red** caused by the load-generation
   mechanism itself, not a real concurrency defect. #4396 slice 6b's
   multi-scope fixture (`go/internal/synth/gcp.GenerateMultiScope`) avoids
   this the correct way: every generated resource's `full_resource_name`
   embeds its own scope's distinct `ProjectID`
   (`go/internal/synth/gcp/README.md`, "Multi-scope generation"), so the K
   scopes are disjoint **by construction** at the payload-identity level, not
   only at the `scope_id` level. P5's amplifier MUST produce disjoint payload
   identities per amplified scope (family-aware, not a generic
   `scope_id`/`stable_fact_key` rewrite); where a family-native synth
   generator already exists (GCP today), P5 should prefer amplifying through
   that generator over a generic post-hoc rewrite. The genuinely different
   question — cross-scope contention where two scopes legitimately claim the
   SAME real-world resource UID — is a product-truth question, deferred to
   issue #5007, not something this amplifier note tries to resolve.
2. **Throughput Odù.** Run an amplified Odù at a named scale slot and assert
   the perfcontract thresholds for that slot's class. Smoke/small slots run
   hermetic (`EnforcementHermeticGate`); medium and above run operator-gated
   on consistent hardware (`EnforcementOperatorGated`, `contract.go:6-19`) —
   the same split perfcontract already defines, so no second perf contract
   appears (anti-rewrite rule 5).
3. **Saturation Odù.** Deliberately drive past the graph-write permit pools
   (`ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT` /
   `..._SEMANTIC_MAX_IN_FLIGHT`, `go/internal/graphbackpressure`) and assert
   the **failure shape**, not just survival: backpressure engages (observer
   wait signal fires), work retries with backoff, nothing dead-letters
   spuriously, and the queue drains to the B-12 residual assertions after
   pressure releases. This regression-proofs the #3560 failure class
   (backend slowness dead-lettering recoverable work) as a permanent gate
   instead of a fixed incident.

Net-new here is the amplifier and the saturation scenario runner. Slots,
metrics, thresholds, enforcement classes, drain assertions, and the replay
driver are all adopted.

### Layer 4 — deterministic fault injection

The determinism matrix varies scheduling; nothing varies **failure**. The
platform's recovery story rests on three mechanisms — lease expiry reclaim,
retry with backoff, and idempotent replay (`MERGE` / `ON CONFLICT`) — plus
queue-replay recovery endpoints (`go/internal/runtime/recovery_handler.go`).
Today those are proven piecemeal by unit tests; no gate proves they converge
end to end under injected failure.

Design:

1. **Fault scripts extend the schedulereplay vocabulary.** `schedulereplay`
   already scripts *orders* (in-order/reverse/rotated/duplicate,
   `scenario_test.go:39-153`). Layer 4 adds *faults* to the same script model:
   `kill-worker-after-claim(n)`, `expire-lease-mid-handler`,
   `fail-graph-write-once-then-succeed`,
   `restart-backend-between-phase-groups`. Scripts are data; a fault run is
   replayable byte-for-byte like any other Odù run.
2. **Injection at existing seams only.** Faults inject where decorators
   already wrap: the graph executor seam (precedent:
   `BackpressureExecutor` and `RetryingExecutor` wrap the same interface),
   the claim/heartbeat store interface, and driver-level worker lifecycle.
   No fault hooks inside handlers or collectors — the anti-rewrite placement
   rule holds.
3. **The assertion is unchanged.** After the fault script completes and the
   queue drains, canonicalize and compare against the fault-free run of the
   same Odù: byte-identical canonical graph, B-12 drain assertions green, and
   dead letters only where the script *says* a terminal failure was injected.
   Layers 1–2 already define "still correct"; Layer 4 is another axis on the
   same matrix, which is what keeps it cheap.

---

## Public corpora without provider access

The platform goal includes contributors who have **no cloud account at all**.
Cassette replay alone does not deliver that: someone must record against real
Azure/AWS/GCP once, and sharing recordings publicly is blocked by the
validated redaction limitation — key-name-based only, payloads opaque
(`canonical.go:49-83`).

Decision:

- **Synthetic provider corpora are the primary public mechanism.** A
  deterministic, seeded generator per provider family emits v1 cassettes
  whose payloads are schema-valid against the #4567 payload schemas. Nothing
  sensitive exists to redact, so the corpora are committable and shareable by
  construction. Generators are boring by design: seed in, cassette out,
  byte-identical for the same seed.
- **Recorded provider cassettes stay maintainer-private**, used for parity
  runs that confirm the synthetic corpora still reflect real provider shapes
  (the `parity/` harness precedent).
- **The obfuscator stays shelved with a narrowed trigger:** un-shelve only
  when a bug class is demonstrated that synthetic corpora provably cannot
  reproduce *and* sharing the recorded corpus requires value-level redaction
  that key-name redaction cannot provide. Both conditions, documented, not
  either.

---

## Placement

- **Repo:** the **eshu** main module (`github.com/eshu-hq/eshu/go`). Refuted the
  single-module assumption; Ifá is not a satellite module.
- **Binary + package:** `go/cmd/ifa` (the deployable/CLI) + `go/internal/ifa`
  (the library), matching the existing **gate pattern** used by
  `go/cmd/golden-corpus-gate`, `go/cmd/replay-coverage-gate`, and
  `go/cmd/ci-gates`. Ifá registers in the path-triggered gate registry
  `specs/ci-gates.v1.yaml` with an appropriate Tier/Category
  (`registry.go:1-100`), advisory first then blocking, mirroring
  `replaycoverage` progression.
- **Contract-only dependencies.** `internal/ifa` depends on the **stable
  contract seams**, not internals:
  - `internal/facts` (Envelope) — the intermediate contract.
  - `internal/replay` (cassette codec, canonicalizer) — reused verbatim.
  - `internal/projector` (`FactStore.LoadFacts`) and `internal/reducer` as a
    library, and `internal/storage/postgres` for the replay slice.
  - `internal/perfcontract` and `internal/goldengate` for thresholds and
    snapshot semantics.
  It must **not** import collector internals (1846-file blast radius) or parser
  internals; it observes their output through `facts.Envelope`.

---

## Contributor contract

Two verbs, both grounded in existing machinery.

### `make prove`

A single credential-free entry point (registered in `specs/ci-gates.v1.yaml`,
selected by `scripts/dev/select-gates.sh`) that:

1. Runs the affected Odù set for changed paths (path-triggered, like every other
   gate — `ci-gates.v1.yaml`, `select-gates.sh`).
2. Executes the determinism matrix (Layer 2) for those Odù.
3. Reconciles coverage against the manifest so a new fact kind or surface cannot
   land uncovered (`replaycoverage/README.md:1-161`).
4. Emits the same kind of deterministic dashboard/report the existing
   coverage/golden gates emit.

Two harness-level policies land with `make prove` (P4):

- **Flake policy — no retry-to-green, ever.** A nondeterministic failure IS a
  determinism defect, the exact class this platform exists to catch. The
  response is quarantine-by-issue and root-cause, never an automatic retry
  that turns a red run green. This is the Serialization-Is-Not-A-Fix doctrine
  applied to the harness itself.
- **Prove-latency budget.** The common path of `make prove` carries a measured
  wall-time budget (set from at least three measured runs at P4,
  operator-gated per `perfcontract` enforcement classes). Prove-latency
  regressions are bugs to root-cause, not accepted costs — a slow prove path
  is how test frameworks rot into being skipped.

`make prove` is the local mirror of the CI gate; CI stays authoritative
(consistent with `make pre-pr`).

### Drop-an-Odù

A contributor adds a conformance case by dropping a cassette (or a `LoadFacts`
replay descriptor) and letting expectations derive. The path mirrors the
documented "add a language" 7-step checklist model
(`go/internal/parser/AGENTS.md:107-120`): declare the input, register it, add no
hand-written want-list (expectations derive from the fact-kind registry + B-12
snapshot normalization), and `make prove` validates coverage and determinism.
The cassette must be v1 (fail-closed — `format.go:179-180`) and must carry only
key-name-redactable secrets, because redaction is key-name based and payloads
are opaque (`canonical.go:49-83`) — contributors must not rely on value-content
masking that does not exist.

---

## Shelved: obfuscation and its trigger

A tree-sitter **re-parse + byte-splice leaf tokens + re-parse-and-diff CST
histogram** obfuscator was evaluated and is **shelved**, not adopted.

Why it is feasible (validated): the CST exposes `Kind()`/`StartByte()`/
`EndByte()` (`shared/shared.go:92-130`), byte ranges are exact, and a standalone
tool needs no eshu changes.

Why it is shelved (validated PARTIAL verdict): it is **not zero-cost**. Eshu
discards the CST after one walk (`rust/parser.go:14-92`, `engine.go:37-74`) and
has **no source re-emitter** (`parser/README.md:1-50`), so any obfuscator must
carry its own tree-sitter grammar bindings for each of the 24 tree-sitter
languages (`registry_definitions.go:10-208`) and re-parse independently. That is
a separate language-aware binary with its own grammar infrastructure — real
maintenance surface for a capability the platform does not currently need.

**Trigger to un-shelve (narrowed).** The original trigger was "a requirement
to share corpora whose payloads cannot be redacted by key name alone." The
public-corpus decision above meets most of that requirement more cheaply with
synthetic generation, so the trigger narrows to **both** conditions holding,
documented: (1) a bug class is demonstrated that synthetic corpora provably
cannot reproduce and only a recorded corpus exhibits, **and** (2) sharing that
recorded corpus requires value-level redaction that key-name redaction
(`canonical.go:49-66`) provably cannot provide. Until both are real,
obfuscation stays out of Ifá.

---

## Anti-rewrite rules

These are hard constraints. Ifá reuses; it does not reimplement.

1. **Do not write a new canonicalizer.** Use `internal/replay/canonical.go`
   (idempotent, `:180-181`) for the fact-envelope/cassette canonical form —
   the form the golden-corpus gate, the B-12 snapshot, and Odù expectations
   all normalize against. A second FACT-ENVELOPE canonicalizer would drift
   from that normalization and create false greens. **Amendment (P3): this
   rule governs the fact-envelope/cassette canonical form specifically, not
   "any graph-shaped output."** A graph-STATE dumper is legitimate net-new
   work when no full-graph serializer exists to reuse:
   `go/internal/ifa/graphdump` canonicalizes live NornicDB/Neo4j reads
   (content-addressed, node/edge properties, no internal IDs) by reusing
   `internal/replay`'s shared canonical-JSON core rather than forking a
   second JSON-canonicalization implementation. The constraint this rule
   actually protects — one canonical-JSON core, reused everywhere a canonical
   byte form is needed — holds; only the surface (fact envelopes vs. live
   graph reads) differs, because the design's original text conflated the
   two.
2. **Do not write a new cassette codec.** Use the v1 fail-closed codec
   (`format.go:19`,`:179-180`). Do not add a v2 without a migration and gate.
3. **Do not fork the fact seam.** Assert on `facts.Envelope`
   (`models.go:28-42`) and `FactStore.LoadFacts` (`facts.go:98-103`); do not
   reach into collector internals (1846-file fan-in).
4. **Do not hand-maintain want-lists.** Derive expectations from the fact-kind
   registry (`fact-kind-registry.v1.yaml`) and B-12 snapshot with existing
   `EvidenceKinds` normalization (`snapshot.go:55-86`).
5. **Do not invent a second perf contract.** Use `perfcontract` thresholds and
   enforcement classes (`contract.go:6-53`).
6. **Do not add a new coverage-gate framework.** Extend the existing
   `replaycoverage` reconciliation and advisory→blocking progression
   (`replaycoverage/README.md:1-161`).
7. **Do not claim a unified test-platform identity.** Refuted — no such ADR
   exists; keep Ifá scoped to conformance/determinism and leave gate-identity
   unification as an open question.
8. **Do not fix a determinism failure by lowering worker count.** Per repo rule,
   serialization is not a fix; the determinism matrix exists to catch exactly
   that.
9. **Do not invent a second load-testing taxonomy.** Adopt the scale-lab
   corpus slots and measurement contract (`specs/scale-lab-corpus.v1.yaml`,
   #3170). If a slot or metric is missing, amend that spec, not Ifá.
10. **Do not fork the schema source.** The kind-level fixture pack
    (`sdk/go/factschema/fixturepack`, #4572) and scenario-level Odù are two
    tiers by design — but both MUST derive from the single schema source in
    `sdk/go/factschema/schema/`. An Odù validates against those schemas and
    may compose pack payloads; it never carries its own divergent schema copy.
11. **Do not commit recorded provider cassettes to public corpora.** Redaction
    is key-name-only with opaque payloads (`canonical.go:49-83`); the public
    path is synthetic generation. Recorded cassettes are maintainer-private
    parity inputs.

---

## Phasing

**P0 — contract layer skeleton (no new runtime behavior).**
Create `go/cmd/ifa` + `go/internal/ifa` with contract-only deps. Define the Odù
type over `facts.Envelope` and `LoadFacts`; wire the existing canonicalizer as
the comparator. Register an **advisory** gate in `specs/ci-gates.v1.yaml`.
Evidence: package builds, one Odù canonicalizes idempotently (reusing
`canonical_test.go` patterns), gate selected by `select-gates.sh` on changed
paths.

**P1 — derived expectations + coverage reconciliation + contract-system wiring.**
Derive expectations from `fact-kind-registry.v1.yaml` and the B-12 snapshot with
`EvidenceKinds`/`RequiredEdgeProperties` normalization, and validate Odù
payloads against the #4567 JSON Schemas where they exist (schema absent =
registry-only derivation, flagged in the report, so P1 does not block on
epic #4566 completing). Odù are scenario-level cases that VALIDATE against the
kind-level fixture pack's shared schema source (#4572) — two tiers, one schema
source, per the corrected cross-wiring above. Reconcile Odù coverage against a
manifest using the
`replaycoverage` pattern. Evidence: a new fact kind without an Odù reports
uncovered; false-green case (kustomize vs ArgoCD) correctly fails; a
schema-invalid payload fails conformance.

**P1 terminal proof — typed round-trip Odù (#4804).** Payload-schema
conformance (above) proves a payload's *shape* is valid JSON Schema; it does
not prove the contract system's typed `sdk/go/factschema` structs preserve
every field a collector actually emits. `odu:demo-org-roundtrip`
(`go/internal/ifa/roundtrip.go`) closes that gap for the GCP fact family
(`gcp_cloud_resource`, `gcp_cloud_relationship`, `gcp_collection_warning`,
`gcp_dns_record`, `gcp_iam_policy_observation`): it carries every fact the
demo-org synthetic GCP cassette (`go/internal/synth/gcp`) generates, replayed
through the production `cassette.Source` seam rather than a hand-built
mirror, and `RoundTripTypedPayloads` asserts each fact's typed
Encode→Decode→re-Encode round trip is canonical-byte-identical to the
original payload. A payload that fails typed decode surfaces the same
classified `*factschema.DecodeError` a reducer handler would act on; a
payload that decodes but loses or reshapes a field (for example an unmodeled
key `gcpv1.DNSRecord` silently drops on re-encode, since it carries no
`Attributes` pass-through remainder) reports a canonical-byte mismatch naming
the fact. This is the "contract system alive" proof the epic's W6 milestone
targets: the typed struct layer, not only its JSON Schema, is provably
faithful for one full fact family end to end.

**P2 — concurrent replay driver (net-new).**
Build the thread-safe wrapper around `cassette.Source` (modeled on
`inputtape.RoundTripper`/`schedulereplay.Source`), feeding ingestion → reducer.
Add the git-collector `LoadFacts` replay path (live-only —
`collector-git/main.go`). Evidence: driver passes `-race`; same Odù drains
(`fact_work_items.residual_max:0`) at N=1.

**P3 — determinism matrix + timing (determinism coverage generalized;
shipped — see the Layer 2 addendum above for the measured tiered-fixture
correction).** Run each Odù at N ∈ {1, 2, 4} via `ifa drive -cassette <file>
-workers N` (`go/cmd/ifa/drive.go`) against a fresh Postgres + graph backend
per cell, and assert byte-identical canonical graph across N using the
net-new `go/internal/ifa/graphdump` content-addressed graph dumper (not the
fact-envelope canonicalizer — see the corrected Layer 2 item 3 above); enforce
drain via the existing B-12 gate. Also assert failure-path determinism: a
schema-major-mutated Odù (`ifa mutate-cassette -kind schema-major`) produces
the identical durable dead-letter set across all N against its own terminal
condition (step 3a, corrected above). Evidence (recorded 2026-07-09,
`go/internal/ifa/graphdump/README.md` and `go/internal/reducer/README.md`):
Tier 1 (demo-org alone) matrix green, digest
`f692b33c72b99bb2ca44f25ca08804be425c96324186acd48995a6d59ccbc873` at N=1/2/4;
Tier 2 (demo-org + 8-project synth-multiscope cassette, slice 6b) matrix
green, digest
`e3b183cb9e20fba3c3a3bb0690681502fc444263bc4fc9cd883259ef4ddf8682` at N=1/2/4,
proving the 9-work-unit combination is genuinely N-sensitive: the SAME
cassette pair under `-tags ifadeterminismteeth` diverges across all three
cells (`TEETH: CAUGHT`) while the normal build stays green, catching a
deliberately non-idempotent write; the malformed-fact leg
(`scripts/verify-ifa-dead-letter-determinism.sh`) catches a deliberately racy
dead-letter path the same way (regression tests first, per this platform's
Evidence Rules).

**P4 — `make prove`, drop-an-Odù docs, advisory→blocking. Landed (#4397).**
`make prove` (`scripts/dev/prove.sh`) is the credential-free entry point: it
runs the contract-layer test, the hermetic structural mirrors, and the coverage
reconcile always, then runs the real Docker determinism matrix when Docker is
present and defers loudly otherwise (the `trivy-fs-local.sh` pattern — never a
silent pass). The flake policy (no retry-to-green) is stated in the driver and
enforced by zero retry logic in the CI workflow. The prove-latency budget for
the common credential-free path is a `perfcontract.Threshold`
(`EnforcementOperatorGated`): measured at 4s/4s/4s over three runs, recorded as
5s (worst + ~25% headroom). The drop-an-Odù checklist lives in
`go/internal/ifa/AGENTS.md`, mirroring the parser 7-step model. The three Ifá
gates (`ifa-contract-layer`, `ifa-determinism`, `ifa-dead-letter-matrix`) are
flipped to `blocking: true` in `specs/ci-gates.v1.yaml`; the two determinism-
matrix gates run their real Docker matrix per-PR in
`.github/workflows/ifa-determinism-gate.yml` (mirroring golden-corpus-gate),
satisfying the line-517 rule that the blocking flip require the N-sensitive
Tier-2 run. Evidence: `make prove` runs credential-free and mirrors CI; the docs
build gate passes; the blocking flip and CI wiring are recorded in
`specs/ci-gates.v1.yaml`.

**P5 — load and saturation (Layer 3).**
Build the corpus amplifier and the throughput/saturation Odù classes over the
P2 driver. Depends on scale-lab corpus acceptance (#3170) for slot definitions
and on P3 for the determinism baseline. Smoke/small slots register hermetic;
medium+ register operator-gated. Evidence: one amplified Odù at a small slot
holds its perfcontract thresholds; the saturation Odù reproduces the #3560
failure shape against a permit-starved backend and drains clean after release
(regression test first for the dead-letter-flood assertion).

**P6 — deterministic fault injection (Layer 4).**
Add the fault-script schema and injection decorators at the executor, claim
store, and worker-lifecycle seams. Evidence: each scripted fault class runs to
a byte-identical canonical graph versus the fault-free baseline; a
deliberately non-idempotent write under `expire-lease-mid-handler` is caught
(teeth test, mirroring the schedulereplay divergence test); `-race` clean.

Synthetic provider corpus generators land incrementally from P1 onward (each
generator is an Odù source, not a phase of its own); the first generator ships
with P1 so derivation has a fully synthetic case from the start.

---

## Open questions

- Unified gate identity across the independent gates is unproposed — a separate
  ADR if wanted, not an Ifá feature.
- **Resolved for P3:** the worker-count matrix runs N ∈ {1, 2, 4}
  (`scripts/verify-ifa-determinism.sh`'s `worker_counts=(1 2 4)`), matching
  the design's own illustrative "N ∈ {1, 2, 4, …}" set. **Resolved for P4
  (#4397):** the `make prove` common-path timing budget is measured at 4s/4s/4s
  over three runs and recorded as a 5s `perfcontract.Threshold`
  (`EnforcementOperatorGated`, worst + ~25% headroom). The Docker determinism
  matrix carries its own per-cell baselines and is reported informationally, not
  budgeted, because it varies by machine and Docker state.
- Layer 3 depends on the scale-lab corpus spec (#3170) moving from
  `gate_status: proposed` to accepted; if that spec changes shape during
  acceptance, the Layer 3 slot bindings follow it (anti-rewrite rule 9).
- Saturation budgets (how far past the permit pool, for how long) need
  operator-gated calibration before the saturation Odù can be blocking.
- **Deferred, filed during P3:** cross-scope same-uid contention (two scopes
  legitimately claiming the same real-world resource UID) is a product-truth
  question, not a P5 amplifier bug — issue
  [#5007](https://github.com/eshu-hq/eshu/issues/5007). Nightly
  B-12-corpus-scale determinism composition via `FactSliceSource` (N ∈
  {1, 4}) is deferred to issue
  [#5008](https://github.com/eshu-hq/eshu/issues/5008). An audit of
  `graphdump`'s canonicalizer for streaming/pagination behavior, needed
  before medium+ scale-lab slots exercise it at corpus scale, is deferred to
  issue [#5009](https://github.com/eshu-hq/eshu/issues/5009).
