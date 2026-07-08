// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/truth"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
)

// This file holds typed reducer-derived payload construction for package
// correlations. It is split from package_correlation_writer.go so the writer
// stays below the repo's 500-line cap while the contract mapping stays close to
// the domain-specific decisions it serializes.

func typedPackageOwnershipPayload(
	write PackageCorrelationWrite,
	decision PackageSourceCorrelationDecision,
) (map[string]any, error) {
	return factschema.EncodeReducerPackageOwnershipCorrelation(reducerderivedv1.PackageOwnershipCorrelation{
		PackageID:              decision.PackageID,
		ReducerDomain:          packageCorrelationStringPointer(string(DomainPackageSourceCorrelation)),
		IntentID:               packageCorrelationStringPointer(write.IntentID),
		ScopeID:                packageCorrelationStringPointer(write.ScopeID),
		GenerationID:           packageCorrelationStringPointer(write.GenerationID),
		SourceSystem:           packageCorrelationStringPointer(write.SourceSystem),
		Cause:                  packageCorrelationStringPointer(write.Cause),
		RelationshipKind:       packageCorrelationStringPointer("ownership"),
		VersionID:              packageCorrelationStringPointer(decision.VersionID),
		HintKind:               packageCorrelationStringPointer(decision.HintKind),
		SourceURL:              packageCorrelationStringPointer(decision.SourceURL),
		RepositoryID:           packageCorrelationStringPointer(decision.RepositoryID),
		RepositoryName:         packageCorrelationStringPointer(decision.RepositoryName),
		CandidateRepositoryIDs: uniqueSortedStrings(decision.CandidateRepositoryIDs),
		Outcome:                packageCorrelationStringPointer(string(decision.Outcome)),
		Reason:                 packageCorrelationStringPointer(decision.Reason),
		ProvenanceOnly:         packageCorrelationBoolPointer(decision.ProvenanceOnly),
		CanonicalWrites:        packageCorrelationIntPointer(decision.CanonicalWrites),
		EvidenceFactIDs:        uniqueSortedStrings(decision.EvidenceFactIDs),
		CorrelationKind:        packageCorrelationStringPointer(packageOwnershipCorrelationFactKind),
		SourceLayers:           []string{string(truth.LayerSourceDeclaration)},
	})
}

func typedPackageConsumptionPayload(
	write PackageCorrelationWrite,
	decision PackageConsumptionDecision,
) (map[string]any, error) {
	correlation := reducerderivedv1.PackageConsumptionCorrelation{
		PackageID:        decision.PackageID,
		ReducerDomain:    packageCorrelationStringPointer(string(DomainPackageSourceCorrelation)),
		IntentID:         packageCorrelationStringPointer(write.IntentID),
		ScopeID:          packageCorrelationStringPointer(write.ScopeID),
		GenerationID:     packageCorrelationStringPointer(write.GenerationID),
		SourceSystem:     packageCorrelationStringPointer(write.SourceSystem),
		Cause:            packageCorrelationStringPointer(write.Cause),
		RelationshipKind: packageCorrelationStringPointer("consumption"),
		Ecosystem:        packageCorrelationStringPointer(decision.Ecosystem),
		PackageName:      packageCorrelationStringPointer(decision.PackageName),
		RepositoryID:     packageCorrelationStringPointer(decision.RepositoryID),
		RepositoryName:   packageCorrelationStringPointer(decision.RepositoryName),
		RelativePath:     packageCorrelationStringPointer(decision.RelativePath),
		ManifestSection:  packageCorrelationStringPointer(decision.ManifestSection),
		DependencyRange:  packageCorrelationStringPointer(decision.DependencyRange),
		Outcome:          packageCorrelationStringPointer(string(decision.Outcome)),
		Reason:           packageCorrelationStringPointer(decision.Reason),
		ProvenanceOnly:   packageCorrelationBoolPointer(decision.ProvenanceOnly),
		CanonicalWrites:  packageCorrelationIntPointer(decision.CanonicalWrites),
		EvidenceFactIDs:  uniqueSortedStrings(decision.EvidenceFactIDs),
		CorrelationKind:  packageCorrelationStringPointer(packageConsumptionCorrelationFactKind),
		SourceLayers:     []string{string(truth.LayerSourceDeclaration), string(truth.LayerObservedResource)},
	}
	if strings.TrimSpace(decision.ObservedVersion) != "" {
		correlation.ObservedVersion = packageCorrelationStringPointer(strings.TrimSpace(decision.ObservedVersion))
	}
	if strings.TrimSpace(decision.RequestedRange) != "" {
		correlation.RequestedRange = packageCorrelationStringPointer(strings.TrimSpace(decision.RequestedRange))
	}
	if len(decision.DependencyPath) > 0 {
		correlation.DependencyPath = orderedStrings(decision.DependencyPath)
		correlation.DependencyDepth = packageCorrelationIntPointer(decision.DependencyDepth)
	}
	if strings.TrimSpace(decision.InstalledVersion) != "" {
		correlation.InstalledVersion = packageCorrelationStringPointer(strings.TrimSpace(decision.InstalledVersion))
	}
	if decision.DirectDependency != nil {
		correlation.DirectDependency = packageCorrelationBoolPointer(*decision.DirectDependency)
	}
	if decision.Lockfile {
		correlation.Lockfile = packageCorrelationBoolPointer(true)
	}
	if strings.TrimSpace(decision.DependencyScope) != "" {
		correlation.DependencyScope = packageCorrelationStringPointer(strings.TrimSpace(decision.DependencyScope))
	}
	if strings.TrimSpace(decision.PrivateAssets) != "" {
		correlation.PrivateAssets = packageCorrelationStringPointer(strings.TrimSpace(decision.PrivateAssets))
	}
	if strings.TrimSpace(decision.IncludeAssets) != "" {
		correlation.IncludeAssets = packageCorrelationStringPointer(strings.TrimSpace(decision.IncludeAssets))
	}
	if strings.TrimSpace(decision.ExcludeAssets) != "" {
		correlation.ExcludeAssets = packageCorrelationStringPointer(strings.TrimSpace(decision.ExcludeAssets))
	}
	if decision.DevelopmentOnly {
		correlation.DevelopmentDependency = packageCorrelationBoolPointer(true)
	}
	if decision.TestDependency {
		correlation.TestDependency = packageCorrelationBoolPointer(true)
	}
	if strings.TrimSpace(decision.VersionEvidence) != "" {
		correlation.VersionEvidence = packageCorrelationStringPointer(strings.TrimSpace(decision.VersionEvidence))
	}
	if strings.TrimSpace(decision.UnresolvedMSBuildProperty) != "" {
		correlation.UnresolvedMSBuildProperty = packageCorrelationStringPointer(strings.TrimSpace(decision.UnresolvedMSBuildProperty))
	}
	if strings.TrimSpace(decision.AmbiguousMSBuildProperty) != "" {
		correlation.AmbiguousMSBuildProperty = packageCorrelationStringPointer(strings.TrimSpace(decision.AmbiguousMSBuildProperty))
	}
	if decision.PartialEvidence {
		correlation.PartialEvidence = packageCorrelationBoolPointer(true)
	}
	return factschema.EncodeReducerPackageConsumptionCorrelation(correlation)
}

