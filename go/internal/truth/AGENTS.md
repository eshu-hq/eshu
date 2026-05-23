# internal/truth Agent Instructions

These rules are mandatory for this package. Root `AGENTS.md` still owns the
repo-wide proof, performance, concurrency, and skill-routing rules.

## Read First

1. `README.md` and `doc.go`.
2. `model.go` before changing layers or contracts.
3. `go/internal/reducer/registry.go` before changing reducer registration
   behavior.
4. Query/status response docs before changing API-visible truth fields.

## Local Rules

- Keep this package pure value logic with standard-library imports only.
- Use `ParseLayer` for raw strings; do not cast external input directly to
  `Layer`.
- `LayerCanonicalAsset` is output-only. `Contract.Validate` must reject it in
  `SourceLayers`.
- `SourceLayers` must stay non-empty and duplicate-free.
- `Contract.Supports` can remain a linear scan because source-layer lists are
  intentionally short.
- This package must not own reducer dispatch, storage, query serialization, or
  runtime state.

## Change Gates

- New layers require parser validation tests, contract validation tests,
  reducer registration updates, and API/query docs if surfaced.
- New contract helpers require tests first in `model_test.go`.

## Do Not Change Without Owner Review

- Existing layer string values.
- `canonical_asset` as output-only.
- No-I/O/no-internal-imports package boundary.
