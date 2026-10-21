# Evidence: retire reducer FactLoader/Domain family (#6626 factloader/domain slice)

## What changed

`type FactLoader = factload.FactLoader`, the 14 shared-projection domain
consts (`DomainRepoDependency` … `DomainSubmodulePinEdges`), and
`var allProjectionDomains` are deleted from `compat_decode.go`, and every
caller now spells the canonical `factload.FactLoader` / `reducercontract.*`:

- 123 caller files: qualified `reducer.FactLoader` / `reducer.Domain*` /
  `reducer.allProjectionDomains` repointed, adding the aliased
  `reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"`
  or `reducercontract`/`factload` import where missing (151 contract
  imports added) and dropping 94 now-unused reducer-root imports.
- 74 reducer-root files: in-package bare uses qualified with a
  collision-aware rule — `FactLoader:` composite-literal field names and
  line-start `FactLoader <type>` field declarations keep their name;
  `case Name:` labels still qualify; the `Domain*` consts in `Name:`
  position are map keys (blocker shape / lease-hold tests) and qualify;
  `allProjectionDomains` (read-only `len` + `range` uses only) becomes
  `reducercontract.ProjectionDomains()` at each of its 8 sites.
- Incidents caught by build/vet and fixed: a comment-only
  `reducercontract.` mention pulling a spurious import (need-detection is
  now comment-blind); a `reducer.` doc mention retaining a dead import;
  bare `FactLoader` inside `factload/` itself (self-import cycle) and inside
  `sqlrelationship/` (which keeps its own `FactLoader` alias) — the bare
  pass is therefore root-directory-only; `factload.FactLoader:` field
  damage from mid-line `FactLoader:` sets (now skipped everywhere);
  unsorted import insertion (quoted-vs-bare path comparison) fixed by
  `gofmt -w` over the 30 affected files, all HEAD-clean.
- Guard test `TestFactLoaderDomainFamilyRetired` in
  `compat_alias_retirement_factloader_test.go` scans the whole reducer
  package directory AST for any re-declared alias (bucket 3/4/5/6/7 pattern).
- Deliberate remainder: one README-pinned doc string
  (`code_call_live_documentation_test.go`, "extraction proven by" line)
  keeps the old spelling, as in Slice-A; the doc-parity test passes
  unmodified. No golden, spec, manifest, or inventory content changed.

199 `.go` files (198 repointed + the new guard test) plus this note.

## Why no regression

No-Regression Evidence: baseline is the pre-removal tree where the alias
and the 14 consts forward to identical types, values, and the 14-element
projection list; after is this head with the same spellings named directly,
so every repoint is spelling-only with no signature, envelope, row, or
ordering change. Input shape is the fixed 14-element domain list;
`ProjectionDomains()` returns a deterministic literal. Proof on this head:
`go build ./...` exit 0;
`go vet ./internal/reducer/...` exit 0; `gofmt -l` clean over all 199
edited files; `go test -count=1` green over `./internal/reducer/...`,
`./internal/storage/cypher/...`, `./internal/storage/postgres/` (+`/lock`),
and the ifa/workflow/projector/cmd trees; guard RED (all 16 names flagged
pre-removal) / GREEN (passes post-removal);
`TestMaterializedEdgeLiveProofDocumentationMatchesWiring` passes with the
carve-out; `git diff --check` clean. Precise `reducer.(FactLoader|…|allProjectionDomains)` grep over Go sources returns only the pinned string.
Pre-existing failures reproduced on a clean HEAD worktree and unrelated to
this slice: `replay/parserfixture` flakes intermittently on HEAD too;
`cmd/dirgate` trailer TempDir-cleanup flakes pass in isolation;
`cmd/eshu` vuln-scan tests fail on HEAD (missing `/tmp` repo fixture).

Local toolchain note: `GOTOOLCHAIN=go1.26.10` with
`CGO_CFLAGS="-O2 -g -std=gnu17"` (repo cgo workaround).

## Observability

No-Observability-Change: no new metric instrument, metric label, span,
structured log field, queue domain, worker, lease, or runtime knob.
Domain/fact-loader plumbing keeps reporting through the existing spans and
counters; only the static type spelling at call sites changed.
