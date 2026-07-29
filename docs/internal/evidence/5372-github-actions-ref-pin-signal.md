# GitHub Actions @ref Pin Signal Evidence (#5372)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## GitHub Actions @ref pin signal (#5372)

`resolvedRelationshipEvidenceArtifacts` (`cross_repo_evidence_artifacts.go`)
projects `ref_value`/`ref_pinned` onto the `EvidenceArtifact` graph node for
`GITHUB_ACTIONS_*` evidence kinds only. `ref_value` is the raw `@ref` a
GitHub Actions action/workflow step pins to (read from the evidence-preview
`Details` fields `first_party_ref_version`/`action_ref_name`/
`workflow_ref_name`, populated by `go/internal/relationships`);
`ref_pinned` is `true` only when `ref_value` is a full-length (40- or
64-hex) commit SHA, via the shared `go/internal/ghactionsref` package's
`Pinned` classifier -- the same classifier the query-package read-model path
(`go/internal/query/repository_deployment_evidence_read_model.go`) uses, so
the graph-projection path and the read-model path agree. Both fields are
omitted together when the evidence carries no ref at all (a local `./`
reusable workflow, a Docker action). The kind gate matters because
`first_party_ref_version` is also populated by unrelated evidence families
(Terraform, Ansible, Chef, ...); without it, this projection would fabricate
a GitHub Actions pin-safety label on those.

No-Regression Evidence: this follows the existing byte-level-citation
projection pattern above (issue #3636) exactly -- an additive, conditionally
populated pair of scalar properties on the same evidence-preview walk, no new
MATCH/MERGE anchor, no additional graph round trip. `go test
./internal/ghactionsref ./internal/relationships ./internal/reducer
./internal/query ./internal/storage/cypher -count=1` stays green. The 20-repo
golden corpus carries no GitHub Actions workflow fixtures (confirmed by
`rg -l "GITHUB_ACTIONS|\.github/workflows" testdata/cassettes/
testdata/golden/e2e-20repo-snapshot.json` returning no matches before this
change), so `scripts/verify-golden-corpus-gate.sh` exercises no code path this
change touches and is expected no-diff.

No-Observability-Change: no metrics, spans, or structured logs are added or
altered; ref_value/ref_pinned flow as evidence-fact and graph-node-property
data only, following the existing citation-field pattern.
