# Deployment-evidence incoming read anchored on the bound repository (#6811)

Change: `QueryRepoDeploymentEvidence`'s incoming branch
(`go/internal/query/repository/deployment_evidence.go`) matched
`(artifact:EvidenceArtifact)-[:EVIDENCES_REPOSITORY_RELATIONSHIP]->(r:Repository {id: $repo_id})`,
then `WITH artifact, r MATCH (source:Repository)-[:HAS_DEPLOYMENT_EVIDENCE]->(artifact)`.
It is now one pattern anchored on the bound repository,
`(r:Repository {id: $repo_id})<-[:EVIDENCES_REPOSITORY_RELATIONSHIP]-(artifact:EvidenceArtifact)<-[:HAS_DEPLOYMENT_EVIDENCE]-(source:Repository)`,
with `RETURN`, `ORDER BY path, artifact_id` and `LIMIT $limit` unchanged.
Both patterns require both edges and bind the same variables, so they name
the same rows; `ORDER BY path, artifact_id` is not a total order when one
artifact has two sources, so which tied row survives at the `LIMIT` boundary
was already unspecified and is unchanged in kind. The issue's second read,
`FetchFluxDeploymentSourceTargetBindings`' expansion
(`go/internal/query/impacttrace/impact_trace_deployment_flux_bindings.go`),
is **unchanged**: its mirrored form was measured below and not adopted.
`TestRepositoryContextIncomingReadsAnchorOnTheBoundRepository` now runs the
deployment-evidence read too and fails on the old shape (verified by
restoring the pre-change file: "incoming read is right-anchored on the bound
repository"). No queryplan digest moved: the manifests bind
`queryRepoDeploymentEvidenceDirection`, whose body is unchanged, and the flux
read whose digests they also bind is unchanged.

## Row-set truth first

`TestLiveNornicDBDeploymentEvidenceAnchor`
(`go/internal/query/repository/nornicdb_deployment_evidence_anchor_live_test.go`,
tag `live_nornicdb_answer_truth`) applies Eshu's NornicDB schema, seeds a hub
with exactly 40 incoming artifacts, a leaf with none and a mid-graph
repository with 2, plus `ESHU_6811_FILLER` filler repositories wired to each
other, then asserts that the shipped statement returns exactly the
constructed (artifact, source) pairs and that the production function returns
the same rows; the pre-change statement is run and logged. On a fresh
container of the pinned image
(`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-490-a427a468`, digest
`eb69530f…`) at filler 300, run after the final edit: hub 40, leaf 0, mid 2,
flux 40, production equal to the shipped statement everywhere, pre-change
statement equal too (`scratchpad` run `6811-accuracy-300-r3.log`, exit 0).
The 11-of-40 result the issue saw on `v1.3.3` did not reproduce on the
pinned build, so this change is a performance fix with row-set parity, not
an accuracy fix.

## Theory proof, fresh container per shape

Performance Evidence: `TestLiveNornicDBDeploymentEvidenceAnchorTiming` (one shape per process) applied Eshu's NornicDB schema with `graph.EnsureSchemaWithBackend` (so `Repository.id` and `EvidenceArtifact.id` had their lookup indexes), seeded the fixture, and ran the selected statement against the hub three times with a varying unused `$nonce` parameter and a 60 s client timeout; the harness started a fresh pinned-image container for every (shape, filler) pair and removed it afterwards, so a timed-out statement never contaminated a later measurement. The statements carry the production `MATCH` clauses with the `RETURN` abridged to the identity columns; projection over 40 rows cannot account for the ratios below. Incoming read, filler 300 (342 repositories, 342 artifacts): pre-change 5,777.9 / 5,636.0 / 5,696.0 ms; bound-anchored 0.7 / 0.5 / 0.5 ms; 40 rows both. Filler 3,000 (3,042 repositories, 3,042 artifacts): pre-change TIMEOUT at 60,002 ms on the first run (container discarded); bound-anchored 1.2 / 11.0 / 2.4 ms, 40 rows. Flux expansion (40 artifact ids, 40 source ids): shipped 7.4 / 3.7 / 3.9 ms at filler 300 and 7.1 / 4.0 / 4.0 ms at 3,000; mirrored 8.8 / 5.7 / 5.9 ms and 9.1 / 5.6 / 5.8 ms; 40 rows in every completed run. The mirrored flux form was slower in all six samples (about 1.6 to 2.0 ms in steady state, measured across separate containers), so that half of the theory is disproven and the flux read keeps its shipped shape.

No-Observability-Change: the `deployment_evidence` stage timer, its row count and its log fields are unchanged; only the statement text moved. No metric, span or status field was added.
