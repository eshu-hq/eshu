# Python Reachability File Evidence Fold (#9860)

## Defect

After the #9781 fold bounded the active-evidence loader, the standalone
Python reachability stage still loaded every decoded `file` payload of each
PyPI-evidenced repository and kept all of them for the pass. A live QA pypi
batch spent 91.8 seconds in `load_python_reachability` holding 30,179 Python
file facts. The largest QA repository carries 23,329 file facts (977 MB raw
JSON, about 3.6 GB decoded); at 5x corpus size one repository alone would hold
about 18 GB in a single pass.

Root-Cause Evidence: the QA success log for the pypi pass shows
`sub_signal_load_python_reachability_seconds=91.79` with
`sub_signal_python_reachability_facts=30179`, and the issue's OOM profile
attributes 0.89 GB (11%) of the 7.8 GB heap to this stage's whole-payload
`ListFactsByKind` load path. The loader (`loadPythonReachabilityEvidenceFacts`)
called `factload.LoadFactsForKindAndPayloadValue` per repository scope and
appended every envelope.

## What changed

The stage now pages the `repo_id` payload-value read through a new
`FoldPythonReachabilityFiles` store method (cursor over `observed_at`,
`fact_id`, one read-only repeatable-read snapshot) and folds each row into a
Python digest, dropping the envelopes. The digest shares the digest fact kind
so index builders, envelope counting, and evidence-ID expansion treat it like
the active-evidence digest, with a distinct fact ID so the unique-append keeps
both. Folded files count like retained envelopes did, so pass accounting does
not drop. Loaders without paging fall back to a whole load folded in memory
(test fakes; both production loops pass a `*FactStore` and page).

Folded bytes are observed, not charged: like the #9781 active fold, folding
bounds memory by dropping the payloads, so only retained bytes trip the byte
budget, and the stage retains nothing.

## Benchmark Evidence

Retained-vs-folded theory bench, decode-from-raw Python parser payloads
(imports, calls, decorator buckets, SCIP edges), live heap after GC,
`-benchtime=1x`, go1.26.10, same host:

| rows | retained live heap | folded live heap |
| --- | --- | --- |
| 2,000 | 8.48 MB (4,241 B/row) | 0.38 MB (189 B/row) |
| 10,000 | 42.2 MB | 1.65 MB |
| 20,000 | 84.4 MB (4,222 B/row) | 3.43 MB (172 B/row) |

About 22-25x less live heap, scaling linearly with row count; the fold grows
with distinct repository/package pairs, not bytes. Decode churn is comparable
(fold adds about 18% allocs over plain decode). At QA row sizes (about 42 KB
raw), the retained column scales to about 3.4 GB per pass, matching the
issue's estimate.

## No-Regression Evidence

Parity is pinned by loader-level tests with `DeepEqual` indexes:
`TestLoadPythonReachabilityEvidenceFactsFoldsPagedFiles`,
`TestLoadPythonReachabilityEvidenceFactsKeepsBothDigests` (both digests survive
the unique-append; mutation-verified),
`TestLoadPythonReachabilityEvidenceFactsFallsBackWithoutPager`, and
`TestPythonFoldTheoryParity` (decorator path included). The store seam is
pinned by `TestFoldPythonReachabilityFilesPagesAndFolds` (two page reads for
800 allowlisted files over 500-row pages),
`TestFoldPythonReachabilityFilesReadsSnapshotVariant` (mutation-verified), and
`TestFoldPythonReachabilityFilesRejectsBlankKey`. Core and postgres suites
pass, including with `-race`; golden-corpus mirror passes.

## Observability Evidence

New success-log sub-signals per pass: `python_reachability_files_folded` and
`python_reachability_payload_bytes_folded`; `python_reachability_facts` now
counts folded files. No new Prometheus instruments: the existing
`eshu_dp_supply_chain_impact_active_evidence_payload_bytes` histogram is
unchanged, and folded bytes stay out of `budget.bytesUsed` per the #9781
precedent. After the roll, a pypi pass should show files folded in the
thousands with retained bytes near zero and no `partial` caused by this stage.

## NOT_CHECKED

- Post-merge QA memory readback for the Python stage after deploy.
- A single production-scale replay above 20,000 files in one repository.