func typedPackagePublicationPayload(
	write PackageCorrelationWrite,
	decision PackagePublicationDecision,
) (map[string]any, error) {
	return factschema.EncodeReducerPackagePublicationCorrelation(reducerderivedv1.PackagePublicationCorrelation{
		PackageID:              decision.PackageID,
		ReducerDomain:          packageCorrelationStringPointer(string(DomainPackageSourceCorrelation)),
		IntentID:               packageCorrelationStringPointer(write.IntentID),
		ScopeID:                packageCorrelationStringPointer(write.ScopeID),
		GenerationID:           packageCorrelationStringPointer(write.GenerationID),
		SourceSystem:           packageCorrelationStringPointer(write.SourceSystem),
		Cause:                  packageCorrelationStringPointer(write.Cause),
		RelationshipKind:       packageCorrelationStringPointer("publication"),
		VersionID:              packageCorrelationStringPointer(decision.VersionID),
		Version:                packageCorrelationStringPointer(decision.Version),
		PublishedAt:            packageCorrelationStringPointer(decision.PublishedAt),
		SourceURL:              packageCorrelationStringPointer(decision.SourceURL),
		SourceHintFactID:       packageCorrelationStringPointer(decision.SourceHintFactID),
		SourceHintKind:         packageCorrelationStringPointer(decision.SourceHintKind),
		SourceHintVersionID:    packageCorrelationStringPointer(decision.SourceHintVersionID),
		RepositoryID:           packageCorrelationStringPointer(decision.RepositoryID),
		RepositoryName:         packageCorrelationStringPointer(decision.RepositoryName),
		CandidateRepositoryIDs: uniqueSortedStrings(decision.CandidateRepositoryIDs),
		Outcome:                packageCorrelationStringPointer(string(decision.Outcome)),
		Reason:                 packageCorrelationStringPointer(decision.Reason),
		ProvenanceOnly:         packageCorrelationBoolPointer(decision.ProvenanceOnly),
		CanonicalWrites:        packageCorrelationIntPointer(decision.CanonicalWrites),
		EvidenceFactIDs:        uniqueSortedStrings(decision.EvidenceFactIDs),
		CorrelationKind:        packageCorrelationStringPointer(packagePublicationCorrelationFactKind),
		SourceLayers:           []string{string(truth.LayerSourceDeclaration), string(truth.LayerObservedResource)},
	})
}

func packageCorrelationStringPointer(value string) *string {
	return &value
}

func packageCorrelationBoolPointer(value bool) *bool {
	return &value
}

func packageCorrelationIntPointer(value int) *int {
	return &value
}
