package packageruntime

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry"
)

func boundedParsedMetadata(
	target packageregistry.TargetConfig,
	parsed packageregistry.ParsedMetadata,
) (packageregistry.ParsedMetadata, error) {
	configured, err := configuredPackageIDs(target)
	if err != nil {
		return packageregistry.ParsedMetadata{}, err
	}
	bounded, allowedPackages, err := boundPackages(target, parsed, configured)
	if err != nil {
		return packageregistry.ParsedMetadata{}, err
	}
	allowedVersions, err := boundVersions(target, parsed, allowedPackages, &bounded)
	if err != nil {
		return packageregistry.ParsedMetadata{}, err
	}
	bounded.Dependencies, err = boundPackageVersionObservations(
		parsed.Dependencies,
		func(observation packageregistry.PackageDependencyObservation) packageregistry.PackageIdentity {
			return observation.Package
		},
		func(observation packageregistry.PackageDependencyObservation) string {
			return observation.Version
		},
		allowedPackages,
		allowedVersions,
	)
	if err != nil {
		return packageregistry.ParsedMetadata{}, err
	}
	bounded.Artifacts, err = boundPackageVersionObservations(
		parsed.Artifacts,
		func(observation packageregistry.PackageArtifactObservation) packageregistry.PackageIdentity {
			return observation.Package
		},
		func(observation packageregistry.PackageArtifactObservation) string {
			return observation.Version
		},
		allowedPackages,
		allowedVersions,
	)
	if err != nil {
		return packageregistry.ParsedMetadata{}, err
	}
	bounded.SourceHints, err = boundPackageVersionObservations(
		parsed.SourceHints,
		func(observation packageregistry.SourceHintObservation) packageregistry.PackageIdentity {
			return observation.Package
		},
		func(observation packageregistry.SourceHintObservation) string {
			return observation.Version
		},
		allowedPackages,
		allowedVersions,
	)
	if err != nil {
		return packageregistry.ParsedMetadata{}, err
	}
	bounded.Warnings, err = boundWarnings(parsed.Warnings, allowedPackages, allowedVersions)
	if err != nil {
		return packageregistry.ParsedMetadata{}, err
	}
	bounded.Hosting = parsed.Hosting
	return bounded, nil
}

func configuredPackageIDs(target packageregistry.TargetConfig) (map[string]struct{}, error) {
	configured := map[string]struct{}{}
	for _, rawPackage := range target.Packages {
		normalized, err := packageregistry.NormalizePackageIdentity(packageregistry.PackageIdentity{
			Ecosystem: target.Ecosystem,
			Registry:  target.Registry,
			RawName:   rawPackage,
			Namespace: target.Namespace,
		})
		if err != nil {
			return nil, fmt.Errorf("normalize configured package %q: %w", strings.TrimSpace(rawPackage), err)
		}
		configured[normalized.PackageID] = struct{}{}
	}
	return configured, nil
}

func boundPackages(
	target packageregistry.TargetConfig,
	parsed packageregistry.ParsedMetadata,
	configured map[string]struct{},
) (packageregistry.ParsedMetadata, map[string]struct{}, error) {
	bounded := packageregistry.ParsedMetadata{}
	allowed := map[string]struct{}{}
	for _, observation := range parsed.Packages {
		packageID, err := packageID(observation.Identity)
		if err != nil {
			return packageregistry.ParsedMetadata{}, nil, err
		}
		if len(configured) > 0 {
			if _, ok := configured[packageID]; !ok {
				continue
			}
		}
		bounded.Packages = append(bounded.Packages, observation)
		allowed[packageID] = struct{}{}
	}
	if len(parsed.Packages) > 0 && len(bounded.Packages) == 0 {
		return packageregistry.ParsedMetadata{}, nil,
			fmt.Errorf("package registry metadata does not match configured packages")
	}
	if len(bounded.Packages) > target.PackageLimit {
		return packageregistry.ParsedMetadata{}, nil,
			fmt.Errorf("package registry metadata emits %d packages, exceeds package_limit %d",
				len(bounded.Packages), target.PackageLimit)
	}
	return bounded, allowed, nil
}

func boundVersions(
	target packageregistry.TargetConfig,
	parsed packageregistry.ParsedMetadata,
	allowedPackages map[string]struct{},
	bounded *packageregistry.ParsedMetadata,
) (map[string]struct{}, error) {
	allowedVersions := map[string]struct{}{}
	versionCounts := map[string]int{}
	for _, observation := range parsed.Versions {
		packageID, err := packageID(observation.Package)
		if err != nil {
			return nil, err
		}
		if !containsID(allowedPackages, packageID) {
			continue
		}
		versionCounts[packageID]++
		if versionCounts[packageID] > target.VersionLimit {
			return nil, fmt.Errorf("package registry metadata emits more than version_limit %d for %s",
				target.VersionLimit, packageID)
		}
		bounded.Versions = append(bounded.Versions, observation)
		allowedVersions[versionKey(packageID, observation.Version)] = struct{}{}
	}
	return allowedVersions, nil
}

func boundPackageVersionObservations[T any](
	observations []T,
	identity func(T) packageregistry.PackageIdentity,
	version func(T) string,
	allowedPackages map[string]struct{},
	allowedVersions map[string]struct{},
) ([]T, error) {
	bounded := make([]T, 0, len(observations))
	for _, observation := range observations {
		if keep, err := keepPackageVersionObservation(
			identity(observation),
			version(observation),
			allowedPackages,
			allowedVersions,
		); err != nil {
			return nil, err
		} else if keep {
			bounded = append(bounded, observation)
		}
	}
	return bounded, nil
}

func boundWarnings(
	warnings []packageregistry.WarningObservation,
	allowedPackages map[string]struct{},
	allowedVersions map[string]struct{},
) ([]packageregistry.WarningObservation, error) {
	bounded := make([]packageregistry.WarningObservation, 0, len(warnings))
	for _, warning := range warnings {
		if warning.Package == nil {
			bounded = append(bounded, warning)
			continue
		}
		keep, err := keepPackageVersionObservation(*warning.Package, warning.Version, allowedPackages, allowedVersions)
		if err != nil {
			return nil, err
		}
		if keep {
			bounded = append(bounded, warning)
		}
	}
	return bounded, nil
}

func keepPackageVersionObservation(
	identity packageregistry.PackageIdentity,
	version string,
	allowedPackages map[string]struct{},
	allowedVersions map[string]struct{},
) (bool, error) {
	packageID, err := packageID(identity)
	if err != nil {
		return false, err
	}
	if !containsID(allowedPackages, packageID) {
		return false, nil
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return true, nil
	}
	if len(allowedVersions) == 0 {
		return false, nil
	}
	return containsID(allowedVersions, versionKey(packageID, version)), nil
}

func packageID(identity packageregistry.PackageIdentity) (string, error) {
	normalized, err := packageregistry.NormalizePackageIdentity(identity)
	if err != nil {
		return "", err
	}
	return normalized.PackageID, nil
}

func containsID(values map[string]struct{}, value string) bool {
	_, ok := values[value]
	return ok
}

func versionKey(packageID, version string) string {
	return packageID + "\x00" + strings.TrimSpace(version)
}
