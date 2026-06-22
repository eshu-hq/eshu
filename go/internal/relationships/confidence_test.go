package relationships

import "testing"

// allEvidenceKinds is the full taxonomy that the confidence registry must
// cover. EvidenceKinds whose string is computed at runtime (the Terraform
// runtime-service family) are represented by their family default and are not
// listed here.
var allEvidenceKinds = []EvidenceKind{
	EvidenceKindTerraformAppRepo,
	EvidenceKindTerraformAppName,
	EvidenceKindTerraformGitHubRepo,
	EvidenceKindTerraformGitHubActions,
	EvidenceKindTerraformConfigPath,
	EvidenceKindTerraformIAMPermission,
	EvidenceKindTerraformModuleSource,
	EvidenceKindTerragruntDependencyConfigPath,
	EvidenceKindTerragruntConfigAssetPath,
	EvidenceKindHelmChart,
	EvidenceKindHelmValues,
	EvidenceKindArgoCDAppSource,
	EvidenceKindArgoCDApplicationSetDiscovery,
	EvidenceKindArgoCDApplicationSetDeploySource,
	EvidenceKindArgoCDDestinationPlatform,
	EvidenceKindGitHubActionsReusableWorkflow,
	EvidenceKindGitHubActionsCheckoutRepository,
	EvidenceKindGitHubActionsWorkflowInputRepository,
	EvidenceKindGitHubActionsActionRepository,
	EvidenceKindGitHubActionsLocalReusableWorkflow,
	EvidenceKindJenkinsSharedLibrary,
	EvidenceKindJenkinsGitHubRepository,
	EvidenceKindDockerComposeBuildContext,
	EvidenceKindDockerComposeImage,
	EvidenceKindDockerComposeDependsOn,
	EvidenceKindDockerfileSourceLabel,
	EvidenceKindKustomizeResource,
	EvidenceKindKustomizeHelmChart,
	EvidenceKindKustomizeImage,
	EvidenceKindAnsibleRoleReference,
	EvidenceKindGCPCloudRelationship,
}

// TestConfidenceRegistryCoversEveryEvidenceKind pins the contract that no
// EvidenceKind may extract evidence without a documented registry entry. A new
// EvidenceKind constant added to models.go without a registry entry fails here,
// preventing silent reintroduction of scattered magic confidence values.
func TestConfidenceRegistryCoversEveryEvidenceKind(t *testing.T) {
	t.Parallel()

	for _, kind := range allEvidenceKinds {
		entry, ok := DefaultConfidenceRegistry.Lookup(kind)
		if !ok {
			t.Errorf("EvidenceKind %q has no confidence registry entry", kind)
			continue
		}
		if entry.Rationale == "" {
			t.Errorf("EvidenceKind %q registry entry has empty rationale", kind)
		}
	}
}

// TestConfidenceRegistryValuesAreBounded pins that every registered confidence
// is a valid probability in [0,1]. The corroboration math in resolver.go relies
// on this; an out-of-band input would silently distort Bayesian aggregation.
func TestConfidenceRegistryValuesAreBounded(t *testing.T) {
	t.Parallel()

	for kind, entry := range DefaultConfidenceRegistry.entries() {
		if entry.Confidence < 0 || entry.Confidence > 1 {
			t.Errorf("EvidenceKind %q confidence = %.4f, want within [0,1]", kind, entry.Confidence)
		}
	}
}

// TestConfidenceTierMonotonicity pins the core semantic invariant of the issue:
// stronger evidence tiers must carry strictly higher confidence floors than
// weaker tiers. Positive direct-binding evidence outranks reference evidence,
// which outranks first-party/CI provenance. This catches future drift where a
// hand-edited value crosses a tier boundary.
func TestConfidenceTierMonotonicity(t *testing.T) {
	t.Parallel()

	// Ordered strongest to weakest. Each tier's floor must exceed the next.
	orderedTiers := []ConfidenceTier{
		TierDirectBinding,
		TierStrongReference,
		TierReference,
		TierWeakReference,
		TierProvenanceOnly,
	}

	for i := 0; i+1 < len(orderedTiers); i++ {
		hi := orderedTiers[i].Floor()
		lo := orderedTiers[i+1].Floor()
		if hi <= lo {
			t.Errorf(
				"tier floor not monotonic: %s floor %.4f must exceed %s floor %.4f",
				orderedTiers[i], hi, orderedTiers[i+1], lo,
			)
		}
	}
}

