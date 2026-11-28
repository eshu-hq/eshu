# envregistry

Code-owned source of truth for Eshu's `ESHU_*` environment variables.

## Purpose

Eshu reads ~hundreds of `ESHU_*` variables from scattered `os.Getenv` call
sites. Before this package there was no central registry, no startup validation,
and inconsistent naming (`ESHU_POSTGRES_DSN` vs `ESHU_CONTENT_STORE_DSN`), so
misconfiguration failed late with unhelpful errors. This package declares the
supported core-platform variables once and powers:

- `eshu config validate` — checks the live environment for invalid values,
  deprecated variables, and likely typos.
- `docs/public/reference/env-registry.md` and `docs/public/reference/env-registry/*.md`
  — the generated reference: an index plus one page per subsystem family.

## What it covers

The registry covers the **core platform** subsystems (`postgres`, `graph`,
`runtime`, `api`, `mcp`, `reducer`, `projector`, `coordinator`, `semantic`,
`component`) declared in `entries.go`, and the **hosted-collector** production
configuration declared in `entries_collectors.go` (one `collector-*` subsystem
per collector). Container-registry credential variables (`ESHU_*_OCI_*`,
`ESHU_*_PACKAGE_*`) are integration-test gating read only from `_test.go` and are
out of scope.

`TestRegistryCoversCoreEnvCallSites` is the CI gate: it scans the canonical core,
collector, and split command config files (`coreScanFiles`) and fails if any
`ESHU_*` they read is missing from the registry. This keeps the registry from
drifting away from the code it documents, scoped honestly to what it claims to
cover.

## Exported surface

- `Entry` — one variable declaration (name, type, default, subsystem,
  description, allowed enum values, aliases, deprecation).
- `Registry` / `New` / `Default` — immutable lookup over a set of entries,
  indexed by canonical name and alias.
- `Registry.Validate(env, strict)` — returns `Finding`s (invalid value,
  deprecated, unknown).
- `Registry.RenderPages` — the committed reference as an index page plus one
  page per `pageLayout` entry, each refused if it would pass `PageLineCap` (500)
  lines. `Page`, `IndexPagePath`, `PagesDir`, and `PageLineCap` describe it.
- `Registry.RenderMarkdown` — the whole registry as one document, for
  `eshu config validate --reference`. It is not committed.
- `VarType` constants — `VarString`, `VarInt`, `VarBool`, `VarDuration`,
  `VarEnum`, `VarDSN`.

## Aliases and deprecations

- DSN precedence: `ESHU_FACT_STORE_DSN` → `ESHU_CONTENT_STORE_DSN` →
  `ESHU_POSTGRES_DSN` (declared as aliases of the canonical `ESHU_POSTGRES_DSN`).
- Graph: `ESHU_NEO4J_*` fall back to legacy non-prefixed `NEO4J_*` (declared as
  aliases).
- Deprecated: `ESHU_REDUCER_CLAIM_DOMAIN` → `ESHU_REDUCER_CLAIM_DOMAINS`;
  legacy alias `ESHU_WORKFLOW_COORDINATOR_ENABLE_CLAIMS` →
  `ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED`.

## Maintaining the registry

1. Add or change an `Entry` in `entries.go`. If the variable lives in a new
   split config file, add that file to `coreScanFiles` so the coverage test
   protects it.
2. Regenerate the reference doc:
   `bash scripts/generate-env-registry-doc.sh`.
3. Run `go test ./internal/envregistry -count=1`. If you added a variable read
   in a `coreScanFiles` file, the coverage test enforces it is declared.

The checked-in reference is also guarded by
`bash scripts/verify-env-registry-doc.sh` and its hermetic mirror
`bash scripts/test-generate-env-registry-doc.sh`. The verifier fails when
`docs/public/reference/env-registry.md` or any page under
`docs/public/reference/env-registry/` is manually edited, missing, stale
relative to `go/internal/envregistry`, or left over after the generator stopped
producing it. "Left over" means any non-directory entry in `env-registry/`
(files, hidden files, symlinks; removing a symlink never touches its target);
subdirectories are skipped, never deleted.

### Page layout

The reference is split because one page would pass the 500-line Markdown cap.
`pageLayout` in `pages.go` assigns each `Entry.Subsystem` to one family page.
The assignment is explicit, not packed by size, so a page's URL and its
`## <subsystem>` anchors do not move when a variable is added. The generator
fails, and writes nothing, when a subsystem has entries but no page, appears on
two pages, is listed without entries, or when a page would pass the cap. A new
`ESHU_*` variable in an existing subsystem never needs a layout change. A new
subsystem needs one line in `pageLayout`. If a page nears the cap, move whole
subsystems between pages; keep each page at 350 lines or fewer so a later
variable does not trigger the cap. Slugs and subsystem names must be lowercase
hyphenated names, and the slug `index` is refused, so the `#subsystem` anchors
and `<slug>.md` paths the index emits are the ones mkdocs serves.

Per-subsystem anchors such as `env-registry.md#api` moved to the family pages
with the split: an old deep link now lands at the top of the index, which says
so and lists every subsystem with a link to its section. Links written since
point at `env-registry/<slug>.md#<subsystem>`. The mkdocs nav section
"Environment Variable Registry (generated)" is hand-written; a test
(`TestMkdocsNavListsEveryGeneratedPage`) fails when it omits a page or titles
one differently from `pageLayout`. The pages directory is
`docs/public/reference/env-registry/`, not `generated/`, because the line-cap
gate skips any path segment named `generated`.
