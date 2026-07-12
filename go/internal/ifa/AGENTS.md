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
