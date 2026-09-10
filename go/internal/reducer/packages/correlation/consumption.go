// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import (
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// PackageConsumptionOutcome names the reducer decision for a repository
// manifest dependency matched to package-registry identity.
type PackageConsumptionOutcome string

const (
	// PackageConsumptionManifestDeclared means a Git manifest dependency names
	// the same ecosystem package identity observed in a package registry.
	PackageConsumptionManifestDeclared PackageConsumptionOutcome = "manifest_declared"
)

// PackageConsumptionDecision records one repo-to-package consumption
// correlation admitted from source declaration plus registry identity.
type PackageConsumptionDecision struct {
	PackageID                 string
	Ecosystem                 string
	PackageName               string
	RepositoryID              string
	RepositoryName            string
	RelativePath              string
	ManifestSection           string
	DependencyRange           string
	ObservedVersion           string
	RequestedRange            string
	InstalledVersion          string
	DependencyScope           string
	PrivateAssets             string
	IncludeAssets             string
	ExcludeAssets             string
	DevelopmentOnly           bool
	TestDependency            bool
	VersionEvidence           string
	UnresolvedMSBuildProperty string
	AmbiguousMSBuildProperty  string
	PackageAPIPackages        []string
	PackageAPIIdentitySource  string
	DependencyResolutionState string
	SourceSet                 string
	GeneratedCode             *bool
	PartialEvidence           bool
	DependencyPath            []string
	DependencyDepth           int
	DirectDependency          *bool
	Lockfile                  bool
	Outcome                   PackageConsumptionOutcome
	Reason                    string
	ProvenanceOnly            bool
	CanonicalWrites           int
	EvidenceFactIDs           []string
}

// PackageManifestDependencyFactFilter bounds the active Git dependency facts
// loaded for one package-registry reducer intent.
type PackageManifestDependencyFactFilter struct {
	Ecosystems    []string
	PackageNames  []string
	PackageIDs    []string
	SourceScopeID string
}

// PackageRegistryIdentity is one package-registry package fact, reduced to
// the identity and name forms consumption matching joins on. Exported because
// the code-import family resolves owners through the same identities the
// consumption builder uses.
type PackageRegistryIdentity struct {
	PackageID string
	Ecosystem string
	Names     []string
}

// PackageManifestDependency is one Git manifest or lockfile dependency fact,
// reduced to the identity and version evidence package correlation matches
// against package-registry identities. Exported because supply-chain impact
// and the security-alert bridge read manifest dependencies without going
// through the consumption builder.
type PackageManifestDependency struct {
	FactID                    string
	RepositoryID              string
	RepositoryName            string
	RelativePath              string
	ObservedAt                time.Time
	DependencyName            string
	PackageNamespace          string
	PackageManager            string
	ManifestSection           string
	DependencyRange           string
	ObservedVersion           string
	RequestedRange            string
	InstalledVersion          string
	DependencyScope           string
	PrivateAssets             string
	IncludeAssets             string
	ExcludeAssets             string
	DevelopmentOnly           bool
	TestDependency            bool
	VersionEvidence           string
	UnresolvedMSBuildProperty string
	AmbiguousMSBuildProperty  string
	PackageAPIPackages        []string
	PackageAPIIdentitySource  string
	DependencyResolutionState string
	SourceSet                 string
	GeneratedCode             *bool
	PartialEvidence           bool
	DependencyPath            []string
	DependencyDepth           int
	DirectDependency          *bool
	Lockfile                  bool
	SourceAmbiguous           bool
}

