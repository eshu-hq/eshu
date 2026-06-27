// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"fmt"
	"sort"
)

// ConfidenceTier is a documented band of per-extractor confidence. Tiers exist
// so the relative ordering of evidence strength is explicit and testable rather
// than implied by scattered float literals. Every registry value belongs to one
// tier and must sit at or above that tier's Floor and below the next stronger
// tier's Floor. This is the calibration backbone for issue #3490: the absolute
// numbers may be re-derived from a golden set later, but the tier ordering is a
// fixed semantic contract (direct binding outranks reference, reference
// outranks heuristic value matches).
type ConfidenceTier string

const (
	// TierDirectBinding is an explicit, machine-authored identity binding to a
	// concrete repository: a Terraform app_repo, a GitHub repository URL, a
	// module source, an Argo CD Application source, or an ApplicationSet
	// generator binding. False positives are rare because the target is named
	// directly, not inferred.
	TierDirectBinding ConfidenceTier = "DIRECT_BINDING"
	// TierStrongReference is a named or path reference that strongly implies a
	// single target but is one inference step removed from a direct binding:
	// app_name matches, config paths, Helm chart metadata, explicit checkout
	// repositories, and IAM permission subjects.
	TierStrongReference ConfidenceTier = "STRONG_REFERENCE"
	// TierReference is a structured reference whose target is concrete but whose
	// link to deployment intent is weaker: Compose image references, Kustomize
	// images, action dependencies, and local reusable workflows.
	TierReference ConfidenceTier = "REFERENCE"
	// TierWeakReference is a value- or convention-matched reference that should
	// pass the default threshold only with corroboration in practice: Helm
	// values references, Compose depends_on edges, and single-provider GCP
	// relationship endpoints.
	TierWeakReference ConfidenceTier = "WEAK_REFERENCE"
	// TierProvenanceOnly is reserved for low-trust CI/controller provenance
	// signals that describe where automation runs rather than what is deployed.
	// Such signals must stay below DefaultConfidenceThreshold so they remain
	// provenance-only until corroborated. No relationships extractor currently
	// emits at this tier; it exists so future controller-only evidence has a
	// documented home instead of a fresh magic number.
	TierProvenanceOnly ConfidenceTier = "PROVENANCE_ONLY"
)

// tierFloors maps each tier to its inclusive lower bound. Floors are strictly
// decreasing across the strength ordering; see TestConfidenceTierMonotonicity.
var tierFloors = map[ConfidenceTier]float64{
	TierDirectBinding:   0.95,
	TierStrongReference: 0.90,
	TierReference:       0.86,
	TierWeakReference:   0.80,
	TierProvenanceOnly:  0.0,
}

// Floor returns the inclusive minimum confidence for the tier. Unknown tiers
// return 0 so an unclassified value can never claim a strength floor it has not
// declared.
func (t ConfidenceTier) Floor() float64 {
	return tierFloors[t]
}

// ConfidenceEntry is one registered per-extractor confidence value with the
// provenance needed to audit or recalibrate it: the tier it belongs to and the
// rationale describing why the value is what it is. Rationale is mandatory so a
// value can never reenter the codebase as an undocumented magic number.
type ConfidenceEntry struct {
	// Confidence is the per-fact prior emitted by the extractor for this kind.
	// It is a probability in [0,1] and feeds the Bayesian corroboration math in
	// aggregateEvidenceConfidence.
	Confidence float64
	// Tier is the documented strength band this value belongs to.
	Tier ConfidenceTier
	// Rationale explains the provenance of the value: what signal it scores and
	// why that signal earns this strength.
	Rationale string
}

