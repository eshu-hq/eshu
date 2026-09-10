// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import (
	"strings"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/truth"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
)

// This file holds typed reducer-derived payload construction for package
// correlations. It is split from writer.go so the writer
// stays below the repo's 500-line cap while the contract mapping stays close to
// the domain-specific decisions it serializes.

func typedPackageOwnershipPayload(
	write PackageWrite,
	decision PackageSourceDecision,
) (map[string]any, error) {
	return factschema.EncodeReducerPackageOwnershipCorrelation(reducerderivedv1.PackageOwnershipCorrelation{
		PackageID:              decision.PackageID,
		ReducerDomain:          payloadcore.StringPointer(string(reducercontract.DomainPackageSourceCorrelation)),
		IntentID:               payloadcore.StringPointer(write.IntentID),
		ScopeID:                payloadcore.StringPointer(write.ScopeID),
		GenerationID:           payloadcore.StringPointer(write.GenerationID),
		SourceSystem:           payloadcore.StringPointer(write.SourceSystem),
		Cause:                  payloadcore.StringPointer(write.Cause),
		RelationshipKind:       payloadcore.StringPointer("ownership"),
		VersionID:              payloadcore.StringPointer(decision.VersionID),
		HintKind:               payloadcore.StringPointer(decision.HintKind),
		SourceURL:              payloadcore.StringPointer(decision.SourceURL),
		RepositoryID:           payloadcore.StringPointer(decision.RepositoryID),
		RepositoryName:         payloadcore.StringPointer(decision.RepositoryName),
		CandidateRepositoryIDs: payloadcore.UniqueSortedStrings(decision.CandidateRepositoryIDs),
		Outcome:                payloadcore.StringPointer(string(decision.Outcome)),
		Reason:                 payloadcore.StringPointer(decision.Reason),
		ProvenanceOnly:         payloadcore.BoolPointer(decision.ProvenanceOnly),
		CanonicalWrites:        payloadcore.IntPointer(decision.CanonicalWrites),
		EvidenceFactIDs:        payloadcore.UniqueSortedStrings(decision.EvidenceFactIDs),
		CorrelationKind:        payloadcore.StringPointer(PackageOwnershipFactKind),
		SourceLayers:           []string{string(truth.LayerSourceDeclaration)},
	})
}

func typedPackageConsumptionPayload(
	write PackageWrite,
	decision PackageConsumptionDecision,
) (map[string]any, error) {
	correlation := reducerderivedv1.PackageConsumptionCorrelation{
		PackageID:        decision.PackageID,
		ReducerDomain:    payloadcore.StringPointer(string(reducercontract.DomainPackageSourceCorrelation)),
		IntentID:         payloadcore.StringPointer(write.IntentID),
		ScopeID:          payloadcore.StringPointer(write.ScopeID),
		GenerationID:     payloadcore.StringPointer(write.GenerationID),
		SourceSystem:     payloadcore.StringPointer(write.SourceSystem),
		Cause:            payloadcore.StringPointer(write.Cause),
		RelationshipKind: payloadcore.StringPointer("consumption"),
		Ecosystem:        payloadcore.StringPointer(decision.Ecosystem),
		PackageName:      payloadcore.StringPointer(decision.PackageName),
		RepositoryID:     payloadcore.StringPointer(decision.RepositoryID),
		RepositoryName:   payloadcore.StringPointer(decision.RepositoryName),
		RelativePath:     payloadcore.StringPointer(decision.RelativePath),
		ManifestSection:  payloadcore.StringPointer(decision.ManifestSection),
		DependencyRange:  payloadcore.StringPointer(decision.DependencyRange),
		Outcome:          payloadcore.StringPointer(string(decision.Outcome)),
		Reason:           payloadcore.StringPointer(decision.Reason),
		ProvenanceOnly:   payloadcore.BoolPointer(decision.ProvenanceOnly),
		CanonicalWrites:  payloadcore.IntPointer(decision.CanonicalWrites),
		EvidenceFactIDs:  payloadcore.UniqueSortedStrings(decision.EvidenceFactIDs),
		CorrelationKind:  payloadcore.StringPointer(PackageConsumptionFactKind),
		SourceLayers:     []string{string(truth.LayerSourceDeclaration), string(truth.LayerObservedResource)},
	}
	if strings.TrimSpace(decision.ObservedVersion) != "" {
		correlation.ObservedVersion = payloadcore.StringPointer(strings.TrimSpace(decision.ObservedVersion))
	}
	if strings.TrimSpace(decision.RequestedRange) != "" {
		correlation.RequestedRange = payloadcore.StringPointer(strings.TrimSpace(decision.RequestedRange))
	}
	if len(decision.DependencyPath) > 0 {
		correlation.DependencyPath = payloadcore.OrderedStrings(decision.DependencyPath)
		correlation.DependencyDepth = payloadcore.IntPointer(decision.DependencyDepth)
	}
	if strings.TrimSpace(decision.InstalledVersion) != "" {
		correlation.InstalledVersion = payloadcore.StringPointer(strings.TrimSpace(decision.InstalledVersion))
	}
	if decision.DirectDependency != nil {
		correlation.DirectDependency = payloadcore.BoolPointer(*decision.DirectDependency)
	}
	if decision.Lockfile {
		correlation.Lockfile = payloadcore.BoolPointer(true)
	}
	if strings.TrimSpace(decision.DependencyScope) != "" {
		correlation.DependencyScope = payloadcore.StringPointer(strings.TrimSpace(decision.DependencyScope))
	}
	if strings.TrimSpace(decision.PrivateAssets) != "" {
		correlation.PrivateAssets = payloadcore.StringPointer(strings.TrimSpace(decision.PrivateAssets))
	}
	if strings.TrimSpace(decision.IncludeAssets) != "" {
		correlation.IncludeAssets = payloadcore.StringPointer(strings.TrimSpace(decision.IncludeAssets))
	}
	if strings.TrimSpace(decision.ExcludeAssets) != "" {
		correlation.ExcludeAssets = payloadcore.StringPointer(strings.TrimSpace(decision.ExcludeAssets))
	}
	if decision.DevelopmentOnly {
		correlation.DevelopmentDependency = payloadcore.BoolPointer(true)
	}
	if decision.TestDependency {
		correlation.TestDependency = payloadcore.BoolPointer(true)
	}
	if strings.TrimSpace(decision.VersionEvidence) != "" {
		correlation.VersionEvidence = payloadcore.StringPointer(strings.TrimSpace(decision.VersionEvidence))
	}
	if strings.TrimSpace(decision.UnresolvedMSBuildProperty) != "" {
		correlation.UnresolvedMSBuildProperty = payloadcore.StringPointer(strings.TrimSpace(decision.UnresolvedMSBuildProperty))
	}
	if strings.TrimSpace(decision.AmbiguousMSBuildProperty) != "" {
		correlation.AmbiguousMSBuildProperty = payloadcore.StringPointer(strings.TrimSpace(decision.AmbiguousMSBuildProperty))
	}
	if decision.PartialEvidence {
		correlation.PartialEvidence = payloadcore.BoolPointer(true)
	}
	return factschema.EncodeReducerPackageConsumptionCorrelation(correlation)
}

