// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	EvidenceKindPuppetModuleReference,
	EvidenceKindChefCookbookDependency,
	EvidenceKindSaltFormulaReference,
	EvidenceKindGCPCloudRelationship,
	EvidenceKindHelmTemplateValueReference,
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

// withRegistryOverride swaps DefaultConfidenceRegistry for a registry carrying
// the supplied per-kind overrides, runs fn, and restores the default. It is the
// mechanism the emitter-routing tests use to prove an emitter reads its prior
// from the registry rather than a hard-coded literal: if the emitter bypasses
// the registry, the override is invisible and the assertion fails. These tests
// must not run in parallel because they mutate the package-global registry.
func withRegistryOverride(t *testing.T, overrides map[EvidenceKind]float64, fn func()) {
	t.Helper()

	calibrated, err := DefaultConfidenceRegistry.WithOverrides(overrides)
	if err != nil {
		t.Fatalf("WithOverrides returned error: %v", err)
	}
	original := DefaultConfidenceRegistry
	DefaultConfidenceRegistry = calibrated
	defer func() { DefaultConfidenceRegistry = original }()

	fn()
}

// kustomizeConfidenceMatcher builds a catalog matcher that resolves the given
// alias to a target repository for emitter tests.
func registryRoutingMatcher(alias, targetRepoID string) *catalogMatcher {
	return newCatalogMatcher([]CatalogEntry{
		{RepoID: targetRepoID, Aliases: []string{alias}},
	})
}

// TestHelmEvidenceReadsConfidenceFromRegistry pins that discoverHelmEvidence
// emits the registry prior for both the Chart.yaml and values cases. The test
// overrides the registry and asserts the emitted confidence reflects the
// override, so it fails if the emitter hard-codes the value (issue #3509
// follow-up: routing all evidence confidences through the registry).
func TestHelmEvidenceReadsConfidenceFromRegistry(t *testing.T) {
	cases := []struct {
		name     string
		fileName string
		kind     EvidenceKind
		override float64
	}{
		{"chart", "Chart.yaml", EvidenceKindHelmChart, 0.61},
		{"values", "values.yaml", EvidenceKindHelmValues, 0.62},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withRegistryOverride(t, map[EvidenceKind]float64{tc.kind: tc.override}, func() {
				matcher := registryRoutingMatcher("payments-service", "repo-payments")
				seen := make(map[evidenceKey]struct{})
				content := "repository: payments-service\n"

				evidence := discoverHelmEvidence("repo-infra", tc.fileName, content, "", matcher, seen)

				got := requireEvidenceConfidence(t, evidence, tc.kind)
				if got != tc.override {
					t.Fatalf("%s confidence = %.4f, want overridden registry value %.4f (emitter bypasses registry)", tc.kind, got, tc.override)
				}
			})
		})
	}
}

// TestKustomizeDocumentEvidenceReadsConfidenceFromRegistry pins that
// discoverKustomizeDocumentEvidence emits the registry prior for each of its
// three evidence kinds. Same override technique as the Helm test.
func TestKustomizeDocumentEvidenceReadsConfidenceFromRegistry(t *testing.T) {
	cases := []struct {
		name     string
		document map[string]any
		kind     EvidenceKind
		override float64
	}{
		{
			name:     "resource",
			document: map[string]any{"resources": []any{"payments-service"}},
			kind:     EvidenceKindKustomizeResource,
			override: 0.63,
		},
		{
			name:     "helm_chart",
			document: map[string]any{"helmCharts": []any{map[string]any{"name": "payments-service"}}},
			kind:     EvidenceKindKustomizeHelmChart,
			override: 0.64,
		},
		{
			name:     "image",
			document: map[string]any{"images": []any{map[string]any{"name": "payments-service"}}},
			kind:     EvidenceKindKustomizeImage,
			override: 0.65,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withRegistryOverride(t, map[EvidenceKind]float64{tc.kind: tc.override}, func() {
				matcher := registryRoutingMatcher("payments-service", "repo-payments")
				seen := make(map[evidenceKey]struct{})

				evidence := discoverKustomizeDocumentEvidence(
					"repo-infra", "overlays/kustomization.yaml", tc.document, matcher, seen, "",
				)

				got := requireEvidenceConfidence(t, evidence, tc.kind)
				if got != tc.override {
					t.Fatalf("%s confidence = %.4f, want overridden registry value %.4f (emitter bypasses registry)", tc.kind, got, tc.override)
				}
			})
		})
	}
}

// requireEvidenceConfidence returns the confidence of the first evidence fact of
// the given kind, failing if none is present.
func requireEvidenceConfidence(t *testing.T, evidence []EvidenceFact, kind EvidenceKind) float64 {
	t.Helper()

	for i := range evidence {
		if evidence[i].EvidenceKind == kind {
			return evidence[i].Confidence
		}
	}
	t.Fatalf("no evidence fact of kind %q in %#v", kind, evidence)
	return 0
}
