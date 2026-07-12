# Evidence: parallel largest-first scheduling count walk (#4878)

Issue [#4878](https://github.com/eshu-hq/eshu/issues/4878) reported the repo
tree walked twice before parsing (scheduling count walk vs snapshot
discovery). Quantification on a real 911-repo / 412,265-file corpus showed
the proposed walk merge is the wrong shape (discovery serial sum 18.5s far
exceeds the 7.5s count-walk pre-phase, and a merged walk must descend the
count walk's superset tree), while the count walk's serial placement — it
blocks snapshot worker launch in `startStream` phase 2 — is the actual cost.
This change parallelizes the count loop in `resolveRepositories` (bounded by
`runtime.NumCPU()`, fill-by-index, identical stable sort) and leaves
discovery untouched.

 Performance Evidence: prove-theory shim (OLD verbatim serial pair-build vs
 NEW bounded-parallel fill, identical `sort.SliceStable`) on the 911-repo /
 412,265-file local corpus, 3 alternating iterations, warm cache: OLD serial
 7.49s / 5.96s / 4.22s vs NEW parallel (18 workers) 1.79s / 1.33s / 1.37s —
 3.1-4.5x, saving 2.8-5.7s of dead time before the first snapshot worker
 starts, per selection batch. Equivalence 0/0 on all 911 positions (repo
 order + counts byte-identical) across all iterations, plus run-to-run
 determinism of the parallel shape, a 30-repo synthetic equal-count tie
 block preserving input order, and the empty batch. Race detector pass on
 the full-corpus shim: 0 data races. Local proof of the finished change:
 the real `resolveRepositories` on the same corpus/machine completed in
 1.90s / 1.52s / 1.39s (vs 7.49-7.66s serial baseline) with the
 largest-first invariant asserted and the identical 412,265 file total.

 Observability Evidence: `startStream` now logs
 `collector scheduling scan complete` with `repository_count` and
 `scheduling_scan_duration` before snapshot workers launch, so an operator
 can attribute pre-parse dead time to the scheduling scan directly. No new
 metric instruments or pipeline stages; existing spans and metrics are
 unchanged.

Scheduling output is unchanged by construction: each worker writes only its
own pre-allocated pair index, so the pre-sort input order matches the serial
loop exactly and the stable sort's equal-count (input-order) tie behavior is
preserved. `sourceRunID` derives from the input-order path list built before
sorting and is untouched.

The walk-merge variant of #4878 is recorded as disproven for the current
corpus shape; discovery inside snapshot workers overlaps parse and its
duplicate readdir is CPU-only, not wall time.
