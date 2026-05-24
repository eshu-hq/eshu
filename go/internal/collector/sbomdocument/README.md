# sbomdocument

Fixture-backed CycloneDX and SPDX JSON parsers. Emits reported-confidence
SBOM source facts that the SBOM and attestation attachment reducer can
consume without changes.

## What it owns

This package owns one slice of the parse step in the canonical Eshu
ingestion flow:

```
sync -> discover -> parse -> emit facts -> enqueue work -> reducer
```

Given the raw bytes of a single SBOM JSON document and a fixture context,
it returns a deterministic slice of `facts.Envelope` records:

- `sbom.document` — exactly one per call, carrying format, spec version,
  document digest, subject digest, parse status, counts, and document
  metadata.
- `sbom.component` — one per projected component or SPDX package,
  including the document subject (CycloneDX `metadata.component` or the
  SPDX package(s) that the document `DESCRIBES`).
- `sbom.external_reference` — one per CycloneDX `externalReferences[]`
  entry or SPDX `externalRefs[]` locator.
- `sbom.dependency_relationship` — one per resolved CycloneDX
  `dependencies[].dependsOn` edge or SPDX `relationships[]` entry that
  is not `DESCRIBES`/`DESCRIBED_BY`.
- `sbom.warning` — one per parser warning.

## What it does not own

- Discovery, fetching, or attestation/signature verification — those live
  in the OCI registry collector and the attachment reducer.
- Reducer projection. This package only emits source facts. The reducer
  classifies the document (`attached_parse_only`, `attached_verified`,
  `subject_mismatch`, `ambiguous_subject`, `unknown_subject`,
  `unparseable`, etc.) based on the entire fact bundle including
  attestation evidence.
- Vulnerability evidence. CycloneDX vulnerability sections are explicitly
  not projected; advisory data is owned by the vulnerability intelligence
  collector.

## Verification posture

**Parser output is never verified truth.** Every fact this package emits
is `SourceConfidence = reported` and carries `verification_status = ""`.
The attachment reducer is the only component allowed to mark a document
as `attached_verified`, and it does so only when matching attestation
statements and signature verification facts exist for the same document
digest. Parsed JSON is a strong negative signal (`unparseable` if it
fails) but is not by itself a proof of authenticity or binding to any
artifact.

## Warning reasons

| Reason                       | Meaning                                                      |
|------------------------------|--------------------------------------------------------------|
| `malformed_document`         | JSON failed to decode or did not match a known SBOM shape    |
| `missing_subject`            | Document parsed but no subject digest could be derived       |
| `ambiguous_subject`          | Multiple distinct subject digests were observed              |
| `duplicate_component_identity` | Two components in one document share canonical identity   |
| `unsupported_field`          | A document section is intentionally not projected            |
| `component_missing_identity` | Component lacked both PURL and name                          |
| `unattached_relationship`    | Dependency edge referenced an unknown component              |

## Performance shape

- Parsing is `O(N)` over the document body. There is no graph write and
  no I/O outside of `json.Decode`.
- All output slices are pre-sized from the input shape; the parser does
  not allocate per-component scratch maps.
- Hash, license, and reference outputs are stably sorted so two
  identical inputs produce byte-identical fact bundles.

## Adding a new SBOM format

1. Add `<format>_types.go` with the JSON shape struct (anonymous fields
   that are unprojected stay typed as `map[string]any` or `any`).
2. Add `<format>_fixture.go` with a single exported entry point that
   takes raw bytes plus `FixtureContext`.
3. Add `<format>_components.go` for the per-component projection so the
   per-file LOC budget stays under the repo limit.
4. Add fixtures under `testdata/` covering: image subject, missing
   subject, malformed body, and any format-specific edge cases.
5. Wire the parser into the reducer integration test in
   `reducer_integration_test.go`.
