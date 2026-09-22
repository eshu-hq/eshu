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

## The unresolved Report coupling

As of the #6775 nest, `RuntimeStatuses`, `PromotionProofs`, and the catalog's
internal `presentCollectorCatalog` helper still read the root `Report` type
directly (`Report.Coordinator`, `.AWSCloudScans`, `.VulnerabilitySources`,
`.CollectorFactEvidence`). That cannot compile once this package excludes
root. Before treating any of these three as done:

1. Give each a collector-native signature that takes the raw ingredient
   slices/pointers instead of `Report`.
2. Add (or update) `status/compat_collector.go` with a `Report`-shaped
   forwarder under the original root name, since `internal/query`'s
   collector-readiness and evidence-bundle surfaces call
   `status.CollectorRuntimeStatuses(report)` and
   `status.CollectorPromotionProofs(report, opts)` directly today.
3. Do not silently narrow either function's behavior while changing its
   signature — diff the derived `RuntimeStatus`/`PromotionProof` rows before
   and after on a fixture snapshot.

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
