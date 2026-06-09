# Fact Kind Registries

`internal/facts` keeps one registry helper per fact family. Callers should use
these helpers when building envelopes, checking schema versions, or deciding
whether a component fact kind is core-owned.

## Aggregate Core Registry

- `CoreFactKinds()` returns the sorted, de-duplicated set of core-owned fact
  kinds.
- `IsCoreFactKind(kind)` checks one kind against that aggregate set.

Optional component validation uses the aggregate registry to reject core fact
kind claims before a component can install or enable.

## Family Helpers

Each family helper returns a cloned slice so callers cannot mutate package
state. Each schema-version helper returns the accepted schema version for a
known kind and `ok=false` for unknown input.

| Family | Kinds helper | Schema helper |
| --- | --- | --- |
| AWS cloud | `AWSFactKinds()` | `AWSSchemaVersion(kind)` |
| CI/CD runs | `CICDRunFactKinds()` | `CICDRunSchemaVersion(kind)` |
| Documentation | constants and payload helpers | kind-specific constants |
| EC2 posture | `EC2InstancePostureFactKinds()` | `EC2InstancePostureSchemaVersion(kind)` |
| GCP cloud | `GCPFactKinds()` | `GCPSchemaVersion(kind)` |
| Incident context | `IncidentContextFactKinds()` | `IncidentContextSchemaVersion(kind)` |
| Incident routing | `IncidentRoutingFactKinds()` | `IncidentRoutingSchemaVersion(kind)` |
| Kubernetes live | `KubernetesLiveFactKinds()` | `KubernetesLiveSchemaVersion(kind)` |
| Observability | `ObservabilityFactKinds()` | `ObservabilitySchemaVersion(kind)` |
| OCI registry | `OCIRegistryFactKinds()` | `OCIRegistrySchemaVersion(kind)` |
| Package registry | `PackageRegistryFactKinds()` | `PackageRegistrySchemaVersion(kind)` |
| RDS posture | `RDSPostureFactKinds()` | `RDSPostureSchemaVersion(kind)` |
| S3 bucket posture | `S3BucketPostureFactKinds()` | `S3BucketPostureSchemaVersion(kind)` |
| S3 external principal grants | `S3ExternalPrincipalGrantFactKinds()` | `S3ExternalPrincipalGrantSchemaVersion(kind)` |
| SBOM and attestations | `SBOMAttestationFactKinds()` | `SBOMAttestationSchemaVersion(kind)` |
| Scanner worker | `ScannerWorkerFactKinds()` | `ScannerWorkerSchemaVersion(kind)` |
| Secrets/IAM posture | `SecretsIAMFactKinds()` | `SecretsIAMSchemaVersion(kind)` |
| Security alerts | `SecurityAlertFactKinds()` | `SecurityAlertSchemaVersion(kind)` |
| Semantic evidence | `SemanticFactKinds()` | `SemanticSchemaVersion(kind)` |
| Service catalog | `ServiceCatalogFactKinds()` | `ServiceCatalogSchemaVersion(kind)` |
| Terraform state | `TerraformStateFactKinds()` | `TerraformStateSchemaVersion(kind)` |
| Vulnerability intelligence | `VulnerabilityIntelligenceFactKinds()` | `VulnerabilityIntelligenceSchemaVersion(kind)` |
| Vulnerability suppression | `VulnerabilitySuppressionFactKinds()` | `VulnerabilitySuppressionSchemaVersion(kind)` |
| Work items | `WorkItemFactKinds()` | `WorkItemSchemaVersion(kind)` |

## Ownership Notes

Core fact kinds belong to Eshu. Optional components must claim their own
namespaced kinds and must not copy a core kind even when the payload looks
similar. If a new first-party fact family lands, update its family helper, the
aggregate registry tests, and the public fact-envelope reference in the same
change.
