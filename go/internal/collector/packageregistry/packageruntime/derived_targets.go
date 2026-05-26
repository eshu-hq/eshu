package packageruntime

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry"
)

func normalizeDerivedTargetConfig(config DerivedTargetConfig) DerivedTargetConfig {
	if !config.Enabled {
		return DerivedTargetConfig{}
	}
	ecosystems := make([]packageregistry.Ecosystem, 0, len(config.Ecosystems))
	seen := map[packageregistry.Ecosystem]struct{}{}
	source := config.Ecosystems
	if len(source) == 0 {
		source = []packageregistry.Ecosystem{packageregistry.EcosystemNPM}
	}
	for _, ecosystem := range source {
		ecosystem = packageregistry.Ecosystem(strings.TrimSpace(string(ecosystem)))
		if ecosystem != packageregistry.EcosystemNPM {
			continue
		}
		if _, ok := seen[ecosystem]; ok {
			continue
		}
		seen[ecosystem] = struct{}{}
		ecosystems = append(ecosystems, ecosystem)
	}
	packageLimit := config.PackageLimit
	if packageLimit <= 0 {
		packageLimit = 1
	}
	versionLimit := config.VersionLimit
	if versionLimit <= 0 {
		versionLimit = 1
	}
	return DerivedTargetConfig{
		Enabled:      len(ecosystems) > 0,
		Ecosystems:   ecosystems,
		PackageLimit: packageLimit,
		VersionLimit: versionLimit,
	}
}

func (s *ClaimedSource) derivedTargetForScope(scopeID string) (TargetConfig, error) {
	if !s.derivedTargets.Enabled {
		return TargetConfig{}, fmt.Errorf("package registry target scope_id %q is not configured", scopeID)
	}
	target, ok := derivedNPMTarget(scopeID, s.derivedTargets)
	if !ok {
		return TargetConfig{}, fmt.Errorf("package registry target scope_id %q is not configured", scopeID)
	}
	return target, nil
}

func derivedNPMTarget(scopeID string, config DerivedTargetConfig) (TargetConfig, bool) {
	if !derivedTargetEcosystemEnabled(config, packageregistry.EcosystemNPM) {
		return TargetConfig{}, false
	}
	parsed, err := url.Parse(scopeID)
	if err != nil || parsed.Scheme != string(packageregistry.EcosystemNPM) || parsed.Host != "registry.npmjs.org" {
		return TargetConfig{}, false
	}
	packageName, err := url.PathUnescape(strings.TrimPrefix(parsed.EscapedPath(), "/"))
	if err != nil || strings.TrimSpace(packageName) == "" {
		return TargetConfig{}, false
	}
	identity, err := packageregistry.NormalizePackageIdentity(packageregistry.PackageIdentity{
		Ecosystem: packageregistry.EcosystemNPM,
		Registry:  "https://registry.npmjs.org",
		RawName:   packageName,
	})
	if err != nil || identity.PackageID != scopeID {
		return TargetConfig{}, false
	}
	metadataURL := "https://registry.npmjs.org/" + url.PathEscape(identity.NormalizedName)
	return TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:     "npm",
			Ecosystem:    packageregistry.EcosystemNPM,
			Registry:     "https://registry.npmjs.org",
			ScopeID:      identity.PackageID,
			Namespace:    identity.Namespace,
			Packages:     []string{identity.NormalizedName},
			PackageLimit: config.PackageLimit,
			VersionLimit: config.VersionLimit,
			Visibility:   packageregistry.VisibilityUnknown,
			SourceURI:    metadataURL,
		},
		MetadataURL: metadataURL,
	}, true
}

func derivedTargetEcosystemEnabled(config DerivedTargetConfig, ecosystem packageregistry.Ecosystem) bool {
	for _, candidate := range config.Ecosystems {
		if candidate == ecosystem {
			return true
		}
	}
	return false
}
