// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageruntime

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func (s *ClaimedSource) prefetchWarningGeneration(
	item workflow.WorkItem,
	target TargetConfig,
) (collector.CollectedGeneration, bool, error) {
	switch {
	case targetNeedsCredentials(target):
		collected, err := s.credentialsMissingWarningGeneration(item, target)
		return collected, true, err
	case targetHasNoNativeMetadataSource(target):
		collected, err := s.unsupportedMetadataSourceWarningGeneration(item, target)
		return collected, true, err
	default:
		return collector.CollectedGeneration{}, false, nil
	}
}

func (s *ClaimedSource) parserWarningGeneration(
	item workflow.WorkItem,
	target TargetConfig,
	parseErr error,
) (collector.CollectedGeneration, bool, error) {
	switch parserWarningCode(parseErr) {
	case warningCodeUnsupportedMetadataSource:
		collected, err := s.unsupportedMetadataSourceWarningGeneration(item, target)
		return collected, true, err
	case warningCodeMalformedMetadata:
		collected, err := s.malformedMetadataWarningGeneration(item, target)
		return collected, true, err
	default:
		return collector.CollectedGeneration{}, false, nil
	}
}

func targetNeedsCredentials(target TargetConfig) bool {
	return target.Base.Visibility == packageregistry.VisibilityPrivate &&
		strings.TrimSpace(target.MetadataURL) != "" &&
		strings.TrimSpace(target.BearerToken) == "" &&
		strings.TrimSpace(target.Username) == "" &&
		target.Password == ""
}

func targetHasNoNativeMetadataSource(target TargetConfig) bool {
	if strings.TrimSpace(target.MetadataURL) != "" ||
		strings.TrimSpace(target.DocumentFormat) == DocumentFormatArtifactoryPackage {
		return false
	}
	switch target.Base.Ecosystem {
	case packageregistry.EcosystemGoModule,
		packageregistry.EcosystemMaven,
		packageregistry.EcosystemNuGet,
		packageregistry.EcosystemComposer,
		packageregistry.EcosystemRubyGems,
		packageregistry.EcosystemCargo:
		return true
	default:
		return false
	}
}

func parserWarningCode(err error) string {
	message := err.Error()
	if strings.Contains(message, "not registered") {
		return warningCodeUnsupportedMetadataSource
	}
	return warningCodeMalformedMetadata
}