// BuildPackageConsumptionDecisions matches package registry identities to Git
// manifest dependency facts. It only admits source-declared consumption; package
// name similarity outside manifest evidence is ignored.
func BuildPackageConsumptionDecisions(envelopes []facts.Envelope) []PackageConsumptionDecision {
	identities := ExtractPackageRegistryIdentities(envelopes)
	dependencies := ExtractPackageManifestDependencies(envelopes)
	identityByKey := make(map[string]PackageRegistryIdentity)
	for _, identity := range identities {
		for _, name := range identity.Names {
			for _, key := range PackageConsumptionKeys(identity.Ecosystem, name) {
				identityByKey[key] = identity
			}
		}
	}

	decisions := make([]PackageConsumptionDecision, 0)
	for _, dependency := range dependencies {
		identity, ok := packageConsumptionIdentityForDependency(identityByKey, dependency)
		if !ok {
			continue
		}
		decisions = append(decisions, PackageConsumptionDecision{
			PackageID:                 identity.PackageID,
			Ecosystem:                 identity.Ecosystem,
			PackageName:               dependency.DependencyName,
			RepositoryID:              dependency.RepositoryID,
			RepositoryName:            dependency.RepositoryName,
			RelativePath:              dependency.RelativePath,
			ManifestSection:           dependency.ManifestSection,
			DependencyRange:           dependency.DependencyRange,
			ObservedVersion:           dependency.ObservedVersion,
			RequestedRange:            dependency.RequestedRange,
			InstalledVersion:          dependency.InstalledVersion,
			DependencyScope:           dependency.DependencyScope,
			PrivateAssets:             dependency.PrivateAssets,
			IncludeAssets:             dependency.IncludeAssets,
			ExcludeAssets:             dependency.ExcludeAssets,
			DevelopmentOnly:           dependency.DevelopmentOnly,
			TestDependency:            dependency.TestDependency,
			VersionEvidence:           dependency.VersionEvidence,
			UnresolvedMSBuildProperty: dependency.UnresolvedMSBuildProperty,
			AmbiguousMSBuildProperty:  dependency.AmbiguousMSBuildProperty,
			PackageAPIPackages:        append([]string(nil), dependency.PackageAPIPackages...),
			PackageAPIIdentitySource:  dependency.PackageAPIIdentitySource,
			DependencyResolutionState: dependency.DependencyResolutionState,
			SourceSet:                 dependency.SourceSet,
			GeneratedCode:             payloadcore.CloneBoolPointer(dependency.GeneratedCode),
			PartialEvidence:           dependency.PartialEvidence,
			DependencyPath:            dependency.DependencyPath,
			DependencyDepth:           dependency.DependencyDepth,
			DirectDependency:          dependency.DirectDependency,
			Lockfile:                  dependency.Lockfile,
			Outcome:                   PackageConsumptionManifestDeclared,
			Reason:                    "git manifest dependency matches package registry identity",
			ProvenanceOnly:            false,
			CanonicalWrites:           1,
			EvidenceFactIDs:           []string{dependency.FactID},
		})
	}
	sort.SliceStable(decisions, func(i, j int) bool {
		if decisions[i].PackageID != decisions[j].PackageID {
			return decisions[i].PackageID < decisions[j].PackageID
		}
		if decisions[i].RepositoryID != decisions[j].RepositoryID {
			return decisions[i].RepositoryID < decisions[j].RepositoryID
		}
		return decisions[i].RelativePath < decisions[j].RelativePath
	})
	return decisions
}

// ExtractPackageRegistryIdentities reduces every package-registry package
// fact in envelopes to a PackageRegistryIdentity. Exported because the
// code-import family resolves owners through the same identities the
// consumption builder uses.
func ExtractPackageRegistryIdentities(envelopes []facts.Envelope) []PackageRegistryIdentity {
	seen := make(map[string]struct{})
	out := make([]PackageRegistryIdentity, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.PackageRegistryPackageFactKind || envelope.IsTombstone {
			continue
		}
		packageID := payloadcore.PayloadStr(envelope.Payload, "package_id")
		ecosystem := strings.ToLower(payloadcore.PayloadStr(envelope.Payload, "ecosystem"))
		if packageID == "" || ecosystem == "" {
			continue
		}
		identity := PackageRegistryIdentity{
			PackageID: packageID,
			Ecosystem: ecosystem,
			Names: packageRegistryIdentityNames(
				ecosystem,
				payloadcore.PayloadStr(envelope.Payload, "raw_name"),
				payloadcore.PayloadStr(envelope.Payload, "normalized_name"),
				payloadcore.PayloadStr(envelope.Payload, "namespace"),
			),
		}
		if len(identity.Names) == 0 {
			continue
		}
		key := identity.PackageID + "\x00" + identity.Ecosystem
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, identity)
	}
	return out
}

