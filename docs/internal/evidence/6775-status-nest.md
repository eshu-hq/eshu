# 6775 — nesting `go/internal/status` into family leaves

Issue: #6775 (part of #6053). Base: `origin/main` 1f777b5e4.

## What changed, and why this note exists

`go/internal/status` held 45 non-test `.go` files against the 40-file dirgate
cap. It is split into eight leaf packages (`shared`, `collector`, `cloud`,
`semantic`, `tfstate`, `queue`, `generation`, `changedsince`), with the root
keeping type aliases and forwarders so no caller outside the package changes.

The hot-path evidence gate triggers on this change because two of the new
directory names — `status/collector/` and `status/queue/` — match its path
list, and `status/collector/promotion_catalog.go` is flagged as a hot file.
That trigger is a path match, not a behavioral one: this change moves code
between packages and renames identifiers. It adds no query, no Cypher, no graph
write, no worker, no lease, no claim, no batching, and no concurrency knob.

## Baseline, after, and input shape

Baseline (`origin/main` 1f777b5e4) and after (this branch) are compared on the
same machine, same Go toolchain (go1.27.1), same package, with no backend: the
code under test performs no I/O.

- Input shape A — a fully populated `RawSnapshot`: every one of its **27
  exported fields** set, each slice non-empty, each pointer non-nil.
- Input shape B — a zero `RawSnapshot{}`, which is the state an operator sees
  on a freshly started stack before any facts exist.

Terminal counts: not applicable. This package reads no queue and writes no
rows; it projects an in-memory snapshot into an operator report. The queue
*counts it renders* come from the caller's snapshot and are untouched.

## No-Regression Evidence

No-Regression Evidence: `cd go && go test ./internal/status/... -count=1` — ok,
and `go test -list '.*' ./internal/status/...` discovers **117** tests (116
before this change, plus the empty-snapshot lock added here), proving the
repointed tests still run rather than merely compile.

No-Regression Evidence: `cd go && go build ./... && go vet ./...` — both exit 0
across the module, covering the **149 files outside this package that import
`internal/status`** and the **154** distinct symbols they reference through the
compat surface. (A first census said 170/167 — it counted this package's own
`status_test` files, which import it under the external-test-package pattern.
The module build proves the surface holds regardless of the count.)

No-Regression Evidence: byte-for-byte wire comparison against `origin/main` for
both input shapes. `RenderJSON` and `RenderText` output is identical, asserted
by committed goldens at `go/internal/status/testdata/render_json_golden.json`,
`render_text_golden.txt`, `render_empty_json_golden.json`,
`render_empty_text_golden.txt`, plus an explicit assertion over all **363**
dotted key paths in the decoded document
(`render_json_key_paths_golden.txt`). The empty-shape goldens were generated
from a throwaway worktree at `origin/main` 1f777b5e4, not from this branch, so
they encode pre-change behavior rather than post-change behavior.

No-Regression Evidence: mutation controls prove those goldens bite rather than
pass vacuously. Renaming one json tag in `json.go` (`collector_instance_id` →
`mutated_id`) fails the lock, naming `vulnerability_sources.mutated_id`; making
`domainBacklogsJSON` return nil instead of an empty slice fails the
empty-snapshot lock. Both reverted clean (`git diff --stat` empty).

No-Regression Evidence: whole-module lint,
`bash scripts/dev/precommit-go.sh lint-all` — 0 issues.

## Observability Evidence

Observability Evidence: No-Observability-Change: this change adds no metric,
span, log key or status field, and removes none. It is a package reorganisation
with a compat surface; every operator-facing signal is byte-identical, which is
exactly what the wire goldens above assert. No `internal/telemetry` file is
touched, and no leaf imports `database/sql`, `net/http`, `os` or `io` — verified
by search, which is what makes the "no I/O" claim above checkable rather than
asserted.

## Why the change is safe

The dependency direction is one-way and machine-checked: the root aggregates
every leaf into `RawSnapshot`/`Report`, so root imports leaves and **no leaf
imports the root** (13 × `shared`, 2 × `cloud` by `collector`, zero root
imports). A leaf reaching upward would be an import cycle and would not compile.

The one behavioral edit is a signature narrowing. Three collector functions took
the whole `Report`; they now take a `collector.Evidence` the leaf owns, built by
`collectorEvidence()` at the root. The old `if report.Coordinator != nil` guard
becomes a nil `Instances` slice, which ranges zero times exactly as the guard
did. Field mapping was checked one-for-one against the pre-change source, and
the goldens cover both the populated and empty cases that distinguish them.

Backend/version: none exercised — no backend is involved. The Go toolchain is
go1.27.1.
