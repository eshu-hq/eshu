# AGENTS.md — internal/terraformschema guidance for LLM assistants

## Read first

1. `go/internal/terraformschema/README.md` — package purpose, loader flow,
   exported surface, and invariants
2. `go/internal/terraformschema/schema.go` — `LoadProviderSchema`,
   `InferIdentityKeys`, `ClassifyResourceService`, `ClassifyResourceCategory`;
   all logic lives here
3. `go/internal/terraformschema/categories.go` — the `serviceCategories` table;
   this is the primary extension surface
4. `go/internal/terraformschema/paths.go` — `DefaultSchemaDir`; the env-
   override path
5. `go/internal/terraformschema/schemas/README.md` — how schema files are
   generated, gzipped, and named

## Invariants this package enforces

- **nil-safe load** — `LoadProviderSchema` returns `(nil, nil)` on absent or
  unparseable files. Callers in `internal/relationships` must check for nil
  before calling methods on `ProviderSchemaInfo`. Do not change the nil return
  to an error; schema unavailability is not fatal to evidence extraction.
- **Deterministic provider key ordering** — `providerKeys` are sorted before
  iterating so `ProviderSchemaInfo.ResourceTypes` has a stable build order.
  Do not use unsorted map iteration in the loader.
- **Gzip detection by suffix only** — `.gz` extension triggers
  `gzip.NewReader`; anything else decodes as plain JSON. This is intentional
  and must not be changed to content-sniffing without updating the schema
  packaging documentation.
- **No ESHU-internal imports** — this package has no imports from
  `internal/facts`, `internal/relationships`, `internal/storage`, or any
  other Eshu package. It must remain a leaf dependency.
- **Standard library only** — no external dependencies beyond stdlib. This
  constraint keeps the package usable in thin build environments without
  vendoring.

## Common changes and how to scope them

- **Add a new provider schema** →
  1. Generate with `terraform providers schema -json > <provider>.json`.
  2. Gzip and place in `schemas/` following the `<provider>-<version>.json.gz`
     naming convention.
  3. No code changes needed; RegisterSchemaDrivenTerraformExtractors in
     `internal/relationships` will pick it up via glob.
  4. Run `go test ./internal/terraformschema ./internal/relationships -count=1`.

- **Add a new service/category mapping** →
  1. Add entries to `serviceCategories` in `categories.go`.
  2. Use the resource type's service-part prefix (everything after the first
     `_`).
  3. Prefer longer prefixes to avoid false matches; `ClassifyResourceService`
     uses longest-prefix matching.
  4. Run `go test ./internal/terraformschema -count=1` including the classify
     tests.

- **Add a new identity key pattern** →
  1. Append to `identityKeyPatterns` in `schema.go`.
  2. Place higher-confidence patterns before lower-confidence ones; the first
     match wins.
  3. Run `go test ./internal/terraformschema -count=1` identity tests.

- **Change nested identity block merging** →
  1. Append to `nestedIdentityBlocks` in `schema.go`.
  2. Verify that merging the new block does not shadow existing top-level
     attributes for any resource type.
  3. Run the full loader test suite.

## Failure modes and how to debug

- Symptom: `LoadProviderSchema` returns `(nil, nil)` unexpectedly →
  likely cause: file does not exist at the computed path, or is not a valid
  JSON or gzipped JSON document → check that `DefaultSchemaDir()` returns the
  correct path; check that the file has the `.gz` extension if gzipped.

- Symptom: `InferIdentityKeys` returns empty for a resource type →
  likely cause: none of the `identityKeyPatterns` match a string-typed
  attribute and no `*_name` / `*_identifier` fallback exists → inspect the
  `ResourceTypes[resourceType]` map for the actual attribute names; add the
  key to `identityKeyPatterns` if it is semantically stable across resources.

- Symptom: `ClassifyResourceCategory` returns `"infrastructure"` for a
  known resource type →
  likely cause: the service-part prefix is not in `serviceCategories` →
  add the prefix to the table in `categories.go`; use the longest unambiguous
  prefix.

- Symptom: schema-driven evidence missing for a specific provider resource →
  likely cause: RegisterSchemaDrivenTerraformExtractors was not called with
  the schema directory, or the schema file for that provider is missing → check
  the return value of RegisterSchemaDrivenTerraformExtractors which maps
  provider base name to registered resource count.

## Anti-patterns specific to this package

- **Importing ESHU-internal packages** — any import of packages under
  `go/internal/` other than stdlib is a boundary violation. This package is a
  leaf dependency by design.
- **External dependencies** — do not add `gopkg.in/yaml.v3`, `github.com/`
  modules, or any other external package. The package must stay stdlib-only.
- **Content-sniffing for gzip** — detection must remain suffix-based. Using
  `bytes.Equal(header, gzipMagic)` would complicate reproducibility testing
  and break the documented packaging contract.
- **Adding mutable global state beyond the schema loader** — the
  `serviceCategories` map is package-level but read-only after init. Do not
  add writable global state; use function parameters instead.

## What NOT to change without an ADR

- `identityKeyPatterns` order and values — these affect which resource
  attribute becomes the candidate name in schema-driven evidence; changes
  shift which relationships are resolved in production.
- `serviceCategories` entries once used in stored graph nodes — renaming a
  category (e.g. `"compute"` to `"container"`) is a breaking change for any
  graph queries or API responses that filter on category.
- ESHU_TERRAFORM_SCHEMA_DIR env override semantics in `DefaultSchemaDir` —
  operator playbooks and container configurations rely on this variable name;
  renaming it requires coordinated documentation and deployment changes.
