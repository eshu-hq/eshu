// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageruntime

import (
	"errors"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	warningCodeMetadataNotFound          = "registry_not_found"
	warningCodeMetadataTooLarge          = "metadata_too_large"
	warningCodeUnsupportedMetadataSource = "unsupported_metadata_source"
	warningCodeMalformedMetadata         = "malformed_metadata"
	warningCodeCredentialsMissing        = "credentials_missing"
)

func (s *ClaimedSource) missingMetadataWarningGeneration(
	item workflow.WorkItem,
	target TargetConfig,
) (collector.CollectedGeneration, error) {
	return s.warningGeneration(
		item,
		target,
		warningCodeMetadataNotFound,
		"derived package registry metadata was not found; package evidence remains missing",
	)
}

func (s *ClaimedSource) unsupportedMetadataSourceWarningGeneration(
	item workflow.WorkItem,
	target TargetConfig,
) (collector.CollectedGeneration, error) {
	return s.warningGeneration(
		item,
		target,
		warningCodeUnsupportedMetadataSource,
		"package registry metadata source is not supported by a native adapter; package evidence remains missing",
	)
}

func (s *ClaimedSource) malformedMetadataWarningGeneration(
	item workflow.WorkItem,
	target TargetConfig,
) (collector.CollectedGeneration, error) {
	return s.warningGeneration(
		item,
		target,
		warningCodeMalformedMetadata,
		"package registry metadata was malformed; package evidence remains missing",
	)
}

func (s *ClaimedSource) credentialsMissingWarningGeneration(
	item workflow.WorkItem,
	target TargetConfig,
) (collector.CollectedGeneration, error) {
	return s.warningGeneration(
		item,
		target,
		warningCodeCredentialsMissing,
		"package registry metadata requires credentials that were not configured; package evidence remains missing",
	)
}

func (s *ClaimedSource) metadataTooLargeWarningGeneration(
	item workflow.WorkItem,
	target TargetConfig,
) (collector.CollectedGeneration, error) {
	return s.warningGeneration(
		item,
		target,
		warningCodeMetadataTooLarge,
		"package registry metadata exceeded the configured byte limit; package evidence remains unsupported",
	)
}

func (s *ClaimedSource) warningGeneration(
	item workflow.WorkItem,
	target TargetConfig,
	warningCode string,
	message string,
) (collector.CollectedGeneration, error) {
	observedAt := s.now().UTC()
	warning := packageregistry.WarningObservation{
		WarningKey:          warningCode,
		WarningCode:         warningCode,
		Severity:            "warning",
		Message:             message,
		Ecosystem:           target.Base.Ecosystem,
		Package:             targetWarningPackageIdentity(target, warningCode),
		ScopeID:             target.Base.ScopeID,
		GenerationID:        item.GenerationID,
		CollectorInstanceID: s.collectorInstanceID,
		FencingToken:        item.CurrentFencingToken,
		ObservedAt:          observedAt,
		SourceURI:           targetWarningSourceURI(target, warningCode),
	}
	envelope, err := packageregistry.NewWarningEnvelope(warning)
	if err != nil {
		return collector.CollectedGeneration{}, err
	}
	ingestionScope := scope.IngestionScope{
		ScopeID:       target.Base.ScopeID,
		SourceSystem:  string(scope.CollectorPackageRegistry),
		ScopeKind:     scope.KindPackageRegistry,
		CollectorKind: scope.CollectorPackageRegistry,
		PartitionKey:  target.Base.Provider + ":" + string(target.Base.Ecosystem),
		Metadata:      targetWarningScopeMetadata(target, warningCode),
	}
	generation := scope.ScopeGeneration{
		GenerationID: item.GenerationID,
		ScopeID:      target.Base.ScopeID,
		ObservedAt:   observedAt,
		IngestedAt:   observedAt,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	return collector.FactsFromSlice(ingestionScope, generation, []facts.Envelope{envelope}), nil
}

func targetWarningPackageIdentity(target TargetConfig, warningCode string) *packageregistry.PackageIdentity {
	if suppressPrivateTargetDetail(target, warningCode) {
		return nil
	}
	return targetPackageIdentity(target)
}

func targetWarningSourceURI(target TargetConfig, warningCode string) string {
	if suppressPrivateTargetDetail(target, warningCode) {
		return ""
	}
	return safeSourceURI(firstNonBlank(target.Base.SourceURI, target.MetadataURL))
}

func targetWarningScopeMetadata(target TargetConfig, warningCode string) map[string]string {
	metadata := map[string]string{
		"provider":  target.Base.Provider,
		"ecosystem": string(target.Base.Ecosystem),
	}
	if !suppressPrivateTargetDetail(target, warningCode) {
		metadata["registry"] = target.Base.Registry
	}
	return metadata
}

func suppressPrivateTargetDetail(target TargetConfig, warningCode string) bool {
	return warningCode == warningCodeCredentialsMissing ||
		target.Base.Visibility == packageregistry.VisibilityPrivate
}

func targetPackageIdentity(target TargetConfig) *packageregistry.PackageIdentity {
	name := ""
	if len(target.Base.Packages) > 0 {
		name = strings.TrimSpace(target.Base.Packages[0])
	}
	if name == "" {
		return nil
	}
	identity := packageregistry.PackageIdentity{
		Ecosystem: target.Base.Ecosystem,
		Registry:  target.Base.Registry,
		Namespace: target.Base.Namespace,
		RawName:   name,
	}
	return &identity
}

func isRegistryNotFound(err error) bool {
	var failure interface{ FailureClass() string }
	return errors.As(err, &failure) &&
		strings.TrimSpace(failure.FailureClass()) == collector.RegistryFailureNotFound
}
