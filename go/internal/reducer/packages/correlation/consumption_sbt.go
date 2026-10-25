// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import (
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// ConsumptionKeys returns the registry-identity join keys for this manifest
// dependency, in the same "ecosystem\x00name" form as PackageConsumptionKeys.
// It is the one place a dependency row becomes keys, so an sbt row joins the
// Maven identity of its published artifact (sbt_binary_artifact for `%%`, the
// exact name for `%`) and never the unsuffixed organization:name; see
// packageidentity.ManifestConsumptionKeys for the rules. names are the name
// forms the caller wants matched, such as DependencyName.
func (d PackageManifestDependency) ConsumptionKeys(names ...string) []string {
	identityKeys := packageidentity.ManifestConsumptionKeys(packageidentity.ManifestKeyInput{
		PackageManager:    d.PackageManager,
		Names:             names,
		SBTCrossVersion:   d.SBTCrossVersion,
		SBTBinaryArtifact: d.SBTBinaryArtifact,
	})
	keys := make([]string, 0, len(identityKeys))
	for _, key := range identityKeys {
		keys = append(keys, string(key.Ecosystem)+"\x00"+key.PackageName)
	}
	return payloadcore.UniqueSortedStrings(keys)
}
