// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/platformfam"
)

// This file is the transitional compatibility surface for the platform family
// that moved to [platformfam] (issue #6061). Reducer-root call sites and the
// external packages that name these types -- cmd/reducer, internal/query and
// internal/storage/postgres -- keep their current spelling; each entry is
// deleted once its last caller has moved out of the reducer root.

// PlatformMaterializationWrite captures the bounded canonical reconciliation
// request for one platform materialization reducer intent. See
// [platformfam.PlatformMaterializationWrite].
type PlatformMaterializationWrite = platformfam.PlatformMaterializationWrite

// PlatformMaterializationWriteResult captures the canonical platform
// materialization write outcome returned by the backend adapter. See
// [platformfam.PlatformMaterializationWriteResult].
type PlatformMaterializationWriteResult = platformfam.PlatformMaterializationWriteResult

// PlatformMaterializationWriter persists one platform materialization request
// into a canonical reducer-owned target. See
// [platformfam.PlatformMaterializationWriter].
type PlatformMaterializationWriter = platformfam.PlatformMaterializationWriter

// PlatformGraphLocker coordinates writes that can touch the same Platform.id.
// See [platformfam.PlatformGraphLocker].
type PlatformGraphLocker = platformfam.PlatformGraphLocker

// WorkloadMaterializationReplayer requeues workload materialization after
// stronger deployment evidence becomes available for the same scope
// generation. See [platformfam.WorkloadMaterializationReplayer].
type WorkloadMaterializationReplayer = platformfam.WorkloadMaterializationReplayer

// CrossRepoRelationshipResolver is the cross-repo resolution seam the platform
// materialization handler depends on. [CrossRepoRelationshipHandler] is the
// production implementation. See [platformfam.CrossRepoRelationshipResolver].
type CrossRepoRelationshipResolver = platformfam.CrossRepoRelationshipResolver

// PlatformMaterializationHandler reduces one platform materialization intent
// into a bounded canonical write request. See
// [platformfam.PlatformMaterializationHandler].
type PlatformMaterializationHandler = platformfam.PlatformMaterializationHandler

// PostgresPlatformMaterializationWriter persists one platform-materialization
// reducer reconciliation into the shared fact store. See
// [platformfam.PostgresPlatformMaterializationWriter].
type PostgresPlatformMaterializationWriter = platformfam.PostgresPlatformMaterializationWriter

// TerraformRuntimeFamily describes one Terraform-managed runtime family. See
// [platformfam.TerraformRuntimeFamily].
type TerraformRuntimeFamily = platformfam.TerraformRuntimeFamily

// RuntimeFamilies forwards to [platformfam.RuntimeFamilies].
func RuntimeFamilies() []TerraformRuntimeFamily {
	return platformfam.RuntimeFamilies()
}

// LookupRuntimeFamily forwards to [platformfam.LookupRuntimeFamily].
func LookupRuntimeFamily(kind string) *TerraformRuntimeFamily {
	return platformfam.LookupRuntimeFamily(kind)
}

// InferTerraformRuntimeFamilyKind forwards to
// [platformfam.InferTerraformRuntimeFamilyKind].
func InferTerraformRuntimeFamilyKind(content string) string {
	return platformfam.InferTerraformRuntimeFamilyKind(content)
}

// InferRuntimeFamilyKindFromIdentifiers forwards to
// [platformfam.InferRuntimeFamilyKindFromIdentifiers].
func InferRuntimeFamilyKindFromIdentifiers(values []string) string {
	return platformfam.InferRuntimeFamilyKindFromIdentifiers(values)
}

// InferInfrastructureRuntimeFamilyKind forwards to
// [platformfam.InferInfrastructureRuntimeFamilyKind].
func InferInfrastructureRuntimeFamilyKind(resourceTypes, moduleSources []string) string {
	return platformfam.InferInfrastructureRuntimeFamilyKind(resourceTypes, moduleSources)
}

// MatchesServiceModuleSource forwards to
// [platformfam.MatchesServiceModuleSource].
func MatchesServiceModuleSource(source, kind string) bool {
	return platformfam.MatchesServiceModuleSource(source, kind)
}

// TerraformPlatformEvidenceKind forwards to
// [platformfam.TerraformPlatformEvidenceKind].
func TerraformPlatformEvidenceKind(kind, scope string) string {
	return platformfam.TerraformPlatformEvidenceKind(kind, scope)
}

// FormatPlatformKindLabel forwards to [platformfam.FormatPlatformKindLabel].
func FormatPlatformKindLabel(kind string) string {
	return platformfam.FormatPlatformKindLabel(kind)
}
