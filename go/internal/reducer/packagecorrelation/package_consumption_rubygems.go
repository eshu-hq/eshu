// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packagecorrelation

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

func joinRubyGemsLockfileManifestRanges(dependencies []PackageManifestDependency) {
	manifestByKey := make(map[string]PackageManifestDependency)
	for _, dependency := range dependencies {
		if dependency.Lockfile || dependency.SourceAmbiguous {
			continue
		}
		if packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(dependency.PackageManager)) != packageidentity.EcosystemRubyGems {
			continue
		}
		key := rubyGemsManifestJoinKey(dependency)
		if key == "" {
			continue
		}
		if _, exists := manifestByKey[key]; !exists {
			manifestByKey[key] = dependency
		}
	}
	for index := range dependencies {
		dependency := &dependencies[index]
		if !dependency.Lockfile || dependency.SourceAmbiguous {
			continue
		}
		if packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(dependency.PackageManager)) != packageidentity.EcosystemRubyGems {
			continue
		}
		if dependency.InstalledVersion == "" {
			dependency.InstalledVersion = dependency.DependencyRange
		}
		manifest := manifestByKey[rubyGemsManifestJoinKey(*dependency)]
		if manifest.DependencyRange == "" {
			continue
		}
		dependency.DependencyRange = manifest.DependencyRange
		dependency.DependencyScope = payloadcore.FirstNonBlank(dependency.DependencyScope, manifest.DependencyScope)
		dependency.DevelopmentOnly = dependency.DevelopmentOnly || manifest.DevelopmentOnly
		dependency.TestDependency = dependency.TestDependency || manifest.TestDependency
	}
}

func rubyGemsManifestJoinKey(dependency PackageManifestDependency) string {
	repositoryID := strings.TrimSpace(dependency.RepositoryID)
	name := strings.ToLower(strings.TrimSpace(dependency.DependencyName))
	if repositoryID == "" || name == "" {
		return ""
	}
	return repositoryID + "\x00" + name
}