func typedPackagePublicationPayload(
	write PackageWrite,
	decision PackagePublicationDecision,
) (map[string]any, error) {
	return factschema.EncodeReducerPackagePublicationCorrelation(reducerderivedv1.PackagePublicationCorrelation{
		PackageID:              decision.PackageID,
		ReducerDomain:          payloadcore.StringPointer(string(reducercontract.DomainPackageSourceCorrelation)),
		IntentID:               payloadcore.StringPointer(write.IntentID),
		ScopeID:                payloadcore.StringPointer(write.ScopeID),
		GenerationID:           payloadcore.StringPointer(write.GenerationID),
		SourceSystem:           payloadcore.StringPointer(write.SourceSystem),
		Cause:                  payloadcore.StringPointer(write.Cause),
		RelationshipKind:       payloadcore.StringPointer("publication"),
		VersionID:              payloadcore.StringPointer(decision.VersionID),
		Version:                payloadcore.StringPointer(decision.Version),
		PublishedAt:            payloadcore.StringPointer(decision.PublishedAt),
		SourceURL:              payloadcore.StringPointer(decision.SourceURL),
		SourceHintFactID:       payloadcore.StringPointer(decision.SourceHintFactID),
		SourceHintKind:         payloadcore.StringPointer(decision.SourceHintKind),
		SourceHintVersionID:    payloadcore.StringPointer(decision.SourceHintVersionID),
		RepositoryID:           payloadcore.StringPointer(decision.RepositoryID),
		RepositoryName:         payloadcore.StringPointer(decision.RepositoryName),
		CandidateRepositoryIDs: payloadcore.UniqueSortedStrings(decision.CandidateRepositoryIDs),
		Outcome:                payloadcore.StringPointer(string(decision.Outcome)),
		Reason:                 payloadcore.StringPointer(decision.Reason),
		ProvenanceOnly:         payloadcore.BoolPointer(decision.ProvenanceOnly),
		CanonicalWrites:        payloadcore.IntPointer(decision.CanonicalWrites),
		EvidenceFactIDs:        payloadcore.UniqueSortedStrings(decision.EvidenceFactIDs),
		CorrelationKind:        payloadcore.StringPointer(PackagePublicationFactKind),
		SourceLayers:           []string{string(truth.LayerSourceDeclaration), string(truth.LayerObservedResource)},
	})
}
