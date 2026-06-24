# internal/extensions/skillgen

The skillgen extension owns the source-of-truth skill fragment loader, the
byte-citation preservation contract, the per-host adapter registry, and the
deterministic render pipeline that produces the roundtrip baseline committed
at the repo root under `expected/`.

## Purpose

The package reads `skill-fragments/*.md`, parses each fragment's YAML
frontmatter, formats the `byte_citation` into a stable top-of-file comment
block, and renders one skill file per registered host (Claude Code, Cursor,
Codex). The `per-collector-matrix` fragment is the only fragment that
consumes per-deployment capability overrides read from
`skill-fragments/capabilities.local.yaml` (gitignored).

## Ownership boundary

The package owns the agent-facing skill file contract. It does not own
graph, query, storage, collector, runtime, or telemetry logic; it is a
build-time generator with no runtime side effects. The package must remain
free of Eshu internal package imports; it depends only on the standard
library and `gopkg.in/yaml.v3` for frontmatter parsing.

## Exported surface

See `doc.go` for the godoc package contract. The public surface includes:

- `Fragment`, `LoadFragments` for source-of-truth loading.
- `NormalizeByteCitation`, `FormatCommentBlock` for citation comment-block
  preservation.
- `Host`, `HostAdapter`, `RenderInput`, `AdapterFor`, `AllHosts`,
  `HostFromString` for the per-host adapter registry.
- `Capabilities`, `DefaultCapabilities`, `LoadCapabilities` for the
  per-deployment capability override file.
- `RenderAll`, `RenderResult`, `WriteExpected`, `CheckDrift`, `Drift` for
  the deterministic render pipeline and the `gen`/`check` subcommand
  surface.
- `byteCitationPrefix` for tests and adapters that need the comment marker.

## Dependencies

The package depends only on the Go standard library and `gopkg.in/yaml.v3`
(already in `go.mod`). It intentionally imports no Eshu storage, facts,
workflow, telemetry, reducer, query, or graph packages. The package is a
build-time generator; the absence of runtime dependencies is the
discipline.

## Telemetry

This package emits no telemetry directly. It is invoked by
`go/cmd/skillgen` at build time, and the `check` subcommand exits
non-zero when the baseline drifts. Adding a metric here would be wrong;
build-time drift is detected by the S3 CI gate, not by runtime telemetry.

## Gotchas / invariants

- `LoadFragments` returns fragments in id-sorted order. The render pipeline
  relies on this for deterministic byte output; do not change the sort.
- `FormatCommentBlock` is sorted and deduplicated. The emitted comment
  block is byte-stable for the same fragment set, so the roundtrip
  baseline test in `render_test.go` is a byte equality check.
- `byte_citation` accepts both the longhand `path#start-end` and the
  shorthand `path#N` (single line). The local-first fragment uses the
  shorthand per the S1 codex-review fix
  (`go/internal/semanticqueue/README.md#10`).
- `Capabilities.Source` is `"default"` when no override file is present;
  callers that need to distinguish "loaded from file" from "default" must
  check this string, not the map contents.
- `RenderAll` is host-agnostic. Host-specific file shape, frontmatter
  schema, and the always-on-layer file live in the per-host adapter
  (claude_code.go, cursor.go, codex.go). Adding a new frontmatter field
  is local to the owning adapter.

## Related docs

- `docs/internal/skill-fragments-design.md` — the S1 design contract.
- `skill-fragments/` — the source-of-truth fragments.
- `expected/` — the roundtrip baseline committed next to the generator.
- `go/cmd/skillgen/` — the `gen` and `check` CLI.

## Evidence

No-Regression Evidence: `go test ./internal/extensions/skillgen/...
./cmd/skillgen/... -count=1 -race` covers fragment frontmatter parsing,
byte-citation normalization and comment-block emission, the host
registry, deterministic render across all three v1 hosts, the committed
`expected/` roundtrip baseline, drift detection (content mismatch and
missing file), capability override loading (default, file override,
malformed YAML, blank file), the AWS-disabled property test, and the
property-style "never panics" test across mixed capability configs.

No-Observability-Change: the package emits no telemetry. The `check`
subcommand is a build-time gate, not a runtime probe; it reads
filesystem state and exits non-zero on drift, which CI surfaces as a
failed job. No metric, span, log, or status surface is added.
