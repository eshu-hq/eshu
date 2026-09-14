# NornicDB v1.3.2 alignment evidence (#6162)

## Artifact identity

Eshu's Compose, Helm, R-5 replay, and Kubernetes governance-proof defaults use
one immutable upstream artifact:

```text
timothyswt/nornicdb-cpu-bge:v1.3.2@sha256:a47ae7eadc80229d3109ade7a57dfc1f1504b7586798859e2b2ac6fc38897440
```

The upstream `v1.3.2` tag resolves to commit
`d2c8a9b47d67887506fb112a30144115caea77ed`. Its OCI index contains:

| Platform | Manifest digest | Validation in this change |
| --- | --- | --- |
| `linux/amd64` | `sha256:4256d970a1aad702b85fbd4dafa9299bb274d82090ae90eb59b88d48e9291adc` | Docker runtime and live Eshu proof |
| `linux/arm64` | `sha256:e443f176095d4b7b647fec73f75527c5d634c414dd5645ea36a206187aacab02` | Manifest inventory and rejecting verifier fixture only; no arm64 runtime claim |

The amd64 executable has SHA-256
`0393de0b61ec3c901a5f476eb3f68e398198db4f593048f7ab76e23690e2d2c7`.
The published image has no OCI labels and reports `NornicDB v1.3.1` because
upstream's v1.3.2 tag retained `1.3.1` in `pkg/buildinfo/VERSION`. The immutable
index digest, platform child digest, selected platform, upstream tag commit,
and executable hash identify the artifact; the version banner alone does not.

## Storage and rollback boundary

This change makes no in-place storage-format compatibility claim. The v1.3.1
alignment branch never merged, so `origin/main` still names the legacy Compose
volume `nornicdb_data` and Helm claim `<release>-nornicdb-data`. The v1.3.2
defaults select a fresh `nornicdb_v132_data` Compose volume and retained
`<release>-nornicdb-v132-data` Helm claim. An existing legacy claim fails closed
until the operator explicitly acknowledges fresh-volume migration. The legacy
claim remains preserved and unmounted by the v1.3.2 deployment so rollback can
restore it without an older binary opening v1.3.2-written storage.

## Local proof on 2026-09-14

Commands ran from the feature worktree with `GOTOOLCHAIN=go1.26.6`, `CC=clang`,
and isolated per-worktree Go caches.

| Contract | Command | Result |
| --- | --- | --- |
| Runtime defaults and storage contracts | `go test ./internal/runtime -run '^(TestComposeNornicDBImage|TestHelmNornicDB)' -count=1` | pass |
| Snapshot-conflict classifier regression | `go test ./internal/storage/cypher -run '^(TestClassifyTransientNeo4jErrorPrioritizesNornicDBWriteConflict|TestClassifyTransientNeo4jErrorRejectsV131ConflictNearMisses|TestRetryingExecutorV131WriteConflictUsesBoundedMetricReason)$' -count=1` | pass |
| Shell verifier mirrors | `bash scripts/test-verify-replay-tier.sh`, `bash scripts/test-verify-k8s-two-team-governance-proof.sh`, and `bash scripts/test-k8s-two-team-governance-provenance.sh` | pass |
| Required live backend conformance | `bash scripts/verify_backend_conformance_live.sh` against the exact v1.3.2 amd64 artifact | pass; supported corpus, write-conflict retry, stale-attribute removal, and heterogeneous CloudResource batch contracts |
| R-5 replay | `bash scripts/verify-replay-tier.sh` | pass; offline graph truth and tombstone/idempotent replay completed in 77 seconds; SQL UNION branch proof completed in 21 seconds |
| B-7 golden corpus | `bash scripts/verify-golden-corpus-gate.sh` | 562 pass, 0 required failures, 0 advisory warnings; 135 seconds total |
| Ifá restart slice | `bash scripts/verify-ifa-fault-injection.sh --shard 4/21` | pass; baseline and restart graph digests matched with zero dead letters and cross-scope edges |

The accepted backend-conformance run omitted the explicitly opt-in value-flow
cloud-sink pair, as the verifier reports. A separate opt-in run returned zero
rows on both fresh v1.3.1 and v1.3.2 stores. This is an existing unsupported
query-shape boundary, not a v1.3.2 regression, and this change makes no support
claim for that pair.

## Retry and concurrency contract

PR #6657's prior remote head observed
`Neo.TransientError.Transaction.Outdated` with `conflict detected: edge ...
changed after transaction start`. The retry classifier recognized the older
`conflict:` spelling, so the retry remained safe but used the generic
`transient_error` telemetry reason instead of `write_conflict`. The classifier
now accepts the exact typed v1.3.1-and-v1.3.2 spelling while rejecting untyped,
wrong-code, incomplete, and reordered lookalikes. The live v1.3.2 relationship
snapshot test forced the conflict, retried once within the existing budget, and
converged.

