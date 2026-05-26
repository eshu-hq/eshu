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

const warningCodeMetadataNotFound = "metadata_not_found"

func (s *ClaimedSource) missingMetadataWarningGeneration(
	item workflow.WorkItem,
	target TargetConfig,
) (collector.CollectedGeneration, error) {
	observedAt := s.now().UTC()
	warning := packageregistry.WarningObservation{
		WarningKey:          warningCodeMetadataNotFound,
		WarningCode:         warningCodeMetadataNotFound,
		Severity:            "warning",
		Message:             "derived package registry metadata was not found; package evidence remains missing",
		Package:             targetPackageIdentity(target),
		ScopeID:             target.Base.ScopeID,
		GenerationID:        item.GenerationID,
		CollectorInstanceID: s.collectorInstanceID,
		FencingToken:        item.CurrentFencingToken,
		ObservedAt:          observedAt,
		SourceURI:           safeSourceURI(firstNonBlank(target.Base.SourceURI, target.MetadataURL)),
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
		Metadata: map[string]string{
			"provider":  target.Base.Provider,
			"ecosystem": string(target.Base.Ecosystem),
			"registry":  target.Base.Registry,
		},
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
