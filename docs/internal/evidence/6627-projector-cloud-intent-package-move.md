# #6627 projector cloud-intent package move

## Scope and classification

This change relocates and renames three in-process projector intent packages:
`cloudinventory` to `cloud/inventory`, `awscloudruntimedrift` to
`cloud/runtime/drift/aws`, and `multicloudruntimedrift` to
`cloud/runtime/drift/multi`. It also adds documentation-only `cloud`,
`cloud/runtime`, and `cloud/runtime/drift` namespaces. Package declarations,
import paths and aliases, exported builder names, filenames, tests, registry
citations, and documentation change. Executable trigger logic and reducer
intent values do not.

This is not a performance improvement and does not show that these packages
are independently extractable. The leaves still depend on Eshu's internal
facts, projector-intent, and reducer contracts; root projector assembly still
owns ordering, queue writes, retries, and telemetry.

## Proof environment

| Item | Baseline | After |
| --- | --- | --- |
| Source | `origin/main` at `cec137cd5a9a3d87790faad36b12ef12d85952c4` | Code-bearing commit `23619cb3f25bff7ac70a55bdcfe934b454418a93`; this evidence note is a documentation-only follow-up |
| Go | `go1.26.6 linux/amd64` | `go1.26.6 linux/amd64` |
| Backend/version | None; in-memory builder, dispatcher, registry, and static-contract proof | Same |
| Runtime measurement | Not run; no wall-clock, throughput, CPU, or allocation claim | Not run; no wall-clock, throughput, CPU, or allocation claim |

## No-regression proof

The same nine leaf tests ran before and after. They cover empty and unrelated
generations, AWS-only exclusion from provider-neutral drift, AWS/GCP/Azure
triggers, earliest-fact selection, exact domain, entity key, reason, fact ID,
and both source-system fallback tiers. Both runs exited 0.

Eight root tests also ran before and after. Six exercise the AWS and
provider-neutral runtime-drift dispatch paths; the other two pin the ordered
fan-out at 44 probes and 42 emitted intents, including every intent field. The
AWS drift probe remains immediately after package-source correlation, the
multi-cloud drift probe remains immediately after AWS drift, and inventory
admission remains after Azure relationship materialization rather than being
regrouped with the drift probes. Both runs exited 0.

The following commands ran from each checkout's `go/` directory. Host-local
cache paths are intentionally omitted.

```bash
GOTOOLCHAIN=go1.26.6 ../scripts/go-test-run-guard.sh 9 \
  '^TestBuild(CloudInventoryAdmission|AWSCloudRuntimeDrift|MultiCloudRuntimeDrift)ReducerIntent' -- \
  ./internal/projector/cloudinventory \
  ./internal/projector/awscloudruntimedrift \
  ./internal/projector/multicloudruntimedrift -count=1

GOTOOLCHAIN=go1.26.6 ../scripts/go-test-run-guard.sh 9 \
  '^TestBuildReducerIntent' -- \
  ./internal/projector/cloud/inventory \
  ./internal/projector/cloud/runtime/drift/aws \
  ./internal/projector/cloud/runtime/drift/multi -count=1

GOTOOLCHAIN=go1.26.6 ../scripts/go-test-run-guard.sh 8 \
  '^(TestBuildProjectionQueuesSingleAWSCloudRuntimeDriftIntent|TestBuildProjectionDoesNotQueueAWSCloudRuntimeDriftWithoutAWSResource|TestBuildProjectionQueuesMultiCloudRuntimeDriftIntentForGCPScope|TestBuildProjectionQueuesMultiCloudRuntimeDriftIntentForAzureScope|TestBuildProjectionDoesNotQueueMultiCloudRuntimeDriftForAWSOnlyScope|TestBuildProjectionDoesNotQueueMultiCloudRuntimeDriftWithoutCloudResourceFacts|TestAppendScopeGenerationReducerIntentsFanOutParity|TestReducerIntentProbeCountMatchesDocumentedCount)$' -- \
  ./internal/projector -count=1
```

Production source was compared after deterministic package and exported-symbol
substitution; all three normalized diffs were empty. The complete projector
package tree passed. The MCP route-serves-data registry's three guarded tests
passed after its load-bearing cloud-inventory source paths were repointed.

The blocking replay-coverage gate passed with 437 of 437 required surfaces
satisfied and 29 of 29 projection surfaces satisfied. The B-7 cassette tree
and B-12 `e2e-20repo-snapshot.json` are byte-identical to the base commit. No
live B-7 backend run was used as evidence because the runtime behavior and
golden artifacts did not change.

## Observability

No-Observability-Change: no metric instrument or label, span, structured-log
message or field, status field, queue table, failure class, route, or runtime
setting is added, removed, or renamed. No backend telemetry sample was
collected because this proof is backend-free and executable behavior is
unchanged.

Operators retain `eshu_dp_reducer_intents_enqueued_total` and
`eshu_dp_projector_run_duration_seconds` on the projector side. The reducer
domains retain their existing execution, duration, admission, evidence-load,
and finding-publication signals. The moved pure builders emit no signal of
their own. The telemetry coverage rows now point at the new source paths.
