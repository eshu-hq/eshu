# Evidence: reducer compat alias removals (#6626 batch 6, bucket 4)

## What changed

One reducer-root compat alias with live callers is deleted from the
`kubernetes_correlation_compat` stanza in `go/internal/reducer/compat_cloud.go`
(the stanza marker stays with a retirement note, per bucket 3 convention):

- `KubernetesCorrelationNodesNotReadyFailureClass` (canonical
  `kubernetescorrelation.KubernetesCorrelationNodesNotReadyFailureClass`)

Three caller files are mechanically repointed at the canonical spelling with
identical assertions (same failure-class string value, same test expectations):

- `go/internal/storage/postgres/reducer_queue_readiness_sql.go` (exempt-list
  entry; adds the `kubernetescorrelation` import)
- `go/internal/storage/postgres/reducer_queue_kubernetes_correlation_readiness_test.go`
  (two uses; adds the `kubernetescorrelation` import)
- `go/internal/reducer/kubernetes_correlation_readiness_seam_test.go` (two
  uses; file already imported `kubernetescorrelation`)

The now-unused `kubernetescorrelation` import is dropped from
`compat_cloud.go`. A guard test,
`TestBucket4KubernetesCorrelationAliasRetired` in
`compat_alias_retirement_bucket4_test.go`, scans the whole reducer package
directory for any re-declared alias (bucket 3 pattern).

## Why this stanza

Fresh census on head `08f30971be`: 332 alias symbols parsed across
`compat_cloud.go`, `compat_correlation.go`, `compat_decode.go`, and
`compat_projection.go`, with no zero-caller aliases. The
`kubernetes_correlation_compat` stanza is the smallest whole stanza: 1 alias
with code callers in 3 files (5 use sites). Comment-only mentions in
`gcp_relationship_materialization.go` and
`ec2_instance_identity_materialization.go` name the concept in prose and need
no edit. Excluded per bucket scope: `SharedProjectionIntentRow` (goes last as
the coordinated slice), `compat_decode.go` decode-seam stanzas plus
`codedataflow_input_invalid_test.go` (claimed by open PR #8513 / bucket 3),
`go/internal/reducer/crossplane/` files (#8496), and sweep/orphan and
phase/group leaf paths (#8518).

## Why no regression

No-Regression Evidence: the retired alias was a same-valued `const`
forwarder, so every repointed use evaluates the identical
`kubernetes_correlation_nodes_not_ready` literal the storage contract
matches on; the canonical literal is unchanged
(`kubernetescorrelation/kubernetes_correlation_materialization.go:67`) and
remains the only definition. Proof on this head:
`TestBucket4KubernetesCorrelationAliasRetired` failed before the removal
(`compat_cloud.go still declares ...`) and passes after;
`go build ./internal/reducer/... ./internal/storage/postgres/...` exit 0;
`go test ./internal/storage/postgres/ -run 'KubernetesCorrelation' -count=1`
ok; `go test ./internal/reducer/ -run 'KubernetesCorrelation' -count=1` ok.
No graph query, queue predicate, worker, lease, batch, runtime knob, or
storage contract changes: the exempt-list entry keeps its position and value
in `nonCountingReducerRetryFailureClasses`.

## Observability

No-Observability-Change: no metric, span, status field, route, or log
contract changes; nothing emitted or consumed the alias spelling, and the
failure-class string the queue matches on is byte-identical.
