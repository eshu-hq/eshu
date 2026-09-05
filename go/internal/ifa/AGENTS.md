# AGENTS.md - internal/ifa guidance

## Read first

1. `README.md` - package purpose, boundaries, and P1 derivation/coverage.
2. `doc.go` - godoc contract.
3. `odu.go` - Odù contract-layer canonicalization.
4. `expectations.go`, `evidence.go`, `schema.go`, `coverage.go` - the P1
   derivation join and coverage reconciliation.
5. `roundtrip.go` - `RoundTripTypedPayloads` and `demoOrgRoundtripOdu`, the P1
   terminal typed-payload round-trip proof (issue #4804).
6. `mutate.go`, `dead_letters.go` - the P3 failure-path-determinism fixture
   generator and dead-letter-set comparator (ADR step 3a, issue #4396).
7. `go/internal/replay/AGENTS.md` - canonicalization invariants reused here.
8. `go/internal/replaycoverage/AGENTS.md` - the coverage machinery Ifá reuses
   wholesale.
9. `go/internal/synth/gcp/AGENTS.md` - the synthetic GCP corpus generator
   `demoOrgRoundtripOdu` depends on.
10. `go/internal/ifa/materializededges/AGENTS.md` (#5351, split out #6053) -
    the `materialized_edges:<domain>` exhaustiveness gate: binds an Odù
    expectation to a reducer-materialized graph edge family so a
    materialization silently ceasing to produce an edge family is caught.
    `*_family_odu.go`/`*_family_catalog.go` in this package still seed each
    family's fixture (`sql_relationship_odu.go` was the first); the guards
    and dispatch that consume them moved to that sibling package.

## Invariants

- Ifá observes contract seams; it does not import collector or parser internals
  directly. `relationships.DiscoverEvidence` and
  `conformance.ValidatePayloadSchemas` are the only two derivation seams into
  that layer, and both are called with an Odù's own facts, never a hand-built
  substitute.
- Canonical comparison must reuse `go/internal/replay.Canonicalize` /
  `CanonicalizeValue`; do not add a second canonicalizer.
- Odù facts are treated as immutable inputs. Clone envelopes before rendering so
  caller-owned payload maps are not shared into comparison work.
- Keep this package deterministic: no wall-clock time, randomness, network, or
  storage side effects inside canonicalization or derivation.
- Expectations are derived, never hand-listed. Do not add a static
  fact-kind-to-evidence-kind table; run the real extractor. Do not string-match
  a read surface to a query-shape key; read the replay-coverage manifest's
  `read_surface:*` rows.
- Reuse `go/internal/replaycoverage` wholesale for coverage bookkeeping
  (`Manifest`/`LoadManifest`, `Reconcile`, `Findings`, `BuildReport`,
  `ValidateRequiredProofGates`, the `Resolver` interface) and
  `goldengate.RequiredCorrelation` verbatim. Do not build a second coverage
  framework.
- An Odù↔required-correlation binding in `specs/ifa-coverage-manifest.v1.yaml`
  must be validated via `EvidenceSatisfies(rc, DiscoveredEvidence(odu))`, never
  asserted by name alone; see `coverage_falsegreen_test.go` for the required
  deliberate-break proof before trusting a new binding.
- Only seed a coverage-manifest row once it is genuinely green (the C-1
  seed-only-green-rows philosophy); an aspirational binding stays on the
  uncovered worklist.
- The `ifa-contract-layer`, `ifa-determinism`, and `ifa-dead-letter-matrix` CI
  gates are CI-blocking as of P4 (#4397); do not revert them to advisory. The two
  determinism-matrix gates run their real Docker matrix per-PR in
  `.github/workflows/ifa-determinism-gate.yml`; keep their `local.command` in
  `specs/ci-gates.v1.yaml` pointed at the hermetic `test-verify-*.sh` mirror (not
  the Docker script) so `make pre-pr` stays credential-free. `make prove` reaches
  past the registry to run the real Docker matrix locally when Docker is present,
  and defers loudly (never a silent pass) when it is not.
- `MutateCassette` never mutates its `src` argument; it always returns a
  cloned `cassette.File` (`cloneCassette`, a JSON round trip). Do not change it
  to mutate in place — callers pass the same in-memory cassette across
  multiple `MutateCassette` calls in tests and expect the source untouched.
- `MutateCassette`'s two `MutationKind` values reach very different runtime
  outcomes for a fact kind core registers a schema version for — see
  `mutate.go`'s `MutationKind` doc comment (proven empirically, not just by
  reading the decode seam, via `scripts/verify-ifa-dead-letter-determinism.sh`)
  before assuming either kind's failure_class or which stage's dead-letter
  path fires.
- `DeadLetterSetsEqual` compares on every field of `DeadLetterRecord`,
  including `FailureClass`. Do not narrow it to `WorkItemID`-only equality —
  the ADR's step 3a teeth test requires catching a divergent `failure_class`
  on an otherwise-matching work item.
- `reducer.MaterializedEdgeFamilies()` and
  `reducer.DirectMaterializedEdgeFamilies()` are the ONLY enumeration sources
  for `materialized_edges:<domain>` surfaces, and BOTH must be reconciled. Do
  not hand-list families in `materializededges/materialized_edges.go` or in
  either ledger file; a family must come from one of those two functions. The
  first is locked to `allProjectionDomains` by a reducer-package test. The
  second is held to the Cypher its ports actually execute by
  `TestDirectMaterializedEdgePortsMatchTheExecutedCypher` — every reducer port
  reaching a relationship MERGE must be a declared family. Do NOT re-derive
  either list from port names: the guard that did so missed six ports that
  merge relationships under names carrying no `Edges` suffix (#6181).
- Reconciling only ONE half is the blindness #6181 reported: a family whose
  enumeration or ledger half is never read produces no row, no finding and no
  output, so the gate reports green without knowing it exists. Any caller that
  reconciles the gate, or that holds every waiver to a rule, MUST load both
  halves through `materializededges.LoadMaterializedEdgeLedger` rather than
  calling `LoadMaterializedEdgeWaivers` on a single path.
  Two fixtures are the exception and are the only ones:
  `materialized_edges_falsegreen_test.go` and
  `materialized_edges_waiver_granularity_test.go` each build a
  `RunMaterializedEdgeCoverage` input scoped to
  `reducer.MaterializedEdgeFamilies()`, so handing them the direct half would
  add waivers for families that run does not enumerate. A single-path read is
  correct only where the families passed beside it are the same half's, and
  the call site has to say so. Everything else goes through the loader.
- A `materialized_edges:<family>` coverage row is not exhaustively covered
  until BOTH its `baseline` (proof_gate `ifa-determinism`) and `fault`
  (proof_gate `ifa-fault-injection`) scenario_type rows resolve covered.
  `sql_relationships` additionally requires `delta_tombstone`, proven live by
  `ifa-determinism` after its baseline assertion (#5554).
  `materializedEdgeScenarioRequirements` computes this requirement directly
  from `reducer.MaterializedEdgeFamilies()` in code; do not add a
  `scenario_requirements:` section to
  `specs/ifa-materialized-edge-coverage.v1.yaml` — it would be ignored (the
  loaded value is always overwritten before `Reconcile` runs) and would mislead
  a reviewer into thinking the file controls the requirement.
- A family claiming a `materialized_edges:<family>` coverage row MUST also
  declare at least one trigger in BOTH the `ifa-determinism` and
  `ifa-fault-injection` blocks of `specs/ci-gates.v1.yaml`, and a non-blank
  entry in `materializedEdgeFamilyTriggerStems`
  (`materializededges/materialized_edges_trigger_stems_test.go`) holding a
  substring that at least one of those triggers contains.
  `TestEveryCoveredFamilyTriggersBothLiveGates` enforces both, over the MERGED
  ledger and BOTH family enumerations. A SHARED family needs its stem the day it
  is enumerated, not the day it is covered — declare the stem the family's
  triggers WILL use; nothing checks it until the coverage row lands. A DIRECT
  family (#6181) owes its stem the day it claims a coverage row instead: #6228
  writes the live wiring, and a stem declared before those triggers exist would
  be a guess. ONE EXCEPTION, added with #6309: a DIRECT family whose live
  determinism triggers ALREADY exist may declare its stem ahead of its
  coverage row, because the stem is then read off committed triggers rather
  than guessed. kubernetes_namespace_environment and iam_instance_profile_role
  were that case -- both stayed waived while carrying stems ahead of their
  coverage rows, until #6309 wired both live matrices and converted their
  waivers to coverage rows. workload_cloud_relationship is the remaining
  waived direct family. Do not remove a stem
  for being ahead of its coverage row without first checking whether the
  family's determinism triggers exist; see the doc comment on
  directFamilyTriggerStems in
  go/internal/ifa/materializededges/materialized_edges_trigger_stems_test.go. Without a trigger, the gate never re-runs when
  that family's Odù, cassette, extractor, or writer changes, and the coverage
  row keeps asserting a proof that has gone stale. Note what this does NOT
  check: whether the declared triggers are the RIGHT ones for the family. No
  gate can, so that stays a review obligation.
- An uncovered `(surface, proof_gate)` row MUST be either bound to a real
  coverage row or listed in the manifest's `waivers:` section with a tracked
  issue; a row in neither fails the blocking gate. Waivers are keyed per
  `(surface, proof_gate)` (each waiver row carries a `proof_gate:` — one of
  `ifa-determinism` / `ifa-fault-injection`), so a per-family waiver with no
  `proof_gate` is too coarse and fails to load. Waiving the `fault` gate does
  NOT green the `baseline` row and vice versa — this is why `sql_relationships`
  could keep a proven baseline while its confirmed-false fault (#5555) was
  waived, before that fault was fixed and the waiver removed once the fault
  row gained real coverage. A waiver on a `(surface, proof_gate)` that later
  gains real coverage is flagged as stale — remove the `waivers:` row in the
  same change that adds the coverage row.
- The manifest is a CLAIMS LEDGER, not a roadmap: absence of a required
  `(surface × scenario_type)` row means NOT CLAIMED / not covered, never
  inferred covered. Do NOT add a permanently-waived row for a dimension you
  cannot prove live. SQL delta-live is now an unwaived required row; its proof
  must keep driving gen 2 and checking the accumulated exact set.
- Before trusting a new family's expected-edge-set fixture against a live
  backend, read `README.md`'s Gotchas note on the #5351 live-proof finding: a
  `content_entity` fact whose `relative_path` has no matching `file` fact
  produces zero graph nodes silently, and a `Function`/`Class`/other
  `canonicalNamePathLineEntityLabels` endpoint's real graph uid is a derived
  hash, not the fact's own `entity_id`.

## Drop an Odù

Adding a conformance case (an Odù) mirrors the parser package's "add a language"
7-step model (`go/internal/parser/AGENTS.md`). Expectations *derive* from the
fact-kind registry plus the B-12 snapshot; you never hand-write a want-list.

1. **Declare the input.** Either drop a **v1 cassette** under
   `testdata/cassettes/` (the format is fail-closed — a non-v1 cassette is
   rejected, `go/internal/replay/format.go`) or add a `LoadFacts`/synth
   descriptor that produces the Odù's `facts.Envelope` set (see
   `demoOrgRoundtripOdu` and the `synth/gcp` generator for the two existing
   patterns).
2. **Redact by key name only.** Cassette redaction is key-name based and payloads
   are opaque (`go/internal/replay/canonical.go`); a secret that is not removable
   by its key name MUST NOT be in the fixture. Do not rely on value-content
   masking — it does not exist.
3. **Register the Odù** in `catalogSeed` (`catalog_seed.go`) as a
   `CatalogOdu{Odu: Odu{Name: "odu:<name>", Facts: ...}}`. Prefer deriving the
   facts from fixturepack valid-payload examples (like `awsPackOdu`) so the Odù
   stays in lockstep with the contract schemas.
4. **Do not hand-list expectations.** `Derive` enumerates the surfaces (one per
   fact-kind-registry entry, one per B-12 evidence-narrowed correlation);
   coverage is computed, never asserted by name — see `coverage_falsegreen_test.go`.
5. **Bind the surfaces the Odù proves** in `specs/ifa-coverage-manifest.v1.yaml`
   (`fact_kind:<kind>` / `narrowed_correlation:<rc>` → `scenario: odu`,
   `ref: odu:<name>`). Seed a row ONLY once it is genuinely green (the C-1
   seed-only-green-rows philosophy); an aspirational binding stays on the
   uncovered worklist.
6. **Run `make prove`.** It reconciles coverage against the manifest (so a new
   fact kind or surface cannot land uncovered) and, when Docker is present, runs
   the determinism matrix over the affected Odù. A nondeterministic failure is a
   determinism defect — quarantine-by-issue and root-cause it; never retry to
   green (the flake policy, `scripts/verify-ifa-determinism.sh`).
7. **Document a new kind or surface** in the same change (the fact-kind registry
   and the relevant package README), the way the parser model documents a new
   language.

## P5 — load and saturation (Layer 3)

- Amplify only through `AmplifyAtSlot` (`amplify.go`). It is family-aware and
  delegates to `synth/gcp.GenerateMultiScope`. Do NOT add a generic
  `scope_id`/`stable_fact_key` rewrite — the ADR Layer 3 landmine proves it is
  determinism-unsafe for cloud-resource families (shared payload identity MERGEs
  onto one node and races last-writer-wins). A new family needs its own
  disjoint-by-construction generator or `AmplifyAtSlot` returns an error.
- `ScaleSlot` (`slots.go`) ADOPTS `specs/scale-lab-corpus.v1.yaml`; the lockstep
  test asserts every bound id is present in the spec. Do not invent a second
  taxonomy or a second perf contract — reuse `perfcontract`'s enforcement split.
- The runtime scenario runners are in `saturation/` and `throughput/`
  subpackages (see their `AGENTS.md`), kept out of this pure core. The
  `ifa-load-saturation` CI gate runs them with `-race`.

## Verification

```bash
cd go && go test ./internal/ifa/... -count=1   # core + saturation + throughput
make prove   # credential-free coverage + determinism mirror (Docker matrix when present)
```

## Adding A Family To The Live Gates

The coverage-row contract above is only half of what a new family needs. The
live gates are driven by a shell family registry
(`scripts/lib/ifa_family_registry.sh`, one row file per family under
`ifa_family_registry/rows/`), and a family that skips any of the artifacts
below is silently not proven rather than loudly missing. Work the list in
order; the gate column tells you what catches a mistake and, more usefully,
what does not.

| Artifact | Where | Enforced by |
|---|---|---|
| Registry row file | `scripts/lib/ifa_family_registry/rows/NN_<family>.sh` | Loader fails closed on a missing/empty rows directory; the derived-pins module fails on a row with no pin |
| Hand-derived pin file | `scripts/lib/ifa_family_registry_pins/<family>.sh` + an `IFA_FAMILY_PINS_NAMES` entry | `test-ifa-family-registry-derived-pins-cases.sh`, both directions |
| Determinism hand-authored section | `scripts/lib/test-ifa-determinism-family-cases.sh` | Bidirectional totality against the registry |
| Fault cells in the dispatch block | `scripts/verify-ifa-fault-injection.sh` | `test-ifa-fault-injection-shard-cases.sh` — set-equality against `--list-cells`, plus the cell-count pin |
| Cell names in `IFA_FAULT_ALL_CELLS` | `scripts/lib/ifa_fault_shard.sh` | Same set-equality check; a dispatched-but-unlisted cell runs in NO shard |
| Triggers in both gate blocks | `specs/ci-gates.v1.yaml` | `TestEveryCoveredFamilyTriggersBothLiveGates` |
| Workflow `paths:` entries | `.github/workflows/ifa-determinism-gate.yml` | Registry-subset-of-workflow lockstep |
| Blocker declaration vs handler shape | registry row `IFA_FAMILY_BLOCKER_KIND` | `TestMaterializedEdgeFamilyBlockerLockstep` — a family whose handler holds no `IntentWriter` may not declare `shared_intent_lock` |
| Cassette + expected-edge path globals | `scripts/lib/ifa_family_fixtures.sh` (`<family>_cassette`, `<family>_expected_edges`) | `ifa_family_fixtures_require` fails before any Compose stack starts. The registry's `CASSETTE_VAR`/`EXPECTED_VAR` hold only the NAMES of these globals, so a row can look complete while the paths do not exist |
| Atomic-group entry for a family-scoped trio | `IFA_FAULT_ATOMIC_GROUPS` in `scripts/lib/ifa_fault_shard.sh` | The shard partitioner co-locates the group. Without an entry the baseline lands in a different shard from its recovery cells, which then read an unset `digests` key |
| Dispatch ORDER within that trio | `scripts/verify-ifa-fault-injection.sh` | `run_ifa_fault_injection_atomic_group_ordering_cases` — the baseline must dispatch before every other member. Co-location alone does not give you order |
| Cell names in the hand-authored literal list | `ifa_full_cell_list_literal` in `scripts/lib/test-ifa-fault-injection-shard-cases.sh` | Nothing but you. It is typed by hand ON PURPOSE — deriving it from the arrays it checks would make the check agree with itself |
| Coverage row (what makes the family COUNT as covered) | `specs/ifa-materialized-edge-coverage.v1.yaml` | The coverage-row contract above. Add it only once both gates really drive and assert the family — a row added earlier claims a proof that is not being run |
| Seam fixtures for the family's triggers | `scripts/lib/ifa_live_gate_selector_cases.sh` | The registry↔workflow lockstep, which runs the REAL matcher over a concrete path. Adding a trigger without a fixture here is silent: a string-only comparison agrees on a broken glob too. One representative path per pattern, in the list matching where the file EXECUTES (common / fault-only / determinism-only) |
| Trigger stem | `materializedEdgeFamilyTriggerStems`, `go/internal/ifa/materializededges/materialized_edges_trigger_stems_test.go` | `TestEveryCoveredFamilyTriggersBothLiveGates` can only check a family whose stem is registered. This one fails loudly rather than silently, but it is on the path |

A new family's `materialized_edges_<family>.go` guard belongs in the
`materializededges` subpackage, NOT here. Its `<family>_family_odu.go` and
`<family>_family_catalog.go` stay in this package. Nothing enforces that split:
`dirgate` only counts files per directory, so a guard written into the wrong
package compiles, passes every gate, and is only caught in review.

Two caps bite, and both are blocking pre-commit gates: `dirgate` caps each
package at 40 non-test `.go` files (`go-dir-gate`), and the 500-line file cap is
the other (`go-file-cap`). The 500-line cap is NOT enforced on `_test.go` --
the linter skips them -- so a test file can drift past it silently; `dirgate`
does not count them at all. This subpackage was not created because ifa breached
the directory cap; it was under it, and the split was taken preemptively to buy
headroom for the families still queued. Measure both before you add, and
deliberately no numbers here, because a count written into prose goes stale the
moment anyone edits the file, and this section's line-cap paragraph was already
wrong twice that way:

```bash
for d in go/internal/ifa go/internal/ifa/materializededges; do
  printf '%s: ' "$d"
  ls "$d"/*.go | rg -v '_test\.go$' | wc -l
done
```

Line-cap headroom is the constraint that will bite first. Several files grow per
family and sit close to the hard 500-line limit. Measure before you add —
deliberately no numbers here, because a count written into prose goes stale the
moment anyone edits the file, and this paragraph was already wrong twice that
way:

```bash
wc -l go/internal/reducer/materialized_edge_family_blocker_shape_test.go \
  scripts/lib/test-ifa-fault-injection-shard-cases.sh \
  scripts/test-verify-ifa-fault-injection.sh \
  scripts/verify-ifa-fault-injection.sh \
  scripts/verify-ifa-determinism.sh | sort -rn
```

Those five grow per family or per cell-list change: the blocker expectations
entry plus its citation, the hand-authored cell literal, a `source` and a
`run_*` call per new case module, two dispatch lines plus this file's convention
of a rationale comment, and — in the determinism gate — the post-delta
generation-2 per-family assertions, roughly two lines each. NOT its drive/assert
wiring: that is the registry loop this design replaced the inline blocks with,
and a new family costs a row there, not a new block.
Note also that the 500-line cap is enforced only for Go: `.pre-commit-config.yaml`'s
`go-file-cap` hook declares `types: [go]` and the `filelength` linter plugin is
Go-only (it also skips `_test.go`), so for every shell file above, for the Go
test file, and for YAML, the limit is policy and nothing will stop you crossing
it. `.github/workflows/ifa-determinism-gate.yml` is over it deliberately (the
sharding trade-off, the baseline-per-shard argument and the trigger
justifications are the kind of rationale this repo would rather keep than trim,
and `test.yml` has been over it on main for longer) — a considered exception,
not an oversight, and the place to split if it grows again is the sharding
rationale into the library it documents. When
headroom runs out, extract — move a self-contained block of
rationale to the library it actually documents, the way the `cell_failgraphwrite_sql`
history and the deployable-unit ordering note were moved. Do not trim comments
to fit; the rationale in these files is what lets a reviewer catch a cell whose
stated intent has drifted from what it does, which is the defect class this
whole program exists to close.

The doc sentence on `MaterializedEdgeOduResolver.Resolve`
("Current guards cover ...") enumerates the resolver's `case` arms — families
with a REGISTERED VACUITY GUARD — not families that are live-proven. Update it
and its pinned copy in `code_call_live_documentation_test.go` in the same
change that adds the family's `case` arm, whatever the live-proof status.
Live proof is tracked by the coverage-manifest row and the waiver table, never
by this sentence. Two authors read this the opposite way in one PR, which is
why it is written down: the deciding evidence is that the sentence already
listed codeowners_ownership_edges while that family's fault side was
explicitly not live-proven, so the live-proven reading was false of the
sentence as shipped.

Not caught by any STATIC check — get these right by hand. Note the difference
between "nothing enforces this" and "nothing enforces it until the gate runs":
the first is a silent gap, the second costs you a twenty-minute cell instead of
a millisecond, which is worth avoiding but is not a hole.

- **`IFA_FAMILY_RETRY_BASELINE_VAR` and `IFA_FAMILY_HANDLER_GO_FILE`.** Both are
  required only for `shared_intent_lock` families — other rows may record
  `handler_go_file` deliberately, and `rows/06` does — both are read only at
  gate RUNTIME, and neither is one of the pinned fields. For a
  `shared_intent_lock` family `_ifa_generic_require_retry_baseline` dies naming
  the family and the missing row, and the kill cell then proves
  retry-above-baseline via `ifa_fault_assert_retried_above` scoped to the
  family's wait_key; the family's baseline cell must populate the named variable
  before its kill cell runs, and `baseline_code_call_retried` is the model. So a
  missing row fails loudly rather than downgrading the cell — but it fails part
  way into a four-shard CI run, which is the expensive place to find out.
  `handler_go_file` has exactly one consumer,
  `_ifa_generic_require_intent_writer`; the Go blocker lockstep does not read it
  (it reflects on the real handler struct instead, which is a stronger check of
  a different property).
- **Triggers for the family's own new lib files.** The sourced-to-triggered
  drift walk only resolves a `source` line whose literal text contains
  `scripts/`; anything sourced through a variable is invisible to it, so a new
  mechanism file needs its trigger added by hand.
- **Regenerating `docs/public/reference/ci-gates.md`** after any trigger change.
  `verify-ci-gates-registry.sh` does not check that artifact;
  `test-generate-ci-gates-doc.sh` does, and CI runs it.
- **`ci.check_names`** if a job's shape changes. A matrix job emits
  `<job> (shard N/4)`, never `<job>`, and the required-check resolver matches on
  exact equality — so a renamed job with no `check_names` resolves to MISSING
  forever and the checks that do run belong to no gate.

The dangerous artifact is the expected-edge set. It is derived by hand from the
family's materialization semantics and asserted exactly, so a wrong derivation
does not fail loudly — it certifies the wrong graph truth, which is worse than
the uncovered state it replaced. Derive it from the reducer's real extraction
path and the writer's actual MERGE, and prove it against a live backend before
seeding the coverage rows.
