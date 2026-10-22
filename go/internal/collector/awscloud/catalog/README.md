# AWS Collector Catalog

## Purpose

The catalog package is the leaf that the #6696 AWS consolidation moves the
flat `constants_<service>.go` families into, grouped by service category
(`storage.go`, then one file per follow-up category). It exists because the
constants cannot live beside any service: the awscloud root uses the `Service*`
constants itself while every service package imports the root, so either
placement would be an import cycle. A leaf that imports neither side breaks
it.

## Ownership boundary

Owns the AWS service-kind, resource-type, and relationship string constants
only. It does not own scanners, runtime binding, redaction, envelopes, or
graph truth — those stay in `awscloud`, `awscloud/service/<service>`, and the
reducer. It must never import `awscloud`, any `awscloud/service` package, or
the AWS SDK: any of those reintroduces the cycle this package exists to
avoid.

## Exported surface

- `ServiceS3`, `ServiceEFS`, `ServiceFSx`, `ServiceStorageGateway`,
  `ServiceBackup` — storage-category service-kind strings
- `ResourceTypeS3Bucket`, `ResourceTypeEFS*`, `ResourceTypeFSx*`,
  `ResourceTypeStorageGateway*`, `ResourceTypeBackup*` — storage-category
  fact-schema resource-type tokens
- `RelationshipS3*`, `RelationshipEFS*`, `RelationshipFSx*`,
  `RelationshipStorageGateway*`, `RelationshipBackup*` — storage-category
  reducer relationship types
- `ServiceEC2`, `ServiceAutoScaling`, `ServiceLambda`, `ServiceECS`,
  `ServiceEKS`, `ServiceECR`, `ServiceBatch`, `ServiceLightsail` —
  compute-category service-kind strings
- `ResourceTypeEC2*`, `ResourceTypeAutoScaling*`, `ResourceTypeLambda*`,
  `ResourceTypeECS*`, `ResourceTypeEKS*`, `ResourceTypeECR*`,
  `ResourceTypeBatch*`, `ResourceTypeLightsail*` — compute-category
  fact-schema resource-type tokens
- `RelationshipEC2*`, `RelationshipAutoScaling*`, `RelationshipLambda*`,
  `RelationshipECS*`, `RelationshipEKS*`, `RelationshipBatch*`,
  `RelationshipLightsail*` — compute-category reducer relationship types
- `ServiceSNS`, `ServiceSQS`, `ServiceEventBridge`, `ServiceStepFunctions`,
  `ServiceMQ`, `ServiceMSK`, `ServiceSES`, `ServicePinpoint`,
  `ServiceFirehose` — messaging-category service-kind strings
- `ResourceTypeSNS*`, `ResourceTypeSQS*`, `ResourceTypeEventBridge*`,
  `ResourceTypeStepFunctions*`, `ResourceTypeMQ*`, `ResourceTypeMSK*`,
  `ResourceTypeSES*`, `ResourceTypePinpoint*`, `ResourceTypeFirehose*` —
  messaging-category fact-schema resource-type tokens
- `RelationshipSNS*`, `RelationshipSQS*`, `RelationshipEventBridge*`,
  `RelationshipStepFunctions*`, `RelationshipMQ*`, `RelationshipMSK*`,
  `RelationshipSES*`, `RelationshipPinpoint*`, `RelationshipFirehose*` —
  messaging-category reducer relationship types
- `ServiceAthena`, `ServiceEMR`, `ServiceGlue`, `ServiceKinesis`,
  `ServiceKinesisAnalyticsV2`, `ServiceOpenSearch`,
  `ServiceOpenSearchServerless` — analytics-category service-kind strings
  (query, processing, streaming, and search, in `analytics.go`)
- `ResourceTypeAthena*`, `ResourceTypeEMR*`, `ResourceTypeGlue*`,
  `ResourceTypeKinesis*`, `ResourceTypeManagedFlink*`,
  `ResourceTypeFirehose*`, `ResourceTypeSplunk*`,
  `ResourceTypeOpenSearch*` — analytics-category fact-schema resource-type
  tokens
- `RelationshipAthena*`, `RelationshipEMR*`, `RelationshipGlue*`,
  `RelationshipKinesis*`, `RelationshipManagedFlink*`,
  `RelationshipFirehoseDeliveryStream*`, `RelationshipOpenSearch*` —
  analytics-category reducer relationship types
