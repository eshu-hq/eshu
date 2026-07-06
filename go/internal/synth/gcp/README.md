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

## Determinism

`Generate(Options{Seed: N, ...})` called twice with identical options produces
byte-identical output (`TestGenerateIsByteIdenticalForSameSeed`), because
generation draws only from the seeded PCG and the fixed, sorted
`assetTypeInventory`, and canonicalization removes any remaining
run-to-run churn (key ordering, the derived `generation_id`, the collapsed
`observed_at` sentinel).

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
