// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packages

import (
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// DerivationFromConfig decodes the owned-package derivation settings from one
// package-registry collector instance's configuration. The configuration is
// validated against the collector contract first, so a malformed instance
// fails here rather than silently deriving no targets.
func DerivationFromConfig(raw string) (packageRegistryDerivationConfiguration, error) {
	if err := workflow.ValidatePackageRegistryCollectorConfiguration(raw); err != nil {
		return packageRegistryDerivationConfiguration{}, err
	}
	var decoded packageRegistryRuntimeConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return packageRegistryDerivationConfiguration{}, fmt.Errorf("decode package registry derivation config: %w", err)
	}
	return decoded.DeriveFromOwnedPackages, nil
}
