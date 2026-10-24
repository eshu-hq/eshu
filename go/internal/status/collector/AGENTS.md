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
`terminal fetch failure: <class> (<operation>)`. Keep it bounded: the class
must pass `BoundedFailureClassForKind` (a local per-kind allowlist in
`terminal_failure_classes.go`, deliberately not an import of any collector
runtime package) and the operation must pass `BoundedFailureOperation`;
anything else reads as `unknown`. Every kind reports the nine common classes
(the five provider classes plus `attempt_budget_exhausted`, `identity_mismatch`,
`collect_failure`, and `commit_failure`) and its own classes from
`kindFailureClasses`; a class only another kind owns reads `unknown` for it.
A kind that is not first-party is hosted by the extension host: it reports the
common classes and the host's own, and every other class matching the host's
pattern collapses to `extension_defined`, so the set never grows with third-party
text. A class added to any set must also land, in the same change, in both
OpenAPI `last_failure_class` enums
(`TestOpenAPITerminalFailureFieldsMatchStatusContract` in `query`), the class
table in `docs/public/reference/http-api/collector-terminal-failure.md`
(`TestTerminalFailureDocTableMatchesTheCode`), and the cardinality pin
(`failureClassCardinality`), and stay aligned with the security-alert read's
class set, which pins the nine common classes for that one kind.
`TestBoundedFailureClassesMatchTheCollectorWriters` ties each kind's set to the
writer's constants; it is the only place this package names collector runtime
packages, and only in a test. Never add the
failure message or provider status code to `TerminalFailure`, `RuntimeStatus`,
`PromotionProof`, or their JSON. The reader (`storage/postgres`) supplies only
failures of enabled, non-deactivated instances inside the instance's window
(3 x `scan_interval`, 30 minute floor) with no later completed work item of the
same instance for the same scope, so clearing and aging out are the reader's
job, not this package's. Every `TerminalFailure` is terminal by construction:
the reader selects rows by the `failed_terminal` status, never by the
`last_failure_terminal` flag, because the scanner worker dead-letters without
setting that flag. Do not add a per-row terminal gate back. A failure for an
unregistered or disabled instance never changes health.

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