- `ServiceQuickSight`, `ServiceCleanRooms`, `ServiceDatazone`,
  `ServiceDatabrew`, `ServiceDataSync` — analytics-category service-kind
  strings (BI, collaboration, governance, preparation, and movement, in
  `analytics_data.go`)
- `ResourceTypeQuickSight*`, `ResourceTypeCleanRooms*`,
  `ResourceTypeDatazone*`, `ResourceTypeDatabrew*`, `ResourceTypeDataSync*` —
  analytics-category fact-schema resource-type tokens
- `RelationshipQuickSight*`, `RelationshipCleanRooms*`,
  `RelationshipDatazone*`, `RelationshipDatabrew*`, `RelationshipDataSync*` —
  analytics-category reducer relationship types
- `ServiceSageMaker`, `ServiceBedrock` — analytics-category service-kind
  strings (machine learning, in `analytics_ml.go`)
- `ResourceTypeSageMaker*`, `ResourceTypeBedrock*` — analytics-category
  fact-schema resource-type tokens
- `RelationshipSageMaker*`, `RelationshipBedrock*` — analytics-category
  reducer relationship types
- `ServiceIAM`, `ServiceKMS`, `ServiceACM`, `ServiceACMPCA`,
  `ServiceSigner`, `ServiceSSOAdmin` — security-category service-kind
  strings (identity, access, and cryptography, in `security.go`)
- `ResourceTypeIAM*`, `ResourceTypeKMS*`, `ResourceTypeACMCertificate`,
  `ResourceTypeSigner*`, `ResourceTypeSSOAdmin*`, `IAMPolicySource*` —
  security-category fact-schema resource-type tokens
- `RelationshipIAM*`, `RelationshipKMS*`,
  `RelationshipACMCertificateUsedByResource`,
  `RelationshipACMPCA*`, `RelationshipSigner*`, `RelationshipSSOAdmin*` —
  security-category reducer relationship types
- `ServiceGuardDuty`, `ServiceSecurityHub`, `ServiceSecurityLake`,
  `ServiceDetective`, `ServiceInspector2`, `ServiceMacie`, `ServiceShield`,
  `ServiceFMS` — security-threat-category service-kind strings
  (detection and response, in `security_threat.go`)
- `ResourceTypeGuardDuty*`, `ResourceTypeSecurityHub*`,
  `ResourceTypeSecurityLake*`, `ResourceTypeDetective*`,
  `ResourceTypeInspector2*`, `ResourceTypeMacie*`, `ResourceTypeShield*`,
  `ResourceTypeFMSPolicy` — security-threat-category fact-schema
  resource-type tokens
- `RelationshipGuardDuty*`, `RelationshipSecurityHub*`,
  `RelationshipSecurityLake*`, `RelationshipDetective*`,
  `RelationshipInspector2*`, `RelationshipMacie*`,
  `RelationshipShield*`, `RelationshipFMS*` — security-threat-category
  reducer relationship types
- `ServiceVPC`, `ServiceVPCLattice`, `ServiceRoute53`,
  `ServiceRoute53Resolver`, `ServiceRoute53RecoveryControlConfig`,
  `ServiceDirectConnect`, `ServiceTransitGateway` — network-category
  service-kind strings (core fabric, DNS, and hybrid connectivity, in
  `network.go`)
- `ResourceTypeVPC*`, `ResourceTypeVPCLattice*`, `ResourceTypeRoute53*`,
  `ResourceTypeDirectConnect*`, `ResourceTypeTransitGateway*` —
  network-category fact-schema resource-type tokens
- `RelationshipVPC*`, `RelationshipVPCLattice*`,
  `RelationshipRoute53Resolver*`,
  `RelationshipRoute53RecoveryControlConfig*`,
  `RelationshipDirectConnect*`, `RelationshipTransitGateway*` —
  network-category reducer relationship types
- `ServiceNetworkManager`, `ServiceNetworkFirewall`, `ServiceELB`,
  `ServiceELBv2`, `ServiceGlobalAccelerator`, `ServiceServiceDiscovery`,
  `ServiceCloudFront` — network-edge-category service-kind strings (traffic
  delivery and service connectivity, in `network_edge.go`)
- `ResourceTypeNetworkManager*`, `ResourceTypeNetworkFirewall*`,
  `ResourceTypeELB*`, `ResourceTypeELBv2*`, `ResourceTypeGlobalAccelerator*`,
  `ResourceTypeCloudMap*`, `ResourceTypeCloudFront*` —
  network-edge-category fact-schema resource-type tokens
