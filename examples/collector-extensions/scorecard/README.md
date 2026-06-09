# Reference Scorecard Collector

## Purpose

This package is the reference open-source collector extension for Eshu. It
reads OpenSSF Scorecard-style JSON, emits `collector-sdk/v1alpha1` result
records with namespaced fact kinds, and stays outside core runtime ownership.

## Ownership boundary

The package owns source observation for one Scorecard JSON document,
deterministic fact keys, and SDK result construction. It does not import
`go/internal`, write graph truth, mutate workflow claims, implement hosted
scheduling, or verify package provenance.

## Fact contract

The manifest declares three reported evidence families:

- `dev.eshu.examples.scorecard.snapshot`
- `dev.eshu.examples.scorecard.check`
- `dev.eshu.examples.scorecard.warning`

The reducer contract is `source_evidence_only:no_graph_truth`. These facts are
safe provenance until a separate core-owned reducer or query issue decides how
to consume them.

## Local use

Run the reference collector against the checked-in fixture:

```bash
go run ./cmd/scorecard-collector --input ./testdata/complete.json
```

Verify the component manifest and local CLI inventory lifecycle:

```bash
go -C ../../../go run ./cmd/eshu component inspect ../examples/collector-extensions/scorecard/manifest.yaml
go -C ../../../go run ./cmd/eshu component verify ../examples/collector-extensions/scorecard/manifest.yaml \
  --trust-mode allowlist \
  --allow-id dev.eshu.examples.scorecard \
  --allow-publisher eshu-hq
scripts/test-local-component-lifecycle.sh
```

The digest-pinned image in `manifest.yaml` is a reference placeholder for local
manifest validation. Publishing a real package must replace it with the built
artifact digest.

## Privacy posture

Fixtures use public example repository names only. The collector never accepts
tokens, stores raw provider responses, places credentials in source URIs, or
uses source identifiers as metric labels.

## Verification

Use these focused gates from this directory:

```bash
go test ./...
go run ./cmd/scorecard-collector --input ./testdata/complete.json
scripts/test-local-component-lifecycle.sh
```

From the repository root, pair those with the Eshu docs and hygiene gates when
changing public docs:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

No-Observability-Change: this package does not add a hosted runtime, metric
registration, queue consumer, or graph write path. It only declares the
component-owned metrics prefix that a future hosted adapter can use.
