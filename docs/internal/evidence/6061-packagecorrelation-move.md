# #6061 step 1 — packagecorrelation move: no-regression evidence

No-Regression Evidence (#6061): the packagecorrelation move (the
`package_*` family `git mv` root → `go/internal/reducer/packagecorrelation/`,
6 string helpers evicted to `payloadcore` with one-line forwarders,
followed by a filename-only round stripping the `package_*` prefix per
the repo naming rules) is behavior-preserving. No logic changed:
fact-kind consts alias the identical `factschema` values, payload
construction moved verbatim, and every requalified caller names the same
symbol through its new package.

Baseline: `origin/main` `3f7d72b55` test state for the touched packages.
After the move commits (`e8c890717` + `bbacfa4b8`), measured by the
coordinator promotion preflight (`/tmp/move1-prepr.log`):

Addendum (short-rename round, filename-only, no symbol touched):
`go build ./...` rc=0, `go vet ./internal/reducer/...` rc=0,
`go test ./internal/reducer/... -count=1` rc=0 with zero non-ok
packages. The telemetry-coverage row for the root-compat test seam
already names the new short filename; the citation baseline was
regenerated for the renamed tree.

- `go test ./internal/reducer/... -count=1`: `ok` for
  `go/internal/reducer` (3.6s), `.../packagecorrelation` (1.5s),
  `.../payloadcore` (2.0s), `go/cmd/reducer` (1.1s); zero `FAIL` /
  `--- FAIL` / panics across the whole log.
- B-7 graph-truth node-presence checks all `[PASS]` (Repository 31,
  Directory 53, File 164, Function 276, plus platform/CI nodes);
  path-triggered live lane PASS (391s); race lane PASS (88s).
- Backend: local preflight gates (no live-backend delta claimed here;
  CI lanes re-prove on push).

Why safe: the diff is renames plus import requalification. The one
semantic-adjacent change — 6 generic string helpers now living in
`payloadcore` — is covered by the same unit tests calling through the
forwarders, and a `-gcflags=-m` differential showed inlining preserved
at every base inlined site (see eviction commit message).

No-Observability-Change: no metric name, label, span, or log line
changed. The moved `emitProvenanceEdgeCounter(... "submitted" ...)`
call and the `eshu_dp_provenance_edges_total` semantics are covered by
the telemetry-coverage rows added in this PR (packagecorrelation test
seam, manifest match, versioned test support); the owning pass stays
bounded by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`.
