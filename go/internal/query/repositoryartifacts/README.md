# Repository artifacts package

## Purpose

`repositoryartifacts` holds the file-content artifact readers for the
repository handler family (Issue #6060, lane B): config artifacts (Ansible,
Compose, HCL, Kustomize), deployment artifacts, runtime artifacts
(Dockerfile), controller artifacts, CloudFormation artifacts, and workflow
artifacts (GitHub Actions), plus the shared candidate-file hydration and
the config/deployment/workflow artifact loaders behind the repository story
responses.

## Ownership boundary

This package owns content-derived artifact reads, not handler orchestration
and not graph reads. It imports only the standard library, `querycontract`,
and content-parsing libraries. It never imports `repository`,
`repository/readmodel`, or the query root. The `repository` package and a
handful of staying root stayers consume its exported loaders and
predicates.

## Exported surface

The exported surface is described in [doc.go](doc.go). Loaders and
predicates the `repository` package or staying root stayers call are
exported; per-family artifact shaping stays unexported. Cross-package test
pins go through `testutil` or `querycontract`.

The static workflow evidence and the repository-scoped CI/CD evidence have
listing entry points (`StaticWorkflowArtifactEvidence`,
`LoadRepositoryScopedCICDEvidence`) and `...FromFiles` entry points that take
the file list the caller already read. The repository story uses the latter so
one `ListRepoFiles` read serves the semantic overview and every later stage
(#7126); the two forms return identical evidence for the same files. The listing
entry points read `RepositorySemanticEntityLimit+1` rows and clip to the limit;
the `...FromFiles` entry points take the clipped list plus a `filesTruncated`
flag carrying the caller's sentinel (#7619). The candidate pool is unknown
exactly when that sentinel row exists, so a repository with exactly 5,000 files
is a complete scan. A truncated scan carries
`candidate_pool_status=unknown_at_limit`: no observed workflows
means `state=unknown`, while positive evidence keeps its observed count. The
20-path display cap and 50-file image hydration cap remain separate. Stories
preserve the candidate marker and add `static_workflow_coverage_unknown` to
`missing_evidence` only for a capped scan; this does not backfill the typed
API's other historical missing-evidence entries into uncapped stories.

## Dependencies

Standard library, `querycontract`, and content-parsing libraries, plus
`testutil` in tests. No handler packages, no graph drivers.

## Verification

Run focused `repositoryartifacts` tests, then `repository` and root
`query` suites. Artifact predicates feed access decisions: keep the
fail-closed behavior and bounded limits exactly as they are.