func packageRegistryIdentityNames(ecosystem, rawName, normalizedName, namespace string) []string {
	candidates := []string{rawName, normalizedName}
	namespace = strings.TrimSpace(namespace)
	normalizedName = strings.TrimSpace(normalizedName)
	if namespace != "" && normalizedName != "" {
		candidates = append(candidates, strings.TrimRight(namespace, "/")+"/"+strings.TrimLeft(normalizedName, "/"))
		if packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(ecosystem)) == packageidentity.EcosystemMaven {
			candidates = append(candidates, strings.TrimRight(namespace, ":")+":"+strings.TrimLeft(normalizedName, ":"))
		}
	}
	return payloadcore.UniqueSortedStrings(candidates)
}

// ExtractPackageManifestDependencies reduces every manifest or lockfile
// dependency fact in envelopes to a PackageManifestDependency. Exported
// because supply-chain impact and the security-alert bridge match manifest
// evidence without going through the consumption builder.
func ExtractPackageManifestDependencies(envelopes []facts.Envelope) []PackageManifestDependency {
	repositories := packageSourceRepositoriesByID(extractPackageSourceRepositories(envelopes))
	out := make([]PackageManifestDependency, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != factload.FactKindContentEntity || envelope.IsTombstone {
			continue
		}
		if payloadcore.PayloadStr(envelope.Payload, "entity_type") != "Variable" {
			continue
		}
		if packageManifestMetadataString(envelope.Payload, "config_kind") != "dependency" {
			continue
		}
		repositoryID := payloadcore.PayloadStr(envelope.Payload, "repo_id")
		if repositoryID == "" {
			continue
		}
		packageManager := strings.ToLower(packageManifestMetadataString(envelope.Payload, "package_manager"))
		lockfile := packageManifestMetadataBoolValue(envelope.Payload, "lockfile")
		dependency := PackageManifestDependency{
			FactID:                    envelope.FactID,
			RepositoryID:              repositoryID,
			RepositoryName:            packageRepositoryName(repositoryID, repositories, envelope.Payload),
			RelativePath:              payloadcore.PayloadStr(envelope.Payload, "relative_path"),
			ObservedAt:                envelope.ObservedAt,
			DependencyName:            payloadcore.PayloadStr(envelope.Payload, "entity_name"),
			PackageNamespace:          packageManifestMetadataString(envelope.Payload, "namespace"),
			PackageManager:            packageManager,
			ManifestSection:           packageManifestMetadataString(envelope.Payload, "section"),
			DependencyRange:           packageManifestMetadataString(envelope.Payload, "value"),
			ObservedVersion:           packageManifestObservedVersion(envelope.Payload, packageManager, lockfile),
			RequestedRange:            packageManifestRequestedRange(envelope.Payload),
			InstalledVersion:          packageManifestMetadataString(envelope.Payload, "installed_version"),
			DependencyScope:           packageManifestMetadataString(envelope.Payload, "dependency_scope"),
			PrivateAssets:             packageManifestMetadataString(envelope.Payload, "private_assets"),
			IncludeAssets:             packageManifestMetadataString(envelope.Payload, "include_assets"),
			ExcludeAssets:             packageManifestMetadataString(envelope.Payload, "exclude_assets"),
			DevelopmentOnly:           packageManifestMetadataBoolValue(envelope.Payload, "development_dependency"),
			TestDependency:            packageManifestMetadataBoolValue(envelope.Payload, "test_dependency"),
			VersionEvidence:           packageManifestMetadataString(envelope.Payload, "version_evidence"),
			UnresolvedMSBuildProperty: packageManifestMetadataString(envelope.Payload, "unresolved_msbuild_property"),
			AmbiguousMSBuildProperty:  packageManifestMetadataString(envelope.Payload, "ambiguous_msbuild_property"),
			PackageAPIPackages:        packageManifestMetadataStrings(envelope.Payload, "package_api_packages"),
			PackageAPIIdentitySource:  packageManifestMetadataString(envelope.Payload, "package_api_identity_source"),
			DependencyResolutionState: packageManifestMetadataString(envelope.Payload, "dependency_resolution_state"),
			SourceSet:                 packageManifestMetadataString(envelope.Payload, "source_set"),
			GeneratedCode:             packageManifestMetadataBool(envelope.Payload, "generated_code"),
			PartialEvidence:           packageManifestMetadataBoolValue(envelope.Payload, "partial_evidence"),
			DependencyPath:            packageManifestMetadataStrings(envelope.Payload, "dependency_path"),
			DependencyDepth:           packageManifestMetadataInt(envelope.Payload, "dependency_depth"),
			DirectDependency:          packageManifestMetadataBool(envelope.Payload, "direct_dependency"),
			Lockfile:                  lockfile,
			SourceAmbiguous:           packageManifestMetadataBoolValue(envelope.Payload, "source_ambiguous"),
		}
		if dependency.DependencyName == "" || dependency.PackageManager == "" {
			continue
		}
		if packageManifestMetadataString(envelope.Payload, "lockfile_unsupported_feature") != "" {
			continue
		}
		if dependency.SourceAmbiguous {
			continue
		}
		dependency = normalizePackageManifestDependencyChain(dependency)
		out = append(out, dependency)
	}
	joinRubyGemsLockfileManifestRanges(out)
	return out
}

