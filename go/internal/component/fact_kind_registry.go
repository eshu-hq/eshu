// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// factKindClaim is the minimal ownership record used to compare component
// manifest declarations.
type factKindClaim struct {
	componentID    string
	version        string
	kind           string
	schemaVersions []string
}

// validateComponentFactKind enforces the component-side namespace boundary.
func validateComponentFactKind(kind string) error {
	trimmed := strings.TrimSpace(kind)
	if kind != trimmed {
		return fmt.Errorf("fact kind %q must be canonical without surrounding whitespace", kind)
	}
	if facts.IsCoreFactKind(trimmed) {
		return fmt.Errorf("fact kind %q is core-owned by Eshu and cannot be claimed by optional components", trimmed)
	}
	if !strings.Contains(trimmed, ".") {
		return fmt.Errorf("fact kind %q must be namespaced with a collision-resistant prefix", trimmed)
	}
	return nil
}

// validateInstallFactKindClaims compares a candidate manifest against durable
// local ownership state before the registry writes the install.
func (r Registry) validateInstallFactKindClaims(candidate Manifest, state registryState) error {
	candidateClaims := factKindClaims(candidate)
	for _, installed := range state.Components {
		installedManifest, err := r.installedManifest(installed)
		if err != nil {
			return err
		}
		if err := validateClaimSet(candidateClaims, factKindClaims(installedManifest)); err != nil {
			return err
		}
	}
	return nil
}

// validateEnableFactKindClaims protects old registries that predate install
// collision checks from enabling an unsafe component.
func (r Registry) validateEnableFactKindClaims(component InstalledComponent, state registryState) error {
	candidateManifest, err := r.installedManifest(component)
	if err != nil {
		return err
	}
	candidateClaims := factKindClaims(candidateManifest)
	for _, installed := range state.Components {
		if installed.ID == component.ID && installed.Version == component.Version {
			continue
		}
		installedManifest, err := r.installedManifest(installed)
		if err != nil {
			return err
		}
		if err := validateClaimSet(candidateClaims, factKindClaims(installedManifest)); err != nil {
			return err
		}
	}
	return nil
}

// installedManifest loads the registry-owned manifest path instead of trusting
// the persisted ManifestPath field.
func (r Registry) installedManifest(component InstalledComponent) (Manifest, error) {
	manifest, err := LoadManifest(r.manifestPath(component.ID, component.Version))
	if err != nil {
		return Manifest{}, WrapError(
			ErrorCodeCorruptedRegistryState,
			fmt.Sprintf("cannot validate fact-kind ownership for installed component %q version %q", component.ID, component.Version),
			err,
		)
	}
	return manifest, nil
}

// factKindClaims extracts only the ownership fields needed for collision
// checks.
func factKindClaims(manifest Manifest) []factKindClaim {
	claims := make([]factKindClaim, 0, len(manifest.Spec.EmittedFacts))
	for _, family := range manifest.Spec.EmittedFacts {
		claims = append(claims, factKindClaim{
			componentID:    manifest.Metadata.ID,
			version:        manifest.Metadata.Version,
			kind:           family.Kind,
			schemaVersions: append([]string(nil), family.SchemaVersions...),
		})
	}
	return claims
}

// validateClaimSet rejects an overlapping fact kind unless shared ownership is
// explicit and schema-compatible.
func validateClaimSet(candidates, existing []factKindClaim) error {
	for _, candidate := range candidates {
		for _, installed := range existing {
			if candidate.kind != installed.kind {
				continue
			}
			if compatibleSharedFactKindOwner(candidate, installed) {
				continue
			}
			return factKindCollisionError(candidate, installed)
		}
	}
	return nil
}

// compatibleSharedFactKindOwner allows only same-component version lineage to
// share a fact kind.
func compatibleSharedFactKindOwner(candidate, installed factKindClaim) bool {
	if candidate.componentID != installed.componentID {
		return false
	}
	return sameSchemaMajorSet(candidate.schemaVersions, installed.schemaVersions)
}

// sameSchemaMajorSet treats a major-version mismatch as incompatible fact-kind
// ownership.
func sameSchemaMajorSet(left, right []string) bool {
	leftMajors, ok := schemaMajorSet(left)
	if !ok {
		return false
	}
	rightMajors, ok := schemaMajorSet(right)
	if !ok || len(leftMajors) != len(rightMajors) {
		return false
	}
	for major := range leftMajors {
		if _, ok := rightMajors[major]; !ok {
			return false
		}
	}
	return true
}

// schemaMajorSet extracts semver major versions from a manifest declaration.
func schemaMajorSet(versions []string) (map[string]struct{}, bool) {
	majors := make(map[string]struct{}, len(versions))
	for _, version := range versions {
		normalized := normalizeSemver(version)
		if !semver.IsValid(normalized) {
			return nil, false
		}
		majors[semver.Major(normalized)] = struct{}{}
	}
	return majors, len(majors) > 0
}

// factKindCollisionError keeps collision failures deterministic and actionable.
func factKindCollisionError(candidate, installed factKindClaim) error {
	if candidate.componentID == installed.componentID {
		return Errorf(
			ErrorCodeFactKindCollision,
			"component %q version %q fact kind %q is not schema-compatible with installed version %q; use the same schema major set or uninstall the existing version first",
			candidate.componentID,
			candidate.version,
			candidate.kind,
			installed.version,
		)
	}
	return Errorf(
		ErrorCodeFactKindCollision,
		"component %q fact kind %q conflicts with installed component %q version %q; use a different namespaced fact kind, install a compatible version of the same component, or uninstall the existing owner first",
		candidate.componentID,
		candidate.kind,
		installed.componentID,
		installed.version,
	)
}
