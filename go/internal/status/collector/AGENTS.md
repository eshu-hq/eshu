# Agent instructions: internal/status/collector

Scope: `go/internal/status/collector` only. The parent package's instructions
in `go/internal/status/AGENTS.md` still apply.

## The one rule that matters here

This package must not import `internal/status` (the root) or any sibling
leaf, with exactly one declared exception: it may import
`internal/status/cloud` to fold `cloud.AWSScanStatus` rows into
`RuntimeStatus`. The root aggregates every family leaf into
`RawSnapshot`/`Report`, so root and leaves both import `collector`; an
import in the other direction is an import cycle. If you find yourself
needing a type from the root here, the type is in the wrong place — move it
down, do not reach up.

## The Report coupling

`RuntimeStatuses`, `PromotionProofs`, and the catalog's `PresentCatalog`
helper take the collector-native `Evidence` value (coordinator instances, AWS
scans, vulnerability sources, collector fact evidence) instead of the root
`Report` type, because this package cannot import root `Report`. Callers
project with `report.CollectorEvidence()` and call
`collector.RuntimeStatuses(report.CollectorEvidence())` and
`collector.PromotionProofs(report.CollectorEvidence(), opts)` directly; the
transitional `status/compat_collector.go` forwarders were deleted in #6949
batch 7. Do not reintroduce a `Report`-shaped signature here — that would be
an import cycle. When changing these signatures, do not silently narrow
behavior — diff the derived `RuntimeStatus`/`PromotionProof` rows before and
after on a fixture snapshot.

## Terminal failure evidence

`Evidence.TerminalFailures` degrades a coordinator-registered instance and
fails its promotion proof with the blocker
`terminal provider failure: <class> (<operation>)`. Keep it bounded: the class
must pass `BoundedFailureClass` (a local allowlist, deliberately not an import
of any collector runtime package) and the operation must pass
`BoundedFailureOperation`; anything else reads as `unknown`. Never add the
failure message or provider status code to `TerminalFailure`, `RuntimeStatus`,
`PromotionProof`, or their JSON. The reader (`storage/postgres`) supplies only
failures with no later completed work item for the same scope, so clearing is
the reader's job, not this package's. A failure for an unregistered or disabled
instance never changes health.

## Exporting the render/clone/JSON surface

Every render, clone, and JSON-projection helper in this family
(`renderCollectorBackpressureLines`, `renderCollectorRuntimeStatusLines`,
`renderCollectorPromotionProofLines`, `renderVulnerabilitySourceLines`,
`renderCollectorGenerationDeadLetterLine`, `cloneCollectorBackpressure`,
`cloneVulnerabilitySourceStates`, `cloneCollectorGenerationDeadLetterSnapshot`,
`collectorBackpressureJSONRows`, `collectorRuntimeStatusesJSON`,
`collectorPromotionProofsJSON`, `vulnerabilitySourcesJSON`) is called
directly from root `status.go`, `json.go`, or `coordinator.go` and is
currently unexported. Export the whole set — a partial export leaves root
with a broken call site, not a compile error you can defer.

`vulnerabilitySourcesJSON` returns `[]vulnerabilitySourceJSON`, and that
struct type is defined in root `json.go`, not in `json_cloud.go` (the file
this function is moving from). Move the type here too (export it as
`VulnerabilitySourceJSON`) before wiring the function — otherwise this
package will not compile once it excludes root.

## Changing the wire helpers

`BackpressureJSONRows`, `RuntimeStatusesJSON`, `PromotionProofsJSON`, and
`VulnerabilitySourcesJSON` render operator-facing status JSON consumed by the
status HTTP surfaces and MCP status tools. Their struct tags and time
formatting are the published contract.

Before changing any of them, know that they are locked by byte-for-byte
goldens: `internal/status/testdata/render_json_golden.json`,
`render_text_golden.txt`, and the explicit dotted-key-path list in
`render_json_key_paths_golden.txt`. A failure there is a real API break to
justify, not a golden to regenerate reflexively.

## Verification

```bash
cd go && go test ./internal/status/... -count=1
```

Run the recursive `./internal/status/...` path, not the bare package — the
goldens that prove this leaf's wire contract live in the parent package's
tests. Also run `./internal/query/...` when touching `RuntimeStatuses` or
`PromotionProofs`, since the collector-readiness and evidence-bundle read
paths there call these functions through the root forwarder.