func packageSourceRepositoriesByID(repositories []packageSourceRepository) map[string]packageSourceRepository {
	out := make(map[string]packageSourceRepository, len(repositories))
	for _, repository := range repositories {
		if repository.RepositoryID == "" {
			continue
		}
		out[repository.RepositoryID] = repository
	}
	return out
}

func packageRepositoryName(
	repositoryID string,
	repositories map[string]packageSourceRepository,
	payload map[string]any,
) string {
	if repository, ok := repositories[repositoryID]; ok {
		return repository.RepositoryName
	}
	return payloadcore.PayloadStr(payload, "repo_name")
}

func normalizePackageManifestDependencyChain(dependency PackageManifestDependency) PackageManifestDependency {
	if len(dependency.DependencyPath) > 0 {
		if dependency.DependencyDepth == 0 {
			dependency.DependencyDepth = len(dependency.DependencyPath)
		}
		return dependency
	}
	if packageManifestDependencyNeedsProvenChain(dependency) {
		dependency.DependencyDepth = 0
		dependency.DirectDependency = nil
		return dependency
	}
	dependency.DependencyPath = []string{dependency.DependencyName}
	dependency.DependencyDepth = 1
	value := true
	dependency.DirectDependency = &value
	return dependency
}

func packageManifestDependencyNeedsProvenChain(dependency PackageManifestDependency) bool {
	if dependency.Lockfile {
		return true
	}
	if packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(dependency.PackageManager)) ==
		packageidentity.EcosystemNuGet {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(dependency.ManifestSection)) {
	case "package-lock", "gemfile.lock", "cargo-lock":
		return true
	}
	return packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(dependency.PackageManager)) ==
		packageidentity.EcosystemRubyGems
}

func packageConsumptionIdentityForDependency(
	identityByKey map[string]PackageRegistryIdentity,
	dependency PackageManifestDependency,
) (PackageRegistryIdentity, bool) {
	names := []string{dependency.DependencyName}
	if dependency.PackageNamespace != "" {
		names = append(names, strings.TrimSpace(dependency.PackageNamespace)+"/"+dependency.DependencyName)
	}
	for _, key := range PackageConsumptionKeys(dependency.PackageManager, names...) {
		identity, ok := identityByKey[key]
		if ok {
			return identity, true
		}
	}
	return PackageRegistryIdentity{}, false
}

