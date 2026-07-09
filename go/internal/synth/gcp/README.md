# synth/gcp

Deterministic, seeded synthetic GCP corpus generator. Emits a v1 cassette
(`go/internal/replay/cassette`) whose payloads are schema-valid against the
`#4567` GCP JSON Schemas (`sdk/go/factschema/gcp/v1`, shipped for conformance
via `sdk/go/factschema/fixturepack`).

## Why

Contributors without any GCP account cannot record a cassette, and a real
recorded cassette cannot be published: redaction is key-name-only with opaque
payloads (`go/internal/replay/canonical.go:49-83`), so a recorded fixture may
carry values that look sensitive even after redaction. A synthetic generator
sidesteps this entirely — nothing sensitive is ever produced, so there is
nothing to redact and the corpus is committable and shareable by construction
(docs/internal/design/4389-ifa-conformance-platform.md, "Public corpora
without provider access").

## What it does

`Generate(Options) ([]byte, error)`:

1. Seeds a `math/rand/v2` PCG from `Options.Seed`.
2. Builds `Options.ResourceCount` `gcp_cloud_resource` facts, cycling through
   the static `assetTypeInventory` (`asset_types.go`) — a duplicated copy of
   the GCP typed-depth extractor registry's asset-type vocabulary
   (`go/internal/collector/gcpcloud`'s `RegisterAssetExtractor` call sites),
   not a live import of that package.
3. Derives `gcp_cloud_relationship`, `gcp_collection_warning`,
   `gcp_dns_record`, and `gcp_iam_policy_observation` facts from the
   generated resource set in a fixed proportion.
4. Encodes every payload through the real `sdk/go/factschema.EncodeGCP*` seam
   — the same struct-to-map path a collector's own emitter would use — so
   payloads are schema-valid by construction, never a hand-built map.
5. Assembles a `cassette.File`, marshals it, and canonicalizes it with
   `replay.Canonicalize(replay.DefaultCanonicalOptions())` — the same
   canonicalization `go/internal/replay/recorder` applies to a live-recorded
   cassette — then loads the result back through `cassette.ParseAndValidate`
   as a guard before returning it.

`generateFact` fails closed: a fact kind absent from `factKindSchemaVersions`
(the five kinds this generator has schema coverage for) is refused rather than
emitted unvalidated.

## Demo-org coherence profile

`DefaultDemoOrgProfile()` formalizes the demo/conformance corpus identity
scheme already used by the golden-corpus gate: `ESHU_GITHUB_ORG=acme` and
deterministic repository remotes shaped as `github.com/acme/<repo>`. That scheme
is reserved in `JoinKeyRegistry`, including the cross-repo package owner hint
for `github.com/acme/lib-common`, so later synthetic families can share the same
join keys instead of inventing a second fake org.

`GenerateDemoOrgCassette(DefaultDemoOrgProfile())` returns canonical cassette
bytes plus the repository-relative manifest-layout path
`testdata/cassettes/gcpcloud/supply-chain-demo.json`.
`WriteDemoOrgCassette(repoRoot, DefaultDemoOrgProfile())` is the regeneration
entry point for the first generated family: it writes those bytes under
`testdata/generated-cassettes/gcpcloud/supply-chain-demo.json`, while preserving
the committed golden-corpus path as `GeneratedCassette.ManifestPath`. Replacing
the committed `testdata/cassettes/gcpcloud/supply-chain-demo.json` artifact is
only valid under the operator-gated golden-corpus swap test, because the demo
answers depend on projected graph truth, not only cassette shape. The entry
point is Go, not a shell generator script, so the `generate-*.sh` / `lib/` /
`test-generate-*.sh` pattern is intentionally not introduced for this issue.

## Fact-envelope replay helper

`DemoOrgFactEnvelopes(DemoOrgProfile) ([]facts.Envelope, error)`
(`demo_envelopes.go`) generates the demo-org cassette and replays it through
the production `cassette.Source` seam (`go/internal/replay/cassette/
source.go`) — the same `collector.Source` implementation `collector.Service`
drives against a real cassette file — returning every fact envelope the
generated cassette's scope carries. It exists so a consumer that needs the
demo-org corpus as `facts.Envelope` values (for example `go/internal/ifa`'s
`odu:demo-org-roundtrip`, issue #4804) drives the same replay path a real
poll loop would, rather than hand-mirroring the generator's payload shapes
and silently drifting from them.

## Determinism

`Generate(Options{Seed: N, ...})` called twice with identical options produces
byte-identical output (`TestGenerateIsByteIdenticalForSameSeed`), because
generation draws only from the seeded PCG and the fixed, sorted
`assetTypeInventory`, and canonicalization removes any remaining
run-to-run churn (key ordering, the derived `generation_id`, the collapsed
`observed_at` sentinel).

The seed is part of the scope identity, not only cosmetic metadata: the scope
id is `gcp:project:<ProjectID>:seed:<Seed>`. Two corpora with the same
`ProjectID` but different `Seed` therefore carry distinct `scope_id`,
`generation_id` (derived from `scope_id`), and derived `fact_id`
(`facts.StableID` over `scope_id`, `generation_id`, `stable_fact_key`), so both
can be replayed into one store as independent corpora rather than the later run
fencing or overwriting the earlier one
(`TestSameProjectDifferentSeedsHaveDisjointReplayIdentities`).

## Conformance and schema validity

`TestGeneratedCassettePassesConformance` and
`TestGeneratedPayloadsValidateAgainstCheckedInSchemas` prove every generated
payload validates against the checked-in `#4567` JSON Schema for its kind
through the real `sdk/go/collector/conformance` harness — the same harness an
out-of-tree collector's own CI runs — and
`TestSchemaValidationRejectsPayloadMissingRequiredField` proves the negative
case: a payload missing a schema-required field fails conformance with
`FindingPayloadSchemaInvalid`.

## Parity check (maintainer-run, not CI)

`TestParitySyntheticVsRecordedGCPShape` (`parity_test.go`) compares this
generator's fact-kind and asset-type shape against the recorded GCP cassette
(`testdata/cassettes/gcpcloud/supply-chain-demo.json`). It is **skipped by
default and in CI**; a maintainer runs it locally with:

```bash
ESHU_SYNTH_GCP_PARITY=1 go test ./internal/synth/gcp/... -run TestParitySyntheticVsRecordedGCPShape -v
```

This follows the same operator-gated live-smoke precedent as
`go/internal/collector/pagerduty/live_test.go`'s `ESHU_PAGERDUTY_LIVE` gate —
credential-free here (no live GCP account is needed; only the maintainer's
local checkout), but deliberately excluded from the default and CI test runs
because it asserts against a hand-curated, versioned inventory
(`recordedNonExtractorAssetTypes`) that a maintainer, not CI, refreshes when
the real extractor registry changes.

## What it does not do

- No network access, no filesystem reads outside the parity check's own
  `testdata/cassettes/gcpcloud/supply-chain-demo.json` load, and no
  credential of any kind.
- No redaction step: nothing sensitive is ever produced.
- Does not import `go/internal/collector/gcpcloud` or any other collector
  internal package (Contract System v1 §3.5).
