# Deployment-evidence incoming reads anchored on the bound repository (#6811)

Change: `QueryRepoDeploymentEvidence`'s incoming branch
(`go/internal/query/repository/deployment_evidence.go`) matched
`(artifact:EvidenceArtifact)-[:EVIDENCES_REPOSITORY_RELATIONSHIP]->(r:Repository {id: $repo_id})`,
then `WITH artifact, r MATCH (source:Repository)-[:HAS_DEPLOYMENT_EVIDENCE]->(artifact)`.
It is now one pattern anchored on the bound repository,
`(r:Repository {id: $repo_id})<-[:EVIDENCES_REPOSITORY_RELATIONSHIP]-(artifact:EvidenceArtifact)<-[:HAS_DEPLOYMENT_EVIDENCE]-(source:Repository)`,
with `RETURN`, `ORDER BY path, artifact_id` and `LIMIT $limit` unchanged.
`FetchFluxDeploymentSourceTargetBindings`' expansion read
(`go/internal/query/impacttrace/impact_trace_deployment_flux_bindings.go`)
mirrors its second `MATCH` the same way,
`(targetRepo:Repository {id: $repo_id})<-[targetRel:EVIDENCES_REPOSITORY_RELATIONSHIP]-(artifact)`,
for shape consistency; it stays anchored on the `UNWIND`ed artifact ids.
`TestRepositoryContextIncomingReadsAnchorOnTheBoundRepository` now runs the
deployment-evidence read too and fails on the old shape (verified by
restoring the pre-change file: "incoming read is right-anchored on the bound
repository"). The `impacttrace` entry in `hot-cypher.yaml` and both
`query-source-coverage.yaml` digests were rotated to the validator's values.

## Row-set truth first

`TestLiveNornicDBDeploymentEvidenceAnchor`
(`go/internal/query/repository/nornicdb_deployment_evidence_anchor_live_test.go`,
tag `live_nornicdb_answer_truth`) seeds a hub with exactly 40 incoming
artifacts, a leaf with none and a mid-graph repository with 2, plus
`ESHU_6811_FILLER` filler repositories wired to each other, then runs the
production function, the pre-change statement and the candidate statement.
On a fresh container of the pinned image
(`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-490-a427a468`, digest
`eb69530f…`) at filler 300 all three agree with the construction: hub 40,
leaf 0, mid 2, flux expansion 40 (`scratchpad` run `6811-accuracy-300.log`,
exit 0). The 11-of-40 result the issue saw on `v1.3.3` did not reproduce on
the pinned build, so this change is a performance fix with row-set parity,
not an accuracy fix.

## Theory proof, fresh container per shape

Performance Evidence: `TestLiveNornicDBDeploymentEvidenceAnchorTiming` (same file family, one shape per process) applied Eshu's NornicDB schema with `graph.EnsureSchemaWithBackend` (so `Repository.id` and `EvidenceArtifact.id` had their lookup indexes), seeded the fixture, and ran the selected statement against the hub three times with a varying unused `$nonce` parameter and a 60 s client timeout; the harness (`6811-shape-timing.sh`) started a fresh pinned-image container for every (shape, filler) pair and removed it afterwards, so a timed-out statement never contaminated a later measurement. Incoming read, filler 300 (342 repositories, 1,000 artifacts): pre-change 5,777.9 / 5,636.0 / 5,696.0 ms; bound-anchored 0.7 / 0.5 / 0.5 ms; 40 rows both. Filler 3,000 (3,042 repositories, 9,100 artifacts): pre-change TIMEOUT at 60,002 ms on the first run (container discarded); bound-anchored 1.2 / 11.0 / 2.4 ms, 40 rows. Flux expansion (40 artifact ids, 40 source ids): filler 300 pre-change 7.4 / 3.7 / 3.9 ms, mirrored 8.8 / 5.7 / 5.9 ms; filler 3,000 pre-change 7.1 / 4.0 / 4.0 ms, mirrored 9.1 / 5.6 / 5.8 ms; 40 rows all runs. The flux rewrite is therefore shape hygiene, not a measured speedup: its artifact anchor already bounds the work, and the numbers are within run-to-run noise.

No-Observability-Change: the `deployment_evidence` stage timer, its row count and its log fields are unchanged; only the statement text moved. No metric, span or status field was added.
