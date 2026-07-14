# #5234 — Reject blank `ci.run` `run_id` before indexing an image anchor

Follow-up to [#4685](https://github.com/eshu-hq/eshu/issues/4685) (codex review
on PR #5233). The typed container-image-identity decode in
`go/internal/reducer/container_image_identity_typed_evidence.go` accepted a
present-but-blank `run_id` (an explicit empty string is a valid decode for
`decodeAndValidate`), and `cicdRunKeyFromParts` returns
`trimmed(provider):trimmed(run_id):attempt`, which is never empty. A malformed
`ci.run` was therefore indexed under a key like `github_actions::1` and let a
matching malformed `ci.artifact` inherit its repository anchor — the exact join
the pre-typing raw `cicdRunKey` refused. The fix guards blank `provider`/`run_id`
explicitly before indexing, mirroring the empty-string guards the sibling
aws/azure/gcp `image_reference` paths already keep.

## No-Regression Evidence:

- **Change shape:** one `strings.TrimSpace(run.Provider) == "" || strings.TrimSpace(run.RunID) == ""`
  check per `ci.run` envelope on the container-image-identity map-build path.
  This is not a Cypher, graph-write, queue, lease, or batching hot path — it is
  an in-memory anchor index built once per intent.
- **Baseline / input shape:** the #4685 golden-corpus gate on the NornicDB
  backend (`gc4685b`, 20-repo corpus) — `417 pass, 0 required-fail,
  0 advisory-warn`, `PASS: B-7 golden corpus gate green`, no B-12 snapshot drift.
- **After:** unchanged. Valid facts carry a non-blank `run_id` and take the
  identical index path as before; the guard only diverts malformed
  blank-identity facts, which no valid fact — and the golden corpus — contains.
  So the projected graph for valid input is byte-identical and the golden-corpus
  result is not re-run-dependent.
- **Terminal counts:** anchor-map entries for valid runs are unchanged; a blank
  `run_id` run contributes zero entries (was one malformed entry before the
  fix). Proven by `TestContainerImageCIRunsSkipsBlankRunID`, which indexes
  `github_actions::1 -> repo-api` without the guard and zero anchors with it.

## No-Observability-Change:

The blank run is skipped through the same `continue` path as the pre-existing
`key/repositoryID == ""` guard. No metric, span, or structured log is added or
changed. `eshu_dp_reducer_input_invalid_facts_total` continues to cover true
decode-time quarantines (a `ci.run` with an *absent* required key still
dead-letters `input_invalid` via `partitionDecodeFailures`); a present-but-blank
value is a valid decode that is silently skipped, matching the sibling
image_reference paths' empty-string guards.
