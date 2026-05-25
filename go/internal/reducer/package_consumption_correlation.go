package reducer

import (
	"sort"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
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
	PackageID        string
	Ecosystem        string
	PackageName      string
	RepositoryID     string
	RepositoryName   string
	RelativePath     string
	ManifestSection  string
	DependencyRange  string
	DependencyPath   []string
	DependencyDepth  int
	DirectDependency *bool
	Outcome          PackageConsumptionOutcome
	Reason           string
	ProvenanceOnly   bool
	CanonicalWrites  int
	EvidenceFactIDs  []string
}

// PackageManifestDependencyFactFilter bounds the active Git dependency facts
// loaded for one package-registry reducer intent.
type PackageManifestDependencyFactFilter struct {
	Ecosystems    []string
	PackageNames  []string
	PackageIDs    []string
	SourceScopeID string
}

type packageRegistryIdentity struct {
	PackageID string
	Ecosystem string
	Names     []string
}

type packageManifestDependency struct {
	FactID           string
	RepositoryID     string
	RepositoryName   string
	RelativePath     string
	DependencyName   string
	PackageManager   string
	ManifestSection  string
	DependencyRange  string
	DependencyPath   []string
	DependencyDepth  int
	DirectDependency *bool
	Lockfile         bool
	SourceAmbiguous  bool
}

