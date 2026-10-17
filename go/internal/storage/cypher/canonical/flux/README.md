# Canonical flux edges

This package owns the canonical Cypher templates and the Go resolution for
Flux `RECONCILES_FROM` edges. Both reconciler kinds — `FluxKustomization`
(`edges.go`) and `FluxHelmRelease` (`helm.go`) — resolve through the
identical T1-T4 namespace tiers, but are retract-scoped and Cypher-templated
separately since they anchor on different source labels.

The registry holds one relationship type, `RECONCILES_FROM`, covering all
seven (reconciler, target-label) pairs. `querycontract` reads it directly for
the edge-materialization coverage gate. The parent `cypher` package wraps the
Statement-free parts into executable statements
(`canonical_flux_statements.go`); the four Phase G appends
(atlantis, flux, gitlab, helm) keep their order in the root phases file.

The exported templates preserve the original query bytes and parameters.
Changes to their MATCH, MERGE, or SET clauses affect graph truth and must
follow the Cypher performance and materialization proof requirements.

No-Regression Evidence: the nine template values are byte-identical to the
pre-move root files (sorted backtick-literal extraction diff is empty; per
template SHA-256 values match exactly): Kustomization GitRepository
`65da92bb72683d2ed8626f4d281bcb109f8e22be219dabfc74188f9f210a9eef`,
OCIRepository
`ad792cb400ed97f488852bc50116e2d850af2854a2ae43a735429528f0170f7e`,
Bucket `33de50f8109257dc98f37d41a590e6cca5e426917d60497426a2bdf1658fc3d3`,
Kustomization retract
`7f10aaafa632e5d945a140edbab36d0a8ab09ec84c3f4f7fc7d233c4f63efba9`,
HelmRelease retract
`3475e0ae242c4e4070275ff3472a2cd27a31b93199d9d917bde692e728a718a7`,
HelmRelease HelmRepository
`03c899cdebc1997fac0b4563bfac970cbb692c08943597e9dc4868e0f6e7c8b3`,
HelmRelease GitRepository
`3f2b2a0c8d8a73681ab34588886086cdfe875c1fe1a8334c18195c7092993567`,
HelmRelease OCIRepository
`c84545b1a3a738e3e014519cd68ae0f62845c5a969d64642f82c04c2fb5a982d`,
and HelmRelease Bucket
`9b0b307eef7c1667ebf1d967dc389bb94c49b1d673d478b605f6e2aa278233e4`.
All retain the same `UNWIND` input, endpoint matches, relationship MERGE,
and property SET list. Since the query bytes and writer dispatch are
unchanged, the move cannot change database work or row counts. No production
graph write was run; terminal queue/row counts are not applicable to this
source-only move.

Observability Evidence: the existing executor, statement metadata, retry
logs, and graph-write telemetry remain the same because callers dispatch the
same strings through the same writer.

No-Observability-Change: the package move adds no writer route, statement
metadata, metric, span, or log field.