- `RelationshipNetworkManager*`, `RelationshipNetworkFirewall*`,
  `RelationshipELB*`, `RelationshipELBv2*`,
  `RelationshipGlobalAccelerator*`, `RelationshipCloudMap*`,
  `RelationshipCloudFront*` — network-edge-category reducer relationship
  types
- `ServiceRDS`, `ServiceDynamoDB`, `ServiceElastiCache`, `ServiceRedshift`,
  `ServiceMemoryDB`, `ServiceDocDB`, `ServiceDocDBElastic`, `ServiceNeptune` —
  database-category service-kind strings (relational, document, graph, and
  caching, in `database.go`)
- `ResourceTypeRDS*`, `ResourceTypeDynamoDB*`, `ResourceTypeElastiCache*`,
  `ResourceTypeRedshift*`, `ResourceTypeMemoryDB*`, `ResourceTypeDocDB*`,
  `ResourceTypeNeptune*` — database-category fact-schema resource-type tokens
- `RelationshipRDS*`, `RelationshipDynamoDB*`, `RelationshipElastiCache*`,
  `RelationshipRedshift*`, `RelationshipMemoryDB*`, `RelationshipDocDB*`,
  `RelationshipNeptune*` — database-category reducer relationship types
- `ServiceTimestream`, `ServiceKeyspaces`, `ServiceDAX`,
  `ServiceLakeFormation` — database-specialized-category service-kind strings
  (time-series, wide-column, accelerator, and lake governance, in
  `database_specialized.go`)
- `ResourceTypeTimestream*`, `ResourceTypeKeyspaces*`, `ResourceTypeDAX*`,
  `ResourceTypeLakeFormation*` — database-specialized-category fact-schema
  resource-type tokens
- `RelationshipTimestream*`, `RelationshipKeyspaces*`, `RelationshipDAX*`,
  `RelationshipLakeFormation*` — database-specialized-category reducer
  relationship types
- `ServiceAPIGateway`, `ServiceAPIGatewayV2`, `ServiceAppSync`,
  `ServiceAppConfig`, `ServiceAppRunner` — api-category service-kind strings
  (API gateways and application delivery, in `api.go`)
- `ResourceTypeAPIGateway*`, `ResourceTypeAPIGatewayV2*`,
  `ResourceTypeAppSync*`, `ResourceTypeAppConfig*`,
  `ResourceTypeAppRunner*` — api-category fact-schema resource-type tokens
- `RelationshipAPIGateway*`, `RelationshipAPIGatewayV2*`,
  `RelationshipAppSync*`, `RelationshipAppConfig*`,
  `RelationshipAppRunner*` — api-category reducer relationship types
- `ServiceSynthetics`, `ServiceServiceCatalog`, `ServiceProton`,
  `ServiceResilienceHub`, `ServiceFIS` — api-management-category service-kind
  strings (provisioning, testing, and resilience, in `api_management.go`)
- `ResourceTypeSynthetics*`, `ResourceTypeServiceCatalog*`,
  `ResourceTypeProton*`, `ResourceTypeResilienceHub*`, `ResourceTypeFIS*` —
  api-management-category fact-schema resource-type tokens
- `RelationshipSynthetics*`, `RelationshipServiceCatalog*`,
  `RelationshipProton*`, `RelationshipResilienceHub*`,
  `RelationshipFIS*` — api-management-category reducer relationship types
- `ServiceCodeArtifact`, `ServiceCodeBuild`, `ServiceCodeCommit`,
  `ServiceCodeDeploy`, `ServiceCodeGuru`, `ServiceCodePipeline` —
  devtools-category service-kind strings (CI/CD, in `devtools.go`)
- `ResourceTypeCodeArtifact*`, `ResourceTypeCodeBuild*`,
  `ResourceTypeCodeCommit*`, `ResourceTypeCodeDeploy*`,
  `ResourceTypeCodeGuru*`, `ResourceTypeCodePipeline*` — devtools-category
  fact-schema resource-type tokens
- `RelationshipCodeArtifact*`, `RelationshipCodeBuild*`,
  `RelationshipCodeCommit*`, `RelationshipCodeDeploy*`,
  `RelationshipCodeGuru*`, `RelationshipCodePipeline*` — devtools-category
  reducer relationship types
- `ServiceCloudFormation`, `ServiceConfig`, `ServiceControlTower`,
  `ServiceOrganizations`, `ServiceRAM`, `ServiceServiceQuotas`,
  `ServiceResourceGroups`, `ServiceLicenseManager`,
  `ServiceServiceCatalogAppRegistry` — management-category service-kind
  strings (governance and provisioning, in `management.go`)