The contested resource is one relationship snapshot. The backend transaction
is the conflict boundary; the enclosing executor attempt is the retry boundary.
The write remains idempotent through the existing canonical identity, unrelated
concurrency is unchanged, and no worker-count or serialization workaround is
introduced.

## Golden-corpus and restart truth

B-7 replayed the 31-repository, 19-collector-source corpus through the real
pipeline on v1.3.2. All 562 graph, HTTP, MCP, queue, and timing assertions
passed in 135 seconds. The first drain reached zero residual and dead-letter
work; all required reducer intents, cross-scope completion events, and
unroutable work were terminal or absent.

The Ifá `4/21` slice selected only `cell_baseline` and
`cell_restartbackend`. Baseline completed in 14 seconds and the restart cell in
19 seconds. The restart sentinel fired during the drain, both cells reached
zero dead letters, and each checked 627 GCP relationships with `cross_scope=0`.
Both produced the canonical digest:

```text
280a882458096e6813cb4f3d7c6552b92860c5b4c2a6e597ee5cc69c462f8052
```

## Live Kubernetes proof

`bash scripts/run-k8s-two-team-governance-proof.sh --artifacts <temporary-dir>`
rebuilt the Eshu chart and seed images from implementation commit `390901945`,
then ran on a disposable single-node `linux/amd64` Minikube v1.39.0 /
Kubernetes v1.37.0 cluster. Provenance recorded that commit, the immutable
v1.3.2 index, and its honest `NornicDB v1.3.1` banner. The verifier passed:

- unauthenticated API and MCP rejection;
- admin visibility of both seeded repositories;
- one-repository visibility and exact own-selector results for each team;
- cross-scope omission and matching non-disclosing selectors;
- API/MCP parity;
- four applied NetworkPolicy objects with restricted egress; and
- exact backend index and `linux/amd64` platform identity.

The six public-safe JSON artifacts were normalized and hashed in this fixed
order:

```bash
for file in admin.json team-a.json team-b.json unauth.json network-policy.json provenance.json; do
  jq -S . "${artifacts_dir}/${file}"
done | sha256sum
```

The checksum was
`sha256:60c77bfa586cbc386264fa7c4a1522f18032578571890480fd8fa231ebb2d4b5`.
The proof cleaned up its Helm release and namespace. It is local single-node
evidence, not a managed-cluster, multi-node, arm64-runtime, or hostile
packet-flow claim.

## Live storage migration and rollback

Two disposable Minikube namespaces began from the exact `origin/main` chart at
`e119552a07dc4ff9109fae2881a53dfe972a13d4`, with its v1.2.3 image and legacy
`<release>-nornicdb-data` claim. Each wrote and read a sentinel on that claim.

The defaulted-storage-class case and the explicit `standard` case both proved:

1. an unacknowledged upgrade failed without advancing Helm revision 1, changing
   the old deployment image, changing the legacy PVC UID, losing the sentinel,
   or creating the v1.3.2 claim;
2. an acknowledged upgrade advanced to revision 2, ran the exact v1.3.2 image,
   mounted only `<release>-nornicdb-v132-data`, preserved the legacy PVC UID,
   left the legacy claim unmounted, and found no legacy sentinel on fresh
   storage; and
3. rollback advanced to revision 3, restored the exact v1.2.3 image and legacy
   claim with its original UID and sentinel, and retained the fresh v1.3.2 claim
   unmounted.

The explicit-class case additionally rejected acknowledged upgrades requesting
`2Gi` against the live `1Gi` claim or storage class `bogus` against `standard`.
Both failures left Helm at revision 1 and preserved the old image and PVC.
Every test release and namespace was removed after proof.

No-Regression Evidence: This alignment makes no cross-version speedup claim.
On the final immutable v1.3.2 amd64 artifact, the required live conformance and
retry suite passed. R-5 completed its projection/replay phase in 77 seconds and
its SQL-table branch proof in 21 seconds; B-7 completed in 135 seconds; and the
Ifá baseline/restart cells completed in 14 and 19 seconds with identical graph
digests. These results prove the supported contracts stay within their existing
gate boundaries; they are not presented as a speedup over v1.3.1.

Observability Evidence: No metric instrument, label key, span, log field,
status schema, alert, dashboard, worker, queue stage, or API/MCP field changes.
The exact snapshot-conflict shape now uses the existing `write_conflict` reason
instead of `transient_error` in `eshu_dp_neo4j_deadlock_retries_total`; retry
count, budget, backoff, span/log fields, and reason cardinality are unchanged.
Operators retain existing queue residual/dead-letter signals, graph-truth
gates, container health checks, and immutable-image provenance.
