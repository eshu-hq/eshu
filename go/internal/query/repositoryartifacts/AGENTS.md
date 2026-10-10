# AGENTS.md - repositoryartifacts

## Read first

1. `doc.go` for the public contract.
2. `README.md` for the ownership boundary and proof requirements.
3. Parent `../AGENTS.md` for query-wide invariants.

## Invariants

- Leaf package: standard library, `querycontract`, and content-parsing
  libraries only. Never import `repository`, `repository/readmodel`, or the
  query root.
- Config-artifact content is fetched only for the consumed set (#7126). A new
  extractor branch that reads `file.Content` must add its selector to
  `configArtifactReadsContent` (`repository_config_artifacts_hydration.go`) and
  to the unread-class corpus in
  `repository_config_artifacts_hydration_differential_test.go`; the generated
  tree test fails when an extractor reads content the plan does not fetch.
- Artifact predicates feed access decisions (`filter...ForAccess` callers);
  keep the fail-closed behavior and bounded limits exactly as they are.

## Verification

Run focused `repositoryartifacts` tests, then `repository` and root `query`
suites. Run `scripts/verify-package-docs.sh` whenever this package changes.