// ConfidenceRegistry is the single source of truth for per-EvidenceKind
// confidence priors. It replaces the float literals that were previously
// scattered across every extractor file. The registry is immutable after
// construction; callers that want to recalibrate build a derived registry with
// WithOverrides so the shared default is never mutated.
type ConfidenceRegistry struct {
	byKind                   map[EvidenceKind]ConfidenceEntry
	runtimeServiceConfidence float64
	runtimeServiceTier       ConfidenceTier

	// terraformIdentityKeyConfidence scores a schema-driven Terraform resource
	// match made on a declared identity key (a named attribute such as
	// repository or name). The schema extractor emits a dynamically named
	// EvidenceKind per resource type, so this prior is keyed by family rather
	// than by a single EvidenceKind constant.
	terraformIdentityKeyConfidence float64
	// terraformResourceNameFallbackWeight scores a weaker schema-driven match
	// made only on the resource block name when no identity key matched. It sits
	// below DefaultConfidenceThreshold on purpose: a name-only match must not
	// resolve a relationship without corroboration.
	terraformResourceNameFallbackWeight float64
}

// DefaultConfidenceRegistry holds the calibrated-by-hand confidence priors that
// the relationships extractors emit. The values match the historical
// per-extractor literals exactly; centralizing them is the structural fix for
// issue #3490. Recalibration from a golden set replaces these numbers in one
// place and is validated by the tier and bound invariants in confidence_test.go.
var DefaultConfidenceRegistry = newDefaultConfidenceRegistry()