// PackageConsumptionKeys returns the registry-identity join keys for one
// ecosystem plus package-name candidates. Exported because the code-import
// family joins owner records through the same keys the consumption builder
// uses, and supply-chain impact keys manifest evidence the same way.
func PackageConsumptionKeys(ecosystem string, packageNames ...string) []string {
	normalizedEcosystem := packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(ecosystem))
	if normalizedEcosystem == "" {
		return nil
	}
	keys := make([]string, 0, len(packageNames))
	for _, packageName := range packageNames {
		for _, candidate := range PackageConsumptionNameCandidates(normalizedEcosystem, packageName) {
			keys = append(keys, string(normalizedEcosystem)+"\x00"+candidate)
		}
	}
	return payloadcore.UniqueSortedStrings(keys)
}

// PackageConsumptionNameCandidates returns the normalized plus lower-cased
// name forms one manifest dependency name joins on for an ecosystem.
// Exported because supply-chain impact expands filter names through the same
// candidates the consumption join uses.
func PackageConsumptionNameCandidates(
	ecosystem packageidentity.Ecosystem,
	packageName string,
) []string {
	packageName = strings.TrimSpace(packageName)
	if packageName == "" {
		return nil
	}
	normalizedName, ok := packageConsumptionNormalizedName(ecosystem, packageName)
	candidates := make([]string, 0, 2)
	if ok {
		candidates = append(candidates, normalizedName)
	}
	candidates = append(candidates, strings.ToLower(packageName))
	return payloadcore.UniqueSortedStrings(candidates)
}

func packageConsumptionNormalizedName(
	ecosystem packageidentity.Ecosystem,
	packageName string,
) (string, bool) {
	rawName, namespace := packageConsumptionRawNameAndNamespace(ecosystem, packageName)
	identity, err := packageidentity.Normalize(packageidentity.RawIdentity{
		Ecosystem:      ecosystem,
		Registry:       "manifest.local",
		RawName:        rawName,
		Namespace:      namespace,
		PackageManager: string(ecosystem),
	})
	if err != nil {
		return "", false
	}
	if namespace != "" {
		return strings.TrimRight(namespace, "/") + "/" + strings.TrimLeft(identity.NormalizedName, "/"), true
	}
	return identity.NormalizedName, true
}

func packageConsumptionRawNameAndNamespace(
	ecosystem packageidentity.Ecosystem,
	packageName string,
) (string, string) {
	packageName = strings.TrimSpace(packageName)
	if ecosystem != packageidentity.EcosystemMaven && ecosystem != packageidentity.EcosystemHex {
		return packageName, ""
	}
	namespace, name, ok := strings.Cut(packageName, ":")
	if !ok {
		namespace, name, ok = strings.Cut(packageName, "/")
	}
	if !ok {
		return packageName, ""
	}
	return strings.TrimSpace(name), strings.TrimSpace(namespace)
}

func packageManifestDependencyFilter(envelopes []facts.Envelope) PackageManifestDependencyFactFilter {
	identities := ExtractPackageRegistryIdentities(envelopes)
	ecosystems := make([]string, 0)
	names := make([]string, 0)
	packageIDs := make([]string, 0, len(identities))
	for _, identity := range identities {
		ecosystems = append(ecosystems, identity.Ecosystem)
		packageIDs = append(packageIDs, identity.PackageID)
		names = append(names, identity.Names...)
	}
	return PackageManifestDependencyFactFilter{
		Ecosystems:   payloadcore.UniqueSortedStrings(ecosystems),
		PackageNames: payloadcore.UniqueSortedStrings(names),
		PackageIDs:   payloadcore.UniqueSortedStrings(packageIDs),
	}
}

func countCanonicalWrites(decisions []PackageConsumptionDecision) int {
	total := 0
	for _, decision := range decisions {
		total += decision.CanonicalWrites
	}
	return total
}
