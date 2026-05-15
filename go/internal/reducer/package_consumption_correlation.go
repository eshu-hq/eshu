package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
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
	PackageID       string
	Ecosystem       string
	PackageName     string
	RepositoryID    string
	RepositoryName  string
	RelativePath    string
	ManifestSection string
	DependencyRange string
	Outcome         PackageConsumptionOutcome
	Reason          string
	ProvenanceOnly  bool
	CanonicalWrites int
	EvidenceFactIDs []string
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
	FactID          string
	RepositoryID    string
	RepositoryName  string
	RelativePath    string
	DependencyName  string
	PackageManager  string
	ManifestSection string
	DependencyRange string
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
			key := packageConsumptionKey(identity.Ecosystem, name)
			if key == "" {
				continue
			}
			identityByKey[key] = identity
		}
	}

	decisions := make([]PackageConsumptionDecision, 0)
	for _, dependency := range dependencies {
		identity, ok := identityByKey[packageConsumptionKey(dependency.PackageManager, dependency.DependencyName)]
		if !ok {
			continue
		}
		decisions = append(decisions, PackageConsumptionDecision{
			PackageID:       identity.PackageID,
			Ecosystem:       identity.Ecosystem,
			PackageName:     dependency.DependencyName,
			RepositoryID:    dependency.RepositoryID,
			RepositoryName:  dependency.RepositoryName,
			RelativePath:    dependency.RelativePath,
			ManifestSection: dependency.ManifestSection,
			DependencyRange: dependency.DependencyRange,
			Outcome:         PackageConsumptionManifestDeclared,
			Reason:          "git manifest dependency matches package registry identity",
			ProvenanceOnly:  false,
			CanonicalWrites: 1,
			EvidenceFactIDs: []string{dependency.FactID},
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
			FactID:          envelope.FactID,
			RepositoryID:    repositoryID,
			RepositoryName:  packageRepositoryName(repositoryID, repositories, envelope.Payload),
			RelativePath:    payloadStr(envelope.Payload, "relative_path"),
			DependencyName:  payloadStr(envelope.Payload, "entity_name"),
			PackageManager:  strings.ToLower(packageManifestMetadataString(envelope.Payload, "package_manager")),
			ManifestSection: packageManifestMetadataString(envelope.Payload, "section"),
			DependencyRange: packageManifestMetadataString(envelope.Payload, "value"),
		}
		if dependency.DependencyName == "" || dependency.PackageManager == "" {
			continue
		}
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

func packageConsumptionKey(ecosystem, packageName string) string {
	ecosystem = strings.ToLower(strings.TrimSpace(ecosystem))
	packageName = strings.ToLower(strings.TrimSpace(packageName))
	if ecosystem == "" || packageName == "" {
		return ""
	}
	return ecosystem + "\x00" + packageName
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