- `ResourceTypeCloudFormation*`, `ResourceTypeConfig*`,
  `ResourceTypeControlTower*`, `ResourceTypeOrganizations*`,
  `ResourceTypeRAM*`, `ResourceTypeServiceQuotas*`,
  `ResourceTypeResourceGroups*`, `ResourceTypeLicenseManager*`,
  `ResourceTypeServiceCatalogAppRegistry*` — management-category fact-schema
  resource-type tokens
- `RelationshipCloudFormation*`, `RelationshipConfig*`,
  `RelationshipControlTower*`, `RelationshipOrganizations*`,
  `RelationshipRAM*`, `RelationshipServiceQuotas*`,
  `RelationshipResourceGroups*`, `RelationshipLicenseManager*`,
  `RelationshipServiceCatalogAppRegistry*` — management-category reducer
  relationship types
- `ServiceCloudTrail`, `ServiceCloudWatch`, `ServiceCloudWatchLogs`,
  `ServiceXRay`, `ServiceApplicationAutoScaling`, `ServiceComputeOptimizer`,
  `ServiceMWAA` — operations-category service-kind strings (trail, metrics,
  tracing, scaling, and workflows, in `operations.go`)
- `ResourceTypeCloudTrail*`, `ResourceTypeCloudWatch*`,
  `ResourceTypeCloudWatchLogs*`, `ResourceTypeXRay*`,
  `ResourceTypeApplicationAutoScaling*`, `ResourceTypeComputeOptimizer*`,
  `ResourceTypeMWAA*` — operations-category fact-schema resource-type tokens
- `RelationshipCloudTrail*`, `RelationshipCloudWatch*`,
  `RelationshipCloudWatchLogs*`, `RelationshipXRay*`,
  `RelationshipApplicationAutoScaling*`, `RelationshipComputeOptimizer*`,
  `RelationshipMWAA*` — operations-category reducer relationship types
- `ServiceAppMesh`, `ServiceAppStream`, `ServiceElasticBeanstalk`,
  `ServiceWorkSpaces` — g1-services-category service-kind
  strings (service mesh, streaming, desktops, and app hosting, in
  `g1_services.go`)
- `ResourceTypeAppMesh*`, `ResourceTypeAppStream*`,
  `ResourceTypeElasticBeanstalk*`, `ResourceTypeWorkSpaces*`,
  `ResourceTypeACMPCACertificateAuthority` — g1-services-category
  fact-schema resource-type tokens
- `RelationshipAppMesh*`, `RelationshipAppStream*`,
  `RelationshipElasticBeanstalk*`, `RelationshipWorkSpaces*`,
  `RelationshipAppMeshVirtualNodeTrustsCertificateAuthority` —
  g1-services-category reducer relationship types
- `ServiceAMP`, `ServiceCognito`, `ServiceDMS`, `ServiceImageBuilder`,
  `ServiceTransfer`, `ServiceVerifiedAccess` — g1-platform-category
  service-kind strings (migration, image build, access, file transfer,
  identity, and monitoring, in `g1_platform.go`)
- `ResourceTypeAMP*`, `ResourceTypeCognito*`, `ResourceTypeDMS*`,
  `ResourceTypeImageBuilder*`, `ResourceTypeTransfer*`,
  `ResourceTypeVerifiedAccess*` — g1-platform-category fact-schema
  resource-type tokens
- `RelationshipAMP*`, `RelationshipCognito*`, `RelationshipDMS*`,
  `RelationshipImageBuilder*`, `RelationshipTransfer*`,
  `RelationshipVerifiedAccess*` — g1-platform-category reducer
  relationship types
- `ServiceWAFv2`, `ServiceMGN`, `ServiceLocation`, `ServiceDirectoryService`,
  `ServiceAuditManager`, `ServiceAppFlow`, `ServiceAmplify`,
  `ServiceVerifiedPermissions`, `ServiceRolesAnywhere` — g2-services-category
  service-kind strings (in `g2_services.go`)
- `ResourceTypeWAFv2*`, `ResourceTypeMGN*`, `ResourceTypeLocation*`,
  `ResourceTypeDS*`, `ResourceTypeAuditManager*`, `ResourceTypeAppFlow*`,
  `ResourceTypeAmplify*`, `ResourceTypeVerifiedPermissions*`,
  `ResourceTypeRolesAnywhere*` — g2-services-category resource-type tokens
