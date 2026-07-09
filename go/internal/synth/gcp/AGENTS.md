# synth/gcp — agent scope

## Owned surface

- `go/internal/synth/gcp/` — `Generate(Options)`, the seeded GCP corpus
  generator, `assetTypeInventory` (`asset_types.go`), `DemoOrgFactEnvelopes`
  (`demo_envelopes.go`, the production `cassette.Source` replay seam for the
  demo-org corpus), `GenerateMultiScope(MultiScopeOptions)`
  (`multiscope.go`, issue #4396 slice 6b: K-independent-scope generation for
  the Ifá P3 determinism matrix), and the maintainer-run parity check
  (`parity_test.go`).

## Key invariants

- **Never import collector internals.** This package must not import
  `go/internal/collector/gcpcloud` or any other `go/internal/collector/...`
  package (Contract System v1 §3.5, issue #4581). `assetTypeInventory` is a
  deliberately duplicated, static copy of the extractor registry's asset-type
  vocabulary — refresh it by hand from the registry's
  `RegisterAssetExtractor` call sites, never by adding an import.
- **Payloads MUST come from the typed `sdk/go/factschema/gcp/v1` structs**,
  encoded through `factschema.EncodeGCP*`. Never hand-build a
  `map[string]any` payload for a kind this package emits.
- **Fail closed on an unknown kind.** `generateFact` only emits a kind present
  in `factKindSchemaVersions`. Adding a new GCP fact kind here requires first
  confirming a `#4567` JSON Schema exists for it
  (`sdk/go/factschema/schema/<kind>.v1.schema.json`) and adding the kind to
  `factKindSchemaVersions`, never emitting it unconditionally.
- **`Generate` MUST stay deterministic.** The same `Options.Seed` (with
  identical remaining options) must produce byte-identical output
  (`TestGenerateIsByteIdenticalForSameSeed`). Draw randomness only from the
  `*rand.Rand` seeded in `Generate`; never from `time.Now()`,
  map iteration order, or an unseeded global source (`ObservedAt` is stamped
  with `time.Now()` but is a `VolatileKeys` entry that `replay.Canonicalize`
  collapses to a fixed sentinel, so it does not break determinism — do not
  remove that canonicalization step).
- **Always canonicalize through `replay.Canonicalize`** with
  `replay.DefaultCanonicalOptions()`, and load the result back through
  `cassette.ParseAndValidate` before returning it — mirroring
  `go/internal/replay/recorder`'s load-back guard. Never return an
  un-canonicalized or unvalidated cassette.
- **No credentials, no network, no redaction step.** Every value is derived
  from `Options.Seed` and `Options.ProjectID`. Do not add any I/O to
  `Generate` beyond the parity check's own read of the recorded testdata
  cassette (which is test-only, gated, and unrelated to `Generate` itself).
- **`GenerateMultiScope` MUST keep every scope's identity disjoint.** It
  derives each scope's `ProjectID` from `scopeProjectID(i)`
  (`"acme-demo-gcp-<NN>"`), never a value a caller supplies directly, and never
  reuses `DefaultDemoOrgProfile`'s or the recorded demo-org cassette's own
  project id. A change here that could let two scopes share a `ProjectID` (and
  therefore a `full_resource_name`/CloudResource uid) breaks the whole reason
  this function exists — re-read `multiscope.go`'s doc comment and
  `TestGenerateMultiScopeScopesHaveDisjointFullResourceNames` before touching
  the project-id derivation.
- **`GenerateMultiScope` MUST stay deterministic** the same way `Generate`
  must: the same `(Seed, Scopes, ResourceCount)` always produces byte-identical
  output (`TestGenerateMultiScopeIsByteIdenticalForSameInputs`). It reuses
  `Generate`'s own per-scope determinism; do not introduce per-scope
  randomness (e.g. deriving each scope's seed from `time.Now()` or an
  unseeded source) when adding scope variety.
- **The parity check (`parity_test.go`) stays operator-gated, not CI.** Guard
  it with the `ESHU_SYNTH_GCP_PARITY` environment variable, mirroring
  `go/internal/collector/pagerduty/live_test.go`'s `ESHU_PAGERDUTY_LIVE`
  pattern. Do not wire it into `make pre-pr` or any CI workflow.
  `recordedNonExtractorAssetTypes` is a hand-maintained allow-list; when the
  parity check reports a genuinely new stale-inventory finding, refresh
  `assetTypeInventory` from the real extractor registry rather than papering
  over the finding by adding the asset type to the allow-list.

## Skill routing

- `golang-engineering` for any Go change here.
- `eshu-contract-rigor` — this package's payloads flow through
  `sdk/go/factschema`'s typed structs and decode/encode seam.
- `eshu-golden-corpus-rigor` if a change here ever touches
  `testdata/cassettes/gcpcloud/supply-chain-demo.json` or the B-12 snapshot
  (the parity check only reads that cassette; it must never write to it).

## Do not

- Import any `go/internal/collector/...` package.
- Hand-build a fact payload as a raw `map[string]any` for a kind with a typed
  struct.
- Emit a fact kind absent from `factKindSchemaVersions`.
- Wire the parity check into CI or `make pre-pr`.
- Introduce nondeterminism into `Generate`.