// BuildPackageConsumptionDecisions matches package registry identities to Git
// manifest dependency facts. It only admits source-declared consumption; package
// name similarity outside manifest evidence is ignored.
func BuildPackageConsumptionDecisions(envelopes []facts.Envelope) []PackageConsumptionDecision {
	identities := extractPackageRegistryIdentities(envelopes)
	dependencies := extractPackageManifestDependencies(envelopes)
	identityByKey := make(map[string]packageRegistryIdentity)
	for _, identity := range identities {
		for _, name := range identity.Names {
			for _, key := range packageConsumptionKeys(identity.Ecosystem, name) {
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
			PackageID:        identity.PackageID,
			Ecosystem:        identity.Ecosystem,
			PackageName:      dependency.DependencyName,
			RepositoryID:     dependency.RepositoryID,
			RepositoryName:   dependency.RepositoryName,
			RelativePath:     dependency.RelativePath,
			ManifestSection:  dependency.ManifestSection,
			DependencyRange:  dependency.DependencyRange,
			DependencyPath:   dependency.DependencyPath,
			DependencyDepth:  dependency.DependencyDepth,
			DirectDependency: dependency.DirectDependency,
			Outcome:          PackageConsumptionManifestDeclared,
			Reason:           "git manifest dependency matches package registry identity",
			ProvenanceOnly:   false,
			CanonicalWrites:  1,
			EvidenceFactIDs:  []string{dependency.FactID},
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

func extractPackageRegistryIdentities(envelopes []facts.Envelope) []packageRegistryIdentity {
	seen := make(map[string]struct{})
	out := make([]packageRegistryIdentity, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.PackageRegistryPackageFactKind || envelope.IsTombstone {
			continue
		}
		packageID := payloadStr(envelope.Payload, "package_id")
		ecosystem := strings.ToLower(payloadStr(envelope.Payload, "ecosystem"))
		if packageID == "" || ecosystem == "" {
			continue
		}
		identity := packageRegistryIdentity{
			PackageID: packageID,
			Ecosystem: ecosystem,
			Names: packageRegistryIdentityNames(
				payloadStr(envelope.Payload, "raw_name"),
				payloadStr(envelope.Payload, "normalized_name"),
				payloadStr(envelope.Payload, "namespace"),
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

func packageRegistryIdentityNames(rawName, normalizedName, namespace string) []string {
	candidates := []string{rawName, normalizedName}
	namespace = strings.TrimSpace(namespace)
	normalizedName = strings.TrimSpace(normalizedName)
	if namespace != "" && normalizedName != "" {
		candidates = append(candidates, strings.TrimRight(namespace, "/")+"/"+strings.TrimLeft(normalizedName, "/"))
	}
	return uniqueSortedStrings(candidates)
}

func extractPackageManifestDependencies(envelopes []facts.Envelope) []packageManifestDependency {
	repositories := packageSourceRepositoriesByID(extractPackageSourceRepositories(envelopes))
	out := make([]packageManifestDependency, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != factKindContentEntity || envelope.IsTombstone {
			continue
		}
		if payloadStr(envelope.Payload, "entity_type") != "Variable" {
			continue
		}
		if packageManifestMetadataString(envelope.Payload, "config_kind") != "dependency" {
			continue
		}
		repositoryID := payloadStr(envelope.Payload, "repo_id")
		if repositoryID == "" {
			continue
		}
		dependency := packageManifestDependency{
			FactID:           envelope.FactID,
			RepositoryID:     repositoryID,
			RepositoryName:   packageRepositoryName(repositoryID, repositories, envelope.Payload),
			RelativePath:     payloadStr(envelope.Payload, "relative_path"),
			DependencyName:   payloadStr(envelope.Payload, "entity_name"),
			PackageManager:   strings.ToLower(packageManifestMetadataString(envelope.Payload, "package_manager")),
			ManifestSection:  packageManifestMetadataString(envelope.Payload, "section"),
			DependencyRange:  packageManifestMetadataString(envelope.Payload, "value"),
			DependencyPath:   packageManifestMetadataStrings(envelope.Payload, "dependency_path"),
			DependencyDepth:  packageManifestMetadataInt(envelope.Payload, "dependency_depth"),
			DirectDependency: packageManifestMetadataBool(envelope.Payload, "direct_dependency"),
			Lockfile:         packageManifestMetadataBoolValue(envelope.Payload, "lockfile"),
			SourceAmbiguous:  packageManifestMetadataBoolValue(envelope.Payload, "source_ambiguous"),
		}
		if dependency.DependencyName == "" || dependency.PackageManager == "" {
			continue
		}
		if dependency.SourceAmbiguous {
			continue
		}
		dependency = normalizePackageManifestDependencyChain(dependency)
		out = append(out, dependency)
	}
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
	return payloadStr(payload, "repo_name")
}

func packageManifestMetadataString(payload map[string]any, key string) string {
	if value := payloadStr(payload, key); value != "" {
		return value
	}
	raw, ok := payload["entity_metadata"].(map[string]any)
	if !ok {
		return ""
	}
	return payloadStr(raw, key)
}

func packageManifestMetadataStrings(payload map[string]any, key string) []string {
	values := payloadOrderedStrings(payload, key)
	if len(values) > 0 {
		return values
	}
	raw, ok := payload["entity_metadata"].(map[string]any)
	if !ok {
		return nil
	}
	return payloadOrderedStrings(raw, key)
}

func payloadOrderedStrings(payload map[string]any, key string) []string {
	raw, ok := payload[key]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			if value = strings.TrimSpace(value); value != "" {
				out = append(out, value)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			text := strings.TrimSpace(payloadString(map[string]any{"value": value}, "value"))
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			return []string{trimmed}
		}
	}
	return nil
}

func packageManifestMetadataInt(payload map[string]any, key string) int {
	if value := packageManifestMetadataString(payload, key); value != "" {
		parsed, _ := strconv.Atoi(value)
		return parsed
	}
	return 0
}

func packageManifestMetadataBool(payload map[string]any, key string) *bool {
	raw := packageManifestMetadataString(payload, key)
	if raw == "" {
		return nil
	}
	value := strings.EqualFold(raw, "true")
	return &value
}

// packageManifestMetadataBoolValue returns the metadata boolean for `key`
// as a plain bool, defaulting to false when the field is missing. It is
// used for flags whose absence and false value carry the same meaning,
// such as `lockfile` and `source_ambiguous`, where only the true case changes
// downstream behavior.
func packageManifestMetadataBoolValue(payload map[string]any, key string) bool {
	value := packageManifestMetadataBool(payload, key)
	return value != nil && *value
}

func normalizePackageManifestDependencyChain(dependency packageManifestDependency) packageManifestDependency {
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

func packageManifestDependencyNeedsProvenChain(dependency packageManifestDependency) bool {
	if dependency.Lockfile {
		return true
	}
	if strings.EqualFold(dependency.ManifestSection, "package-lock") ||
		strings.EqualFold(dependency.ManifestSection, "gemfile.lock") {
		return true
	}
	return packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(dependency.PackageManager)) ==
		packageidentity.EcosystemRubyGems
}

func packageConsumptionIdentityForDependency(
	identityByKey map[string]packageRegistryIdentity,
	dependency packageManifestDependency,
) (packageRegistryIdentity, bool) {
	for _, key := range packageConsumptionKeys(dependency.PackageManager, dependency.DependencyName) {
		identity, ok := identityByKey[key]
		if ok {
			return identity, true
		}
	}
	return packageRegistryIdentity{}, false
}

func packageConsumptionKeys(ecosystem string, packageNames ...string) []string {
	normalizedEcosystem := packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(ecosystem))
	if normalizedEcosystem == "" {
		return nil
	}
	keys := make([]string, 0, len(packageNames))
	for _, packageName := range packageNames {
		for _, candidate := range packageConsumptionNameCandidates(normalizedEcosystem, packageName) {
			keys = append(keys, string(normalizedEcosystem)+"\x00"+candidate)
		}
	}
	return uniqueSortedStrings(keys)
}

func packageConsumptionNameCandidates(
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
	return uniqueSortedStrings(candidates)
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
	if ecosystem != packageidentity.EcosystemMaven {
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
	identities := extractPackageRegistryIdentities(envelopes)
	ecosystems := make([]string, 0)
	names := make([]string, 0)
	packageIDs := make([]string, 0, len(identities))
	for _, identity := range identities {
		ecosystems = append(ecosystems, identity.Ecosystem)
		packageIDs = append(packageIDs, identity.PackageID)
		names = append(names, identity.Names...)
	}
	return PackageManifestDependencyFactFilter{
		Ecosystems:   uniqueSortedStrings(ecosystems),
		PackageNames: uniqueSortedStrings(names),
		PackageIDs:   uniqueSortedStrings(packageIDs),
	}
}

func packageCorrelationCanonicalWrites(decisions []PackageConsumptionDecision) int {
	total := 0
	for _, decision := range decisions {
		total += decision.CanonicalWrites
	}
	return total
}