func newDefaultConfidenceRegistry() *ConfidenceRegistry {
	return &ConfidenceRegistry{
		runtimeServiceConfidence:            0.96,
		runtimeServiceTier:                  TierDirectBinding,
		terraformIdentityKeyConfidence:      0.78,
		terraformResourceNameFallbackWeight: 0.55,
		byKind: map[EvidenceKind]ConfidenceEntry{
			EvidenceKindTerraformAppRepo: {
				Confidence: 0.99, Tier: TierDirectBinding,
				Rationale: "app_repo names the target repository directly in provisioning config",
			},
			EvidenceKindTerraformGitHubRepo: {
				Confidence: 0.98, Tier: TierDirectBinding,
				Rationale: "an explicit github.com repository URL resolves to exactly one target",
			},
			EvidenceKindTerraformModuleSource: {
				Confidence: 0.98, Tier: TierDirectBinding,
				Rationale: "a module source path binds to one concrete module repository",
			},
			EvidenceKindTerraformGitHubActions: {
				Confidence: 0.97, Tier: TierDirectBinding,
				Rationale: "a GitHub Actions OIDC subject names the owning repository",
			},
			EvidenceKindArgoCDAppSource: {
				Confidence: 0.95, Tier: TierDirectBinding,
				Rationale: "an Argo CD Application source repoURL is an explicit deployment source",
			},
			EvidenceKindArgoCDApplicationSetDiscovery: {
				Confidence: 0.99, Tier: TierDirectBinding,
				Rationale: "an ApplicationSet generator enumerates concrete source repositories",
			},
			EvidenceKindArgoCDApplicationSetDeploySource: {
				Confidence: 0.99, Tier: TierDirectBinding,
				Rationale: "an ApplicationSet template binds a generated app to one deploy source",
			},
			EvidenceKindArgoCDDestinationPlatform: {
				Confidence: 0.97, Tier: TierDirectBinding,
				Rationale: "an ApplicationSet destination names the concrete target platform",
			},
			EvidenceKindTerraformAppName: {
				Confidence: 0.94, Tier: TierStrongReference,
				Rationale: "app_name matches a target repository name; one inference step from a direct repo binding",
			},
			EvidenceKindDockerfileSourceLabel: {
				Confidence: 0.93, Tier: TierStrongReference,
				Rationale: "an OCI source label is an authored, explicit repository reference",
			},
			EvidenceKindGitHubActionsReusableWorkflow: {
				Confidence: 0.93, Tier: TierStrongReference,
				Rationale: "a reusable workflow ref names the repository hosting the deployment logic",
			},
			EvidenceKindTerraformIAMPermission: {
				Confidence: 0.92, Tier: TierStrongReference,
				Rationale: "an IAM permission subject ties provisioning to a named principal repository",
			},
			EvidenceKindJenkinsGitHubRepository: {
				Confidence: 0.92, Tier: TierStrongReference,
				Rationale: "an explicit GitHub repository URL inside Jenkins automation is unambiguous",
			},
			EvidenceKindAnsibleRoleReference: {
				Confidence: 0.92, Tier: TierStrongReference,
				Rationale: "an Ansible role reference names the role-owning repository",
			},
			EvidenceKindPuppetModuleReference: {
				Confidence: 0.90, Tier: TierStrongReference,
				Rationale: "a Puppetfile mod git source names the module-owning repository",
			},
			EvidenceKindChefCookbookDependency: {
				Confidence: 0.90, Tier: TierStrongReference,
				Rationale: "a Berksfile cookbook git source names the cookbook-owning repository",
			},
			EvidenceKindHelmTemplateValueReference: {
				Confidence: 0.90, Tier: TierStrongReference,
				Rationale: "a chart template `{{ .Values.<path> }}` reads a leaf key defined in the same chart's values.yaml",
			},
			EvidenceKindDockerComposeBuildContext: {
				Confidence: 0.91, Tier: TierStrongReference,
				Rationale: "a Compose build context path points at a buildable source repository",
			},
			EvidenceKindGitHubActionsCheckoutRepository: {
				Confidence: 0.91, Tier: TierStrongReference,
				Rationale: "an explicit actions/checkout repo names the config/automation source",
			},
			EvidenceKindHelmChart: {
				Confidence: 0.90, Tier: TierStrongReference,
				Rationale: "Helm Chart.yaml metadata references the packaged target repository",
			},
			EvidenceKindTerraformConfigPath: {
				Confidence: 0.90, Tier: TierStrongReference,
				Rationale: "a config path segment matches a target repository name",
			},
			EvidenceKindTerragruntDependencyConfigPath: {
				Confidence: 0.90, Tier: TierStrongReference,
				Rationale: "a Terragrunt dependency config_path resolves to one config repository",
			},
			EvidenceKindKustomizeResource: {
				Confidence: 0.90, Tier: TierStrongReference,
				Rationale: "a Kustomize resource reference sources deployment config from the target",
			},
			EvidenceKindGitHubActionsWorkflowInputRepository: {
				Confidence: 0.90, Tier: TierStrongReference,
				Rationale: "a repo-bearing workflow input passes an explicit automation/config repository",
			},
			EvidenceKindJenkinsSharedLibrary: {
				Confidence: 0.89, Tier: TierReference,
				Rationale: "a Jenkins shared library reference names the library repository indirectly",
			},
			EvidenceKindKustomizeHelmChart: {
				Confidence: 0.89, Tier: TierReference,
				Rationale: "a Kustomize Helm chart reference points at the chart-owning repository",
			},
			EvidenceKindDockerComposeImage: {
				Confidence: 0.88, Tier: TierReference,
				Rationale: "a Compose image reference maps to a build/source repository",
			},
			EvidenceKindGitHubActionsActionRepository: {
				Confidence: 0.88, Tier: TierReference,
				Rationale: "a step-level action repository is a dependency, not a deploy source",
			},
			EvidenceKindTerragruntConfigAssetPath: {
				Confidence: 0.88, Tier: TierReference,
				Rationale: "a Terragrunt helper/local asset path references a config repository",
			},
			EvidenceKindKustomizeImage: {
				Confidence: 0.86, Tier: TierReference,
				Rationale: "a Kustomize image reference maps to a source repository by image name",
			},
			EvidenceKindGitHubActionsLocalReusableWorkflow: {
				Confidence: 0.86, Tier: TierReference,
				Rationale: "a repo-local reusable workflow is a same-repo deployment-logic reference",
			},
			EvidenceKindHelmValues: {
				Confidence: 0.84, Tier: TierWeakReference,
				Rationale: "a Helm values reference is a convention match that needs corroboration",
			},
			EvidenceKindDockerComposeDependsOn: {
				Confidence: 0.84, Tier: TierWeakReference,
				Rationale: "a Compose depends_on edge implies a service dependency, not a source binding",
			},
			EvidenceKindGCPCloudRelationship: {
				Confidence: 0.82, Tier: TierWeakReference,
				Rationale: "a single-provider GCP endpoint resolves to one repo only when each side is unambiguous",
			},
		},
	}
}

// Lookup returns the registered entry for a kind and whether it exists.
func (r *ConfidenceRegistry) Lookup(kind EvidenceKind) (ConfidenceEntry, bool) {
	entry, ok := r.byKind[kind]
	return entry, ok
}

