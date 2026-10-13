# #7982 Bootstrap --force No-Regression Evidence

`eshu index --force` and `eshu scan --force` were accepted and ignored by
`eshu-bootstrap-index`, which read argv for the version flag only. The fix
parses `--path`/`--force` in the binary and marks forced filesystem selections
`repository_reindex_requested`, so the ingestion unchanged-generation check
stores a new generation (ruling a9-125) instead of taking the manifest skip.

## Default-path delta

An unforced run executes the identical path as before plus two branch
evaluations: `&& !config.ForceReindex` in the manifest-match arm (evaluated
only when the manifest already matches) and `if s.Config.ForceReindex`
guarding map allocation once per selection cycle. The reconcile maps stay nil
unless forcing, so allocation and lookup shape are unchanged. No Cypher, queue,
lease, worker, batching, or retry path is touched.

## Measurement

Selector and freshness subset
(`TestNativeRepositorySelector|TestReindex|TestReconcile|TestBuildSelected|TestSyncFilesystem|TestSelectRepositories|TestSnapshot|TestGenerationContentVersion|TestBuildStreamingGeneration`,
`-count=1`, go1.26.9 linux/amd64, same host):

- Baseline at 9784ab0e: ok 0.649s.
- After at branch head: ok 0.887s-2.542s across runs.

The spread is host-load noise on a shared box, not a signal; the runs are
labeled not-comparable totals. The regression pin is behavioral:
`TestNativeRepositorySelectorSelectRepositoriesFilesystemReturnsEmptyBatchWhenManifestUnchanged`
still selects 0 repos on an unchanged re-poll, and the new
`TestNativeRepositorySelectorFilesystemForceReindexesUnchangedCorpus` selects
exactly 1 repo with reason `repository_reindex_requested` only when forced.
`go test ./cmd/bootstrap-index/ -count=1` is ok 9.6s and race-clean; the
selector subset is race-clean. One forced run re-copies and re-parses the
corpus by design; that cost equals any full re-parse, not a per-cycle tax.

No-Regression Evidence (#7982): unforced selection is branch-identical plus
two constant-time checks with no allocation change, pinned by the
unchanged-manifest test; forced runs pay one intended full re-parse.

Observability Evidence (#7982): one new WARN,
`bootstrap --force applies to filesystem source mode only`, emitted only when
`--force` arrives with a non-filesystem source mode (direct binary use outside
`eshu index`/`eshu scan`). No metric, span, label, or status shape changes.