- `RelationshipWAFv2*`, `RelationshipMGN*`, `RelationshipLocation*`,
  `RelationshipDS*`, `RelationshipAuditManager*`, `RelationshipAppFlow*`,
  `RelationshipAmplify*`, `RelationshipVerifiedPermissions*`,
  `RelationshipRolesAnywhere*` — g2-services-category relationship types
- `ServiceCloudHSMV2`, `ServiceAccessAnalyzer`, `ServiceOutposts`,
  `ServiceDRS`, `ServiceGrafana`, `ServiceSecretsManager`, `ServiceSSM` —
  g2-governance-category service-kind strings (in `g2_governance.go`;
  `ResourcePolicy*` carries no service kind)
- `ResourceTypeCloudHSMV2*`, `ResourceTypeAccessAnalyzer*`,
  `ResourceTypeOutposts*`, `ResourceTypeDRS*`, `ResourceTypeGrafana*`,
  `ResourceTypeSecretsManager*`, `ResourceTypeSSM*`, `ResourcePolicy*` —
  g2-governance-category tokens
- `RelationshipCloudHSMV2*`, `RelationshipAccessAnalyzer*`,
  `RelationshipOutposts*`, `RelationshipDRS*`, `RelationshipGrafana*`,
  `RelationshipSecretsManager*`, `RelationshipSSM*` — g2-governance-category
  relationship types
- `ResourceTypeAWSAccount`, `ResourceTypeGeneric` — g2-common-category shared
  relationship targets (in `g2_common.go`)

See `doc.go` for the full contract. Each follow-up category lands as one more
`<category>.go` file with the same shape. The analytics category ships as
three files (`analytics.go` for query, processing, streaming, and search;
`analytics_data.go` for BI, collaboration, governance, preparation, and
movement; `analytics_ml.go` for machine learning) because one file would pass
the 500-line Go cap at ~989 lines. The security category ships as two files
(`security.go` for identity, access, and cryptography; `security_threat.go`
for detection and response) because one file would pass the 500-line Go cap;
all stay under it. The network category ships as two files
(`network.go` for core fabric, DNS, and hybrid connectivity;
`network_edge.go` for traffic delivery and service connectivity) because one
file would pass the 500-line Go cap; both stay under it. The database
category ships as two files (`database.go` for relational, document, graph,
and caching; `database_specialized.go` for time-series, wide-column,
accelerator, and lake governance) because one file would pass the 500-line
Go cap at ~632 lines; both stay under it. The API category ships as two
files (`api.go` for API gateways and application delivery;
`api_management.go` for provisioning, testing, and resilience) because one
file would pass the 500-line Go cap at ~623 lines; both stay under it. The
devtools category ships as three files (`devtools.go` for CI/CD;
`management.go` for governance and provisioning; `operations.go` for trail,
metrics, tracing, scaling, and workflows) because one file would pass the
500-line Go cap at ~1071 lines; all stay under it. The remaining
`constants_*` files stay in the awscloud root until their category lands.

## Dependencies

Almost none. The package imports only the standard library (tests only)
plus the published fact-schema contract (`sdk/go/factschema/aws/v1`) for
the eight EC2 resource-type tokens it already owned through that contract
(`compute.go` keeps the `awsv1.*` references verbatim, #6696). The contract
imports nothing back, so the leaf still cannot cycle. Anything that imports
the awscloud root or a service package cannot be imported here.

## Telemetry

None. String constants register no metrics, spans, or logs. The scanners
that consume these constants keep their existing counters; see
`docs/public/observability/telemetry-coverage.md`.

## Gotchas / invariants

- A consolidation move must never change a value. `storage_test.go`,
  `compute_test.go`, `messaging_test.go`, `analytics_test.go`,
  `security_test.go`, `network_test.go`, `database_test.go`, `api_test.go`,
  and `devtools_test.go` pin every constant byte-identical to its
  pre-consolidation value; a red test there is a behavior change, not a move.

## Consolidation proof

Per-lane move evidence lives in [`consolidation-proof.md`](consolidation-proof.md):
baseline values read from the base tree, byte-identical pins, affected-suite
results, and the no-regression / no-observability-change markers the
performance-evidence gate scans. Add one entry per follow-up category lane.

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/reference/multi-cloud-collector-contract.md`
- Issue #6696 (consolidation checklist) and epic #6692 (target tree)
