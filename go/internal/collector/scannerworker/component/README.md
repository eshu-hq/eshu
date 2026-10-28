# Scanner Component Runner

## Purpose

`component` adapts the three in-tree scanner analyzers (`image_unpacking`,
`sbom_generation`, `os_package_extraction`) to the
`collector-sdk/v1alpha3` stdio contract. Each analyzer ships as its own
binary (`go/cmd/scanner-component-image`, `-sbom`, `-osruntime`) with its
own component manifest and per-analyzer producer grants; the core extension
host launches one process per call. Refs #8878, #9385.

The runner is an analyzer executor in the same trust domain as the in-tree
worker, not a claim owner: the host validated the claim, the activation,
and the grants before launch. Claim-derived identity the SDK claim does
not carry is recomputed deterministically (the locator hash from the scope
ID via `scannerworker.TargetScopeForComponent`), and executor-local IDs
name the component, never the workflow. None of it crosses the wire.

## Ownership boundary

This package owns the stdio request/page envelope, the operator config
contract, per-analyzer manifests and required grants, page-bound
accounting, and failure-to-Result mapping. Analyzer truth stays in
`imageanalyzer`, `sbomgenerator`, and `osruntime`; claim/commit truth
stays host-side. It never touches Postgres, the graph, the reducer, or the
API/MCP surfaces, and it invents no protocol: it speaks the v1alpha3 shape
the host already decodes.

## Exported surface

- `Runner` — one stateless call: stdin request to stdout result page.
  Construct with the binary `ComponentID` and the stdio streams.
- `ManifestFor`, `RequiredGrants`, `AnalyzerFor` — per-analyzer manifests,
  the grants each manifest needs, and component-ID resolution.
- `IDImage`, `IDSBOM`, `IDOSRuntime`, `Version` — component identities and
  the manifest version.
- `DefaultStdoutLimitBytes` — the mirrored host stdout limit page bounds
  derive from.

## Dependencies

- `internal/collector/scannerworker` for claim input, target derivation,
  output validation, and page splitting.
- `internal/collector/ospackagevulnerability/osruntime`,
  `internal/collector/scannerworker/imageanalyzer`, and
  `internal/collector/scannerworker/sbomgenerator` for the analyzers.
- `internal/component` for manifests and producer grants.
- `internal/factenvelope` for internal-to-SDK fact projection.
- The pinned `eshu-sdk/collector` module for the base wire types; the
  v1alpha3 page envelope is mirrored at the JSON level until the pin
  carries pagination (same posture as the host mirror).

## Telemetry

Observability travels in-band: every call emits one SDK status record and
one structured stderr log line with the component identity, outcome, and
counts only. Claim values never appear in logs. The stderr line is a
debugging aid, not an operator signal: the host captures it in a bounded
64 KiB per-call buffer that only annotates the failure message (byte count
and truncation flag) and is never read on success. The operator-visible
signals are the per-page SDK status records the host records. Host-side
spans, metrics, and grant telemetry remain the operator surfaces.

Observability Evidence: the status record carries fact count and source
latency per page; the stderr line carries component, analyzer, state,
page, facts, and total for failure annotation only; host-side admission,
grant, and status recorders are unchanged, so no new operator surface was
added and none was removed.

## Performance Evidence

No-Regression Evidence: the SBOM repository-walk move is behavior
preserving — the moved walk suites run unmodified in the leaf package
(`go test ./internal/collector/scannerworker/sbomgenerator/`, green) and
the cmd wiring test proves the repointed worker caller end to end; the
target-scope derivation refactor is proven identical by
`TestTargetScopeForComponentAgreesWithWorkItem`.

Per-call cost of the new subprocess path (baseline: direct in-process
`imageanalyzer.Analyze` on a 60-package Alpine rootfs, arm64, Go 1.26.10:
~0.04 ms/op): one component invocation (process spawn, request decode,
same analysis, page emit at an 8 KiB bound yielding 5 of 61 facts on page
0) completes in ~6 ms wall with ~27 MB max RSS. That per-call figure is
measured; the multi-page total is an estimate from it, not a measured
end-to-end run: a hostile full-range result at that bound would need about
24 such calls (~150 ms extrapolated), which is negligible against the
10–15 minute analyzer timeouts and minute-scale claim leases. Pages are
stateless and resumable, so a spent budget fails the claim terminal
instead of holding it. No hot-path Cypher, graph write, queue, or lease
behavior changed: the host claim/commit path is untouched.