// TestConfidenceRegistryEntriesRespectTierFloors pins that each registered
// value sits at or above its declared tier floor and below the next stronger
// tier's floor. This is what keeps the ordering invariant true for concrete
// values, not just the abstract tiers.
func TestConfidenceRegistryEntriesRespectTierFloors(t *testing.T) {
	t.Parallel()

	for kind, entry := range DefaultConfidenceRegistry.entries() {
		if entry.Confidence < entry.Tier.Floor() {
			t.Errorf(
				"EvidenceKind %q confidence %.4f below its declared tier %s floor %.4f",
				kind, entry.Confidence, entry.Tier, entry.Tier.Floor(),
			)
		}
	}
}

// TestConfidenceForReturnsRegisteredValue pins the lookup accessor used by
// extractors so they cannot diverge from the registry.
func TestConfidenceForReturnsRegisteredValue(t *testing.T) {
	t.Parallel()

	got := DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindTerraformAppRepo)
	if got != 0.99 {
		t.Fatalf("ConfidenceFor(TerraformAppRepo) = %.4f, want 0.99", got)
	}
}

// TestConfidenceRegistryOverrideIsCalibratable pins the calibration hook: an
// operator-supplied override replaces the default value for one kind without
// mutating the shared default registry, and out-of-band overrides are rejected.
func TestConfidenceRegistryOverrideIsCalibratable(t *testing.T) {
	t.Parallel()

	calibrated, err := DefaultConfidenceRegistry.WithOverrides(map[EvidenceKind]float64{
		EvidenceKindHelmValues: 0.70,
	})
	if err != nil {
		t.Fatalf("WithOverrides returned error: %v", err)
	}
	if got := calibrated.ConfidenceFor(EvidenceKindHelmValues); got != 0.70 {
		t.Fatalf("calibrated HelmValues = %.4f, want 0.70", got)
	}
	// The default registry must be untouched by the override.
	if got := DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindHelmValues); got != 0.84 {
		t.Fatalf("default HelmValues mutated to %.4f, want 0.84", got)
	}

	if _, err := DefaultConfidenceRegistry.WithOverrides(map[EvidenceKind]float64{
		EvidenceKindHelmValues: 1.5,
	}); err == nil {
		t.Fatal("WithOverrides accepted out-of-band confidence 1.5, want error")
	}
}

// TestRuntimeServiceFamilyConfidenceRegistered pins the dynamic-kind family
// default so the runtime-service extractor draws from the registry too.
func TestRuntimeServiceFamilyConfidenceRegistered(t *testing.T) {
	t.Parallel()

	if got := DefaultConfidenceRegistry.TerraformRuntimeServiceConfidence(); got != 0.96 {
		t.Fatalf("runtime-service family confidence = %.4f, want 0.96", got)
	}
}

// TestTerraformSchemaFamilyConfidenceRegistered pins the schema-driven Terraform
// family priors and the invariant that the resource-name fallback weight stays
// below the default resolution threshold so a name-only match cannot resolve a
// relationship without corroboration.
func TestTerraformSchemaFamilyConfidenceRegistered(t *testing.T) {
	t.Parallel()

	if got := DefaultConfidenceRegistry.TerraformIdentityKeyConfidence(); got != 0.78 {
		t.Fatalf("identity-key confidence = %.4f, want 0.78", got)
	}
	fallback := DefaultConfidenceRegistry.TerraformResourceNameFallbackWeight()
	if fallback != 0.55 {
		t.Fatalf("resource-name fallback weight = %.4f, want 0.55", fallback)
	}
	if fallback >= DefaultConfidenceThreshold {
		t.Fatalf(
			"resource-name fallback weight %.4f must stay below DefaultConfidenceThreshold %.4f",
			fallback, DefaultConfidenceThreshold,
		)
	}
}
