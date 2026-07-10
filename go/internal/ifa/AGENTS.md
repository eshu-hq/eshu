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
- The `ifa-contract-layer` CI gate stays advisory; do not flip it to blocking
  without a follow-up milestone decision, and do not make `ifa coverage`
  hard-fail on that gate's own "not blocking" proof-gate finding.
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

## Verification

```bash
cd go && go test ./internal/ifa -count=1
```
