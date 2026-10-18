# Evidence: reducer compat alias removals (#6626 batch 6)

## What changed

Eight reducer-root compat aliases with zero callers are deleted, leaving the
canonical owning-package symbols untouched and still used:

- `reducer.AWSCloudRuntimeDriftBeginner` (`compat_cloud.go`, canonical
  `awscloud.AWSCloudRuntimeDriftBeginner`)
- `reducer.ServiceMaterializationBeginner` (`compat_correlation.go`,
  canonical `servicecatalog.ServiceMaterializationBeginner`)
- `reducer.ServiceEvidenceFamilyDeployment`,
  `reducer.ServiceEvidenceFamilyDependencies`,
  `reducer.ServiceEvidenceFamilyDocs`,
  `reducer.ServiceEvidenceFamilyIncidents` (`compat_correlation.go`,
  canonical `servicecatalog.ServiceEvidenceFamily*`)
- `reducer.SharedProjectionUnroutableWriter`
  (`compat_correlation.go`, canonical `worker.UnroutableWriter`)
- `reducer.SharedProjectionRefreshFenceLookup`
  (`compat_correlation.go`, canonical `worker.RefreshFenceLookup`)

Retained, not deleted: `ServiceEvidenceFamilyOwnership` and
`ServiceEvidenceFamilyRuntime` (same-package test callers in
`service_runtime_instance_lookup_test.go`), plus `SharedProjectionIntentRow`,
`SharedProjectionIntentInput`, `SharedProjectionAcceptanceKey`,
`SharedProjectionUnroutableRow`, `SharedProjectionWriteReport`, and
`FirstProjectionLookup`, which the diff only realigns.
`SharedProjectionIntentRow` keeps its large caller family for a later
coordinated slice. Downstream prose repoints land in `reducer/doc.go`, the
shared worker docs, the Postgres README, and
`shared_projection_unroutable_intents.go`.

## Why no regression

No-Regression Evidence: baseline (pre-removal tree) and after (removal head)
behave identically because a deleted name with no references cannot alter any
execution path: a repo-wide qualified sweep for `reducer.<Name>` over all
eight deleted spellings returns zero hits outside this note (`rg` exit 1
when this file is excluded; the note itself names each spelling once, and no
code, test, script, or other doc references any of them). No graph query,
queue, worker, lease, batch, runtime knob, or storage contract changes.
Proof on this head: `go build` of the touched trees exits 0 and `go vet` is
clean on the touched packages (a full-module `./...` build/vet additionally
passes where the toolchain resolves third-party cgo; it is environment-limited
by `go-sitter-forest/perl` vs system GCC in some worktrees, untouched by this
diff). No backend/version applies: no graph or query surface is touched.

## Observability

No-Observability-Change: no metric, span, status field, route, or log
contract changes; nothing emitted or consumed these aliases, so operators
keep diagnosing these paths through the existing reducer spans, counters,
and materialization logs.
