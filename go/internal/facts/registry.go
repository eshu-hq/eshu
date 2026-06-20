package facts

import (
	"slices"
	"strings"
)

// CoreFactKinds returns every fact kind owned by the Eshu core data plane.
func CoreFactKinds() []string {
	kinds := DocumentationFactKinds()
	for _, family := range [][]string{
		AWSFactKinds(),
		CICDRunFactKinds(),
		EC2InstancePostureFactKinds(),
		GCPFactKinds(),
		IncidentContextFactKinds(),
		IncidentRoutingFactKinds(),
		KubernetesLiveFactKinds(),
		ObservabilityFactKinds(),
		OCIRegistryFactKinds(),
		PackageRegistryFactKinds(),
		RDSPostureFactKinds(),
		S3BucketPostureFactKinds(),
		S3ExternalPrincipalGrantFactKinds(),
		SBOMAttestationFactKinds(),
		ScannerWorkerFactKinds(),
		SecretsIAMFactKinds(),
		SecurityAlertFactKinds(),
		SemanticFactKinds(),
		ServiceCatalogFactKinds(),
		TerraformStateFactKinds(),
		VulnerabilityIntelligenceFactKinds(),
		VulnerabilitySuppressionFactKinds(),
		WorkItemFactKinds(),
	} {
		kinds = append(kinds, family...)
	}
	slices.Sort(kinds)
	return slices.Compact(kinds)
}

// IsCoreFactKind reports whether kind is reserved by Eshu core.
func IsCoreFactKind(kind string) bool {
	trimmed := strings.TrimSpace(kind)
	if trimmed == "" {
		return false
	}
	_, ok := slices.BinarySearch(CoreFactKinds(), trimmed)
	return ok
}
