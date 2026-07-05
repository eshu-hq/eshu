// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	packageregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/packageregistry/v1"
)

// This file holds the projector-side decode wrappers for the package_registry
// fact family. Each wraps the contracts-module Decode* seam and, on a
// classified *factschema.DecodeError (a missing/null required identity
// field), returns a *projectorDecodeError so partitionProjectorDecodeFailures
// can quarantine the fact per-fact rather than the extractor computing a
// graph identity from an empty-string segment. Only the CONSUMED
// package_registry kinds get a wrapper here: package, package_version, and
// package_dependency. The six typed-but-not-yet-consumed kinds (source_hint,
// package_artifact, vulnerability_hint, registry_event, repository_hosting,
// warning) have no projector read site and therefore no wrapper here —
// decode_packageregistry.go's Decode* seam exists for them already (and, for
// source_hint, a separate reducer domain reads its payload through raw map
// access, not through this seam), but wiring a projector wrapper with no
// caller would be dead code.

// decodePackageRegistryPackage decodes one package_registry.package envelope
// into the typed struct through the contracts seam. A missing required field
// (package_id) yields a self-classifying *projectorDecodeError.
func decodePackageRegistryPackage(env facts.Envelope) (packageregistryv1.Package, error) {
	pkg, err := factschema.DecodePackageRegistryPackage(factschemaEnvelope(env))
	if err != nil {
		return packageregistryv1.Package{}, newProjectorDecodeError(factschema.FactKindPackageRegistryPackage, err)
	}
	return pkg, nil
}

// decodePackageRegistryPackageVersion decodes one
// package_registry.package_version envelope into the typed struct. A missing
// required field (package_id, version_id, version) yields a self-classifying
// *projectorDecodeError.
func decodePackageRegistryPackageVersion(env facts.Envelope) (packageregistryv1.PackageVersion, error) {
	version, err := factschema.DecodePackageRegistryPackageVersion(factschemaEnvelope(env))
	if err != nil {
		return packageregistryv1.PackageVersion{}, newProjectorDecodeError(factschema.FactKindPackageRegistryPackageVersion, err)
	}
	return version, nil
}

// decodePackageRegistryPackageDependency decodes one
// package_registry.package_dependency envelope into the typed struct. A
// missing required join key (package_id, version_id,
// dependency_package_id) yields a self-classifying *projectorDecodeError.
func decodePackageRegistryPackageDependency(env facts.Envelope) (packageregistryv1.PackageDependency, error) {
	dependency, err := factschema.DecodePackageRegistryPackageDependency(factschemaEnvelope(env))
	if err != nil {
		return packageregistryv1.PackageDependency{}, newProjectorDecodeError(factschema.FactKindPackageRegistryPackageDependency, err)
	}
	return dependency, nil
}

// packageRegistryDerefString returns the value a *string points at, or "" when
// it is nil. The typed package_registry structs carry optional fields as
// *string so an absent key stays distinct from an observed empty value; the
// row builders substitute "" for an unobserved field, matching the pre-typing
// payloadString("") behavior.
func packageRegistryDerefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// packageRegistryDerefBool returns the value a *bool points at, or false when
// it is nil. The typed package_registry structs carry the descriptive status
// flags (is_yanked/is_unlisted/is_deprecated/is_retracted on a version,
// optional/excluded on a dependency) as *bool so an absent key on a persisted
// or older fact stays distinct from an observed false and still decodes; the
// row builders substitute false for an unobserved flag, matching the pre-typing
// payloadBoolPtr default (a nil pointer meant false) byte-for-byte.
func packageRegistryDerefBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}
