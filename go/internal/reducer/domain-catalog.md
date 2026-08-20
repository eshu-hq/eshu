# Reducer Domain Catalog

Split from `README.md` (issue #5786). Keep the package overview in
`README.md`; keep the full domain constant table and the workload
signal confidence registry here.

## Domain catalog

All reducer domains are declared in `domain.go` and registered via
`NewDefaultRuntime` / `NewDefaultRegistry` in `defaults.go`. Each domain has an
`OwnershipShape` enforcing cross-source, cross-scope, and either durable
canonical-write or bounded counter-emission requirements.

`AllDomains` returns every reducer-owned domain sorted lexicographically: the
claim/materialization domains in `knownDomains` plus the shared/edge projection
domains in `allProjectionDomains` (`shared_projection.go`), deduplicated. It is
the single enumeration source for tooling that must list the full domain set —
notably the capability surface inventory and its drift gate — so adding a domain
to either registry automatically adds it there. `allProjectionDomains` is a
superset of the partition worker's `sharedProjectionDomains`: it also covers the
domains driven by dedicated projection runners (`code_calls`, `repo_dependency`,
`deployable_unit_edges`).

`MaterializedEdgeFamilies` (`materialized_edge_families.go`, #5351) returns
`allProjectionDomains` as `[]string`, sorted: the drift-proof enumeration the
Ifá `materialized_edges:<domain>` exhaustiveness gate
(`go/internal/ifa/materializededges/materialized_edges.go`) binds an Odù expectation to, so a
reducer materialization silently ceasing to produce an edge family is caught.
A domain added to or removed from `allProjectionDomains` moves this
enumeration in the same change; `TestMaterializedEdgeFamiliesLocksToAllProjectionDomains`
locks the two together.

| Domain constant | Summary |
| --- | --- |
| `DomainWorkloadIdentity` | Resolve canonical workload identity across sources |
| `DomainDeployableUnitCorrelation` | Correlate cross-source deployable-unit evidence and write admitted resolved deployment-repo edges |
| `DomainCloudAssetResolution` | Resolve canonical cloud asset identity across sources |
| `DomainDeploymentMapping` | Materialize platform bindings across sources |
| `DomainDataLineage` | Resolve lineage across sources and scopes |
| `DomainOwnership` | Resolve ownership and responsibility records |
| `DomainGovernance` | Resolve governance and policy attribution |
| `DomainWorkloadMaterialization` | Materialize canonical workload graph nodes |
| `DomainCodeCallMaterialization` | Materialize canonical code-call edges |
| `DomainSemanticEntityMaterialization` | Materialize Annotation, Typedef, TypeAlias, Component semantic nodes |
| `DomainSQLRelationshipMaterialization` | Resolve bounded SQL entity metadata into canonical `READS_FROM`, `REFERENCES_TABLE`, `WRITES_TO`, `HAS_COLUMN`, `TRIGGERS`, `EXECUTES`, `INDEXES`, `MIGRATES`, and embedded-query `QUERIES_TABLE` edges; ambiguous or missing FK/write targets are counted and skipped, never guessed (#5410) |
| `DomainShellExecMaterialization` | Materialize canonical shell execution edges |
| `DomainInheritanceMaterialization` | Materialize inheritance, override, and alias edges |
| `DomainPackageSourceCorrelation` | Classify package-registry source hints and package-version publication evidence without ownership promotion |
| `DomainCodeImportRepoEdge` | Project repo→repo `DEPENDS_ON` edges from per-file external import sources correlated to package-registry ownership (`projection/code-imports`) |
| `DomainAWSCloudRuntimeDrift` | Publish admitted AWS runtime orphan, unmanaged, unknown, and ambiguous drift findings as canonical reducer facts |
| `DomainMultiCloudRuntimeDrift` | Publish admitted provider-neutral runtime orphan, unmanaged, ambiguous, and unknown drift findings keyed on canonical `cloud_resource_uid` for GCP and Azure; AWS rows the shared loader also returns are dropped before publication because `DomainAWSCloudRuntimeDrift` already owns AWS findings (issue #5759) |
| `DomainContainerImageIdentity` | Join Git, OCI registry, and runtime image references into digest-keyed reducer facts |
| `DomainCICDRunCorrelation` | Correlate CI/CD runs, artifacts, and environments with artifact identity evidence |
| `DomainServiceCatalogCorrelation` | Correlate service-catalog entities with explicit repository links, repo-local descriptor scope, and ownership evidence without inventing workloads |
| `DomainSBOMAttestationAttachment` | Attach SBOM and attestation documents to image digests only when subject evidence is explicit |
| `DomainSupplyChainImpact` | Publish vulnerability impact findings only when explicit vulnerability, package, SBOM, image, or repository evidence exists |
| `DomainSecurityAlertReconciliation` | Compare provider repository security alerts with Eshu-owned dependency and impact evidence, including alert-seeded impact rows only when owned dependency evidence matches |
| `DomainSecretsIAMTrustChain` | Build durable secrets/IAM read-model facts from redaction-safe AWS IAM, Kubernetes ServiceAccount/workload, and Vault metadata anchors; supports IRSA and EKS Pod Identity identity-provider hops, writes no graph labels/edges/DDL, and preserves unresolved/stale/partial/permission-hidden/unsupported gaps |
| `DomainAWSResourceMaterialization` | Materialize `aws_resource` facts into canonical `CloudResource` nodes; publishes the `cloud_resource_uid` canonical-nodes phase the AWS relationship edge gates on (issue #805). Also surfaces `running_image_ref`/`running_image_digest` node props on an ECS running-task or Lambda function `CloudResource`, decoded through the typed `awsv1` attribute seam; a multi-container ECS task stays ambiguous and unpromoted rather than guessing (issue #5450) |
| `DomainAWSCloudImageMaterialization` | Project `lambda_function_uses_image` `aws_relationship` facts into canonical `(:CloudResource)-[:AWS_lambda_function_uses_image]->(:ContainerImage)` edges — an additive sibling of `DomainAWSRelationshipMaterialization` for the cross-label target that domain's `CloudResource`-only join index cannot resolve. The target `ContainerImage` uid is computed directly from the relationship's own `resolved_image_uri` attribute (an exact `registry/repository@digest`), never joined; `ecs_task_definition_uses_image` is recognized and always skipped (tag-only, no digest — stays Postgres-only per the #5472 EXACT-ONLY graph-projection policy). Enqueued every generation the scope carries `aws_resource` facts (the same persistent trigger `DomainAWSResourceMaterialization` uses), not gated on `lambda_function_uses_image` relationship presence, so a generation where a Lambda's image relationship disappears (e.g. an Image-to-Zip package switch) still runs Handle's retract-first logic and correctly retracts the prior edge instead of leaving it stale (retraction-safety fix, issue #5450 follow-up). Gates on the `cloud_resource_uid` canonical-nodes phase (source only); never fabricates or dangles an edge (issue #5450). A resolved-but-unscanned target (the OCI registry never observed that digest) is reclassified as a `target_not_materialized` skip via `ContainerImageExistence` BEFORE the `eshu_dp_aws_cloud_image_edges_total` metric/`CanonicalWrites`/evidence summary read the row, so those never over-report an edge the graph does not actually have (issue #5450 P1 follow-up); see `docs/internal/aws-relationship-edge-materialization-design.md` §12 |
| `DomainGCPResourceMaterialization` | Materialize `gcp_cloud_resource` facts into canonical `CloudResource` nodes keyed by `cloudResourceUID(project_id, location, asset_type, full_resource_name)` on the existing `cloud_resource_uid` keyspace; reuses the provider-neutral `CloudResourceNodeWriter`, stores the globally-unique CAI `full_resource_name` as `resource_id` so the GCP relationship edge join resolves endpoints exactly, and publishes the canonical-nodes phase under the distinct `gcp_resource_materialization:<scope>` entity key the GCP relationship edge gates on (issue #2358); see `docs/internal/gcp-cloud-resource-materialization-design.md` |
| `DomainGCPRelationshipMaterialization` | Project `gcp_cloud_relationship` facts into canonical `(:CloudResource)-[:GCP_<TYPE>]->(:CloudResource)` edges, mirroring `DomainAWSRelationshipMaterialization` for GCP; resolves both endpoints by the globally-unique CAI `full_resource_name` against an in-memory join index, gates on the `cloud_resource_uid` canonical-nodes phase published by `gcp_resource_materialization:<scope>`, materializes only `supported` relationships (`partial` treats the target as unresolved, `unsupported` is provenance only), skips+counts unsafe relationship-type tokens, and never fabricates or dangles an edge (issue #2348); see `docs/internal/gcp-cloud-relationship-edge-materialization-design.md` |
| `DomainEC2InstanceNodeMaterialization` | Materialize `ec2_instance_posture` facts into canonical EC2 instance `CloudResource` nodes keyed by `cloudResourceUID(account, region, "aws_ec2_instance", instance_id)` on the existing `cloud_resource_uid` keyspace (the EC2 scanner emits no `aws_resource` inventory fact for instances); carries metadata-only safe identifiers plus derived posture booleans (IMDS, user-data presence, monitoring, public-IP, `instance_profile_arn`) — never user-data content, the raw public IP, or block devices; publishes the `cloud_resource_uid` canonical-nodes phase under the distinct `ec2_instance_node_materialization:<scope>` entity key the future `USES_PROFILE` edge gates on (issue #1146 PR-A); see `docs/internal/design/1146-ec2-instance-node.md` |
| `DomainKubernetesWorkloadMaterialization` | Materialize `kubernetes_live.pod_template` facts into canonical `KubernetesWorkload` nodes keyed by the collector-emitted `object_id`; publishes the `kubernetes_workload_uid` canonical-nodes phase the #388 live-workload edge gates on |
| `DomainKubernetesCorrelationMaterialization` | Project exact live-workload correlation decisions into canonical `RUNS_IMAGE` edges from a `KubernetesWorkload` node to the digest-addressed OCI source node it runs; gates on the `kubernetes_workload_uid` canonical-nodes phase, exact-only, never fabricates or dangles an edge (issue #388 PR3) |
| `DomainKubernetesNamespaceMaterialization` | Materialize `kubernetes_live.namespace` facts into canonical `KubernetesNamespace` nodes keyed by the collector-emitted `object_id` ((cluster_id, namespace) identity); binds an `Environment` node via `TARGETS_ENVIRONMENT` ONLY when a namespace label (`environment` or `app.kubernetes.io/environment`) declares a value in `environment.IsKnownToken`'s known set, tagging it `EvidenceClassNamespaceLabel`; an unrecognized or absent label classifies `StateEnvironmentUnbound` and creates NO `Environment` node. Complete cluster snapshots generation-stamp current nodes and retract reducer-owned nodes absent from that generation, including an empty successful list; partial snapshots and generations containing any quarantined namespace fact remain additive and never retract. Complete-snapshot rows must match the intent cluster before any graph write — the first live-cluster namespace->environment binding (issue #5434) |
| `DomainIAMCanAssumeMaterialization` | Project `aws_iam_permission` trust statements into canonical `(:CloudResource)-[:CAN_ASSUME]->(:CloudResource)` edges from an assuming IAM principal (role/user) to the role whose trust policy grants the assume; gates on the `cloud_resource_uid` canonical-nodes phase (the same gate `aws_relationship_materialization` uses), `effect=Allow` only, skips external / AWS-service / wildcard / account-root / unscanned principals, never fabricates or dangles an edge (issue #1134 PR2) |
| `DomainS3LogsToMaterialization` | Project `s3_bucket_posture` `logging_target_bucket` fields into canonical `(:CloudResource)-[:LOGS_TO]->(:CloudResource)` edges from a source S3 bucket to the target log bucket it delivers server-access logs to; resolves the target by bucket-name equality against an in-memory S3 join index; gates on the `cloud_resource_uid` canonical-nodes phase (the same gate `aws_relationship_materialization` uses); a blank target (logging disabled) is no edge and not a skip; a self-target (bucket logging to itself) is a legal config and DOES emit an edge; cross-account / out-of-scope / unscanned targets are counted, never fabricated or dangled (issue #1144 PR2); see `docs/internal/design/1144-s3-logs-to-edge.md` |
| `DomainS3ExternalPrincipalGrantMaterialization` | Project metadata-only `s3_external_principal_grant` facts into canonical `(:CloudResource)-[:GRANTS_ACCESS_TO]->(:ExternalPrincipal)` graph truth; resolves the source bucket by bucket-name equality against the same S3 in-memory join index, gates on the `cloud_resource_uid` canonical-nodes phase, creates only bounded `ExternalPrincipal` identities keyed by principal kind/value, skips unsupported or unresolved grants with tallies, never creates S3 `CloudResource` nodes, and never propagates raw bucket policy, statement, ACL, condition, action, resource, or object data (issue #1231); see `docs/internal/design/1231-s3-external-principal-grant-projection.md` |
| `DomainRDSPostureMaterialization` | Project `rds_instance_posture` security/operations posture onto existing RDS DB instance and Aurora cluster `CloudResource` nodes; gates on the `cloud_resource_uid` canonical-nodes phase, writes only reducer-owned posture properties, never creates RDS nodes, and leaves KMS/security-group/subnet-group/IAM/parameter/option dependency edges to generic `aws_relationship_materialization` (issue #1233) |
| `DomainEC2InstanceIdentityMaterialization` | Project the `aws_ec2_instance` `aws_resource` identity fact's `ami_id` onto the EC2 instance `CloudResource` node `DomainEC2InstanceNodeMaterialization` already created; gates on the EC2 instance-node `cloud_resource_uid` canonical-nodes phase (`ec2_instance_node_materialization:<scope>`), NOT the generic `aws_resource_materialization:<scope>` phase RDS posture reuses; writes only the disjoint `ami_id`/`ec2_identity_*` properties (never the base identity/posture fields), never creates a node, and its `RetractEC2InstanceIdentityNodes` REMOVEs only its own namespaced properties; the generic `DomainAWSResourceMaterialization`'s `cloudResourceNodeRow` explicitly excludes `aws_ec2_instance` so the two domains never race over the same property (issue #5448) |
| `DomainEC2UsesProfileMaterialization` | Project `ec2_instance_posture` `instance_profile_arn` into canonical `(:CloudResource)-[:USES_PROFILE]->(:CloudResource)` edges from an EC2 instance to the IAM instance profile it uses; derives the source EC2 instance uid the same way `DomainEC2InstanceNodeMaterialization` does (#1146 PR-A) and resolves the target profile by exact ARN equality against an in-memory `aws_iam_instance_profile` join index; gates on a DUAL `cloud_resource_uid` canonical-nodes readiness — the EC2 instance node phase (`ec2_instance_node_materialization:<scope>`) AND the IAM instance-profile node phase (`aws_resource_materialization:<scope>`), published under different entity keys, so the edge never resolves against a not-yet-materialized endpoint; a blank profile (no attached profile) is no edge and not a skip; cross-account / out-of-scope / unscanned profiles are counted, never fabricated or dangled (issue #1146 PR-B). The first edge in the EC2 → profile → role → `CAN_ESCALATE_TO` blast-radius chain; see `docs/internal/design/1146-ec2-uses-profile-edge.md` |
| `DomainIAMInstanceProfileRoleMaterialization` | Project IAM instance-profile `aws_resource` `role_arns` into canonical `(:CloudResource)-[:HAS_ROLE]->(:CloudResource)` edges from an IAM instance profile to each attached IAM role; resolves role targets by exact ARN equality against an in-memory `aws_iam_role` join index; gates on the `cloud_resource_uid` canonical-nodes phase published by `aws_resource_materialization:<scope>` because both endpoint node families are `aws_resource` CloudResource nodes; profiles with no roles still run the reducer to retract stale HAS_ROLE edges but write zero new edges and are not a skip; cross-account / out-of-scope / unscanned roles are counted, never fabricated or dangled (issue #1299). The middle edge in the EC2 -> profile -> role -> `CAN_ESCALATE_TO` blast-radius chain; see `docs/internal/design/1299-iam-instance-profile-role-edge.md` |
| `DomainEC2InternetExposureMaterialization` | Derive conservative `exposed` / `not_exposed` / `unknown` EC2 internet-exposure state from `ec2_instance_posture`, ENI relationship, and security-group rule facts, then write reducer-owned properties onto existing EC2 `CloudResource` nodes only; gates on the EC2 instance-node `cloud_resource_uid` canonical-nodes phase (`ec2_instance_node_materialization:<scope>`), never persists raw public IP addresses, never treats missing ENI/SG/rule evidence as safe false, and keeps unknown posture as `state=unknown` with no boolean exposure property (issue #1301); see `docs/internal/design/1301-ec2-internet-exposure.md` |
| `DomainEC2BlockDeviceKMSPostureMaterialization` | Derive EC2 block-device KMS posture from `ec2_instance_posture.block_devices[]` joined to scanned `aws_ec2_volume`, `aws_kms_key`, and `ec2_volume_uses_kms_key` facts; writes bounded reducer-owned properties onto existing EC2 `CloudResource` nodes only, gates on DUAL `cloud_resource_uid` readiness for the EC2 instance-node phase (`ec2_instance_node_materialization:<scope>`) and the EBS/KMS resource-node phase (`aws_resource_materialization:<scope>`), never writes raw block-device maps, never calls AWS from the reducer, and keeps missing volume facts, missing KMS key facts, AWS-managed/default keys, detached volumes, and tombstones conservative as `state=unknown` (issue #1304) |
| `DomainS3InternetExposureMaterialization` | Derive conservative `exposed` / `not_exposed` / `unknown` S3 internet-exposure state from `s3_bucket_posture` facts and write reducer-owned properties onto existing S3 `CloudResource` nodes only; gates on the `cloud_resource_uid` canonical-nodes phase, resolves the source bucket through the S3 in-memory join index, never reads or persists raw bucket policy, ACL grants, object keys, or object data, and keeps unknown posture as `state=unknown` with no boolean exposure property (issue #1232); see `docs/internal/design/1232-s3-internet-exposure.md` |
| `DomainIncidentRoutingMaterialization` | Project exact PagerDuty incident-routing evidence into reducer-owned `IncidentRoutingEvidence` graph nodes and intended/applied/live evidence relationships without promoting runtime, image, commit, pull-request, Jira, service-health, or root-cause truth |
| `DomainCodeInterprocEvidence` | Project direct `code_interproc_evidence` facts into reducer-owned `TAINT_FLOWS_TO` edges between existing Function nodes |
| `DomainCodeFunctionSummary` | Persist generation-independent value-flow summaries, param sources, and FunctionID->uid mappings, then run post-persist cross-repo fixpoint projection as isolated `reducer/code-interproc-fixpoint` `TAINT_FLOWS_TO` evidence; the fixpoint partitions durable summary/source/sink snapshots before Program assembly and reuses durable solved component results across reducer restarts; unresolved endpoints are skipped rather than fabricated |

`DomainDeployableUnitCorrelation` writes graph truth only after rule evaluation
admits an exact candidate with a resolved deployment repository. The handler
retracts `reducer/deployable-unit-correlation` edges for the source repository,
writes admitted rows through `DomainDeployableUnitEdges`, and only then
publishes `GraphProjectionPhaseDeployableUnitCorrelation`. Rejected,
ambiguous, endpoint-less, and stale candidates therefore remove prior
deployable-unit truth without fabricating a replacement edge.


## Workload Signal Confidence Registry

`ExtractWorkloadCandidates` (`candidate_loader.go`) scores whether a repository
defines a deployable workload from provenance signals (Kubernetes resources,
Argo CD applications, Helm charts, Dockerfiles, docker-compose, CloudFormation,
GitHub Actions, Jenkins). Those signal-strength priors used to be float literals
inlined in the `addProvenance` calls with no documented rationale and no test of
their relative ordering (issue #3490). They now live in one documented registry,
`DefaultWorkloadSignalConfidence` in `workload_signal_confidence.go`, keyed by
`WorkloadSignalKind`. Each entry records the value, a `WorkloadSignalTier`, and a
rationale.

The tiers pin the ordering invariant that matters for admission truth: signals
that describe a deployed runtime outrank CI/controller-only provenance, which
only describes where automation runs.

| Tier | Floor | Signals |
| --- | --- | --- |
| `WorkloadTierOrchestratedRuntime` | 0.95 | `k8s_resource` (0.98), `argocd_application` (0.95) |
| `WorkloadTierPackagedRuntime` | 0.90 | `helm_chart` (0.92) |
| `WorkloadTierLocalRuntime` | 0.78 | `dockerfile_runtime` (0.88), `docker_compose_runtime` (0.78) |
| `WorkloadTierTemplate` | 0.50 | `cloudformation_template` (0.58) |
| `WorkloadTierCIProvenance` | 0.00 | `github_actions_workflow` (0.45), `jenkins_pipeline` (0.42) |

`workload_signal_confidence_test.go` pins that every signal has an entry, every
value is in `[0,1]`, runtime signals strictly outrank CI signals, and tier
floors are monotonic. Recalibrate via
`DefaultWorkloadSignalConfidence.WithOverrides(...)`, which validates `[0,1]` and
never mutates the shared default. Full golden-set calibration of the absolute
values remains future work; this registry is the structural prerequisite.

No-Regression Evidence: centralizing the `addProvenance` confidence literals into
`DefaultWorkloadSignalConfidence` (issue #3490) is a pure refactor. Every emitted
provenance confidence is byte-identical to the prior inline literal (the registry
is built once at package init and read by O(1) map lookup), and the provenance
kind strings are unchanged, so `ExtractWorkloadCandidates` output and downstream
deployable-unit admission behavior are unchanged. It adds no graph write, queue
claim, schema, worker, lease, or batch behavior. Proven by `go test
./internal/reducer -count=1`, with the unchanged `candidate_loader_test.go`
confidence expectations still passing and the new
`workload_signal_confidence_test.go` invariants added.

No-Observability-Change: this refactor adds no runtime stage, metric, or span.
Operators continue to read workload-candidate confidence through the existing
deployable-unit correlation reducer facts and graph-projection phase signals.