// ConfidenceFor returns the registered confidence for a kind, or 0 when the
// kind is unregistered. Returning 0 is deliberate: an unregistered kind must
// never silently inherit a passing confidence.
func (r *ConfidenceRegistry) ConfidenceFor(kind EvidenceKind) float64 {
	return r.byKind[kind].Confidence
}

// TerraformRuntimeServiceConfidence returns the confidence prior for the
// Terraform runtime-service module family, whose EvidenceKind string is
// computed per platform (TERRAFORM_<PLATFORM>_SERVICE) and therefore cannot be
// keyed by a single constant.
func (r *ConfidenceRegistry) TerraformRuntimeServiceConfidence() float64 {
	return r.runtimeServiceConfidence
}

// TerraformIdentityKeyConfidence returns the prior for a schema-driven
// Terraform resource matched on a declared identity key.
func (r *ConfidenceRegistry) TerraformIdentityKeyConfidence() float64 {
	return r.terraformIdentityKeyConfidence
}

// TerraformResourceNameFallbackWeight returns the weaker prior for a
// schema-driven Terraform resource matched only on its block name. It is below
// DefaultConfidenceThreshold so a name-only match needs corroboration to
// resolve.
func (r *ConfidenceRegistry) TerraformResourceNameFallbackWeight() float64 {
	return r.terraformResourceNameFallbackWeight
}

// entries exposes the backing map for invariant tests in the same package. It
// is unexported because external callers must go through Lookup/ConfidenceFor.
func (r *ConfidenceRegistry) entries() map[EvidenceKind]ConfidenceEntry {
	return r.byKind
}

// WithOverrides returns a new registry with the supplied per-kind confidence
// overrides applied. This is the calibration hook: an operator or a future
// golden-set calibration step can replace specific priors without editing
// source or mutating the shared default. Overrides are validated to be in
// [0,1]; an out-of-band value is rejected. Tier classification is recomputed
// from tier floors so an overridden value keeps a truthful tier label.
func (r *ConfidenceRegistry) WithOverrides(overrides map[EvidenceKind]float64) (*ConfidenceRegistry, error) {
	clone := &ConfidenceRegistry{
		byKind:                              make(map[EvidenceKind]ConfidenceEntry, len(r.byKind)),
		runtimeServiceConfidence:            r.runtimeServiceConfidence,
		runtimeServiceTier:                  r.runtimeServiceTier,
		terraformIdentityKeyConfidence:      r.terraformIdentityKeyConfidence,
		terraformResourceNameFallbackWeight: r.terraformResourceNameFallbackWeight,
	}
	for kind, entry := range r.byKind {
		clone.byKind[kind] = entry
	}

	for _, kind := range sortedOverrideKeys(overrides) {
		value := overrides[kind]
		if value < 0 || value > 1 {
			return nil, fmt.Errorf("override confidence for %q = %.4f, must be within [0,1]", kind, value)
		}
		existing, ok := clone.byKind[kind]
		if !ok {
			return nil, fmt.Errorf("override for unregistered EvidenceKind %q", kind)
		}
		existing.Confidence = value
		existing.Tier = tierForConfidence(value)
		existing.Rationale = existing.Rationale + " (calibrated override)"
		clone.byKind[kind] = existing
	}

	return clone, nil
}

// tierForConfidence classifies a value into the strongest tier whose floor it
// meets. Floors are checked strongest-first so a high value claims the highest
// tier it qualifies for.
func tierForConfidence(value float64) ConfidenceTier {
	ordered := []ConfidenceTier{
		TierDirectBinding,
		TierStrongReference,
		TierReference,
		TierWeakReference,
		TierProvenanceOnly,
	}
	for _, tier := range ordered {
		if value >= tier.Floor() {
			return tier
		}
	}
	return TierProvenanceOnly
}

// sortedOverrideKeys returns override keys in deterministic order so override
// application and any error reporting are reproducible.
func sortedOverrideKeys(overrides map[EvidenceKind]float64) []EvidenceKind {
	keys := make([]EvidenceKind, 0, len(overrides))
	for kind := range overrides {
		keys = append(keys, kind)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
