package ociruntime

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const warningMissingManifestDigest = "missing_manifest_digest"

// RegistryClient is the narrow OCI Distribution contract used by the runtime.
type RegistryClient interface {
	Ping(context.Context) error
	ListTags(context.Context, string) ([]string, error)
	GetManifest(context.Context, string, string) (distribution.ManifestResponse, error)
	ListReferrers(context.Context, string, string) (distribution.ReferrersResponse, error)
}

// ClientFactory creates a registry client for one configured target.
type ClientFactory interface {
	Client(context.Context, TargetConfig) (RegistryClient, error)
}

// ClientFactoryFunc adapts a function into a ClientFactory.
type ClientFactoryFunc func(context.Context, TargetConfig) (RegistryClient, error)

// Client creates a registry client.
func (f ClientFactoryFunc) Client(ctx context.Context, target TargetConfig) (RegistryClient, error) {
	return f(ctx, target)
}

// Source scans configured OCI registry repositories and yields generations.
type Source struct {
	Config        Config
	ClientFactory ClientFactory
	Tracer        trace.Tracer
	Instruments   *telemetry.Instruments
	Logger        *slog.Logger
	Clock         func() time.Time

	next int
}

// Next returns the next configured registry repository generation.
func (s *Source) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	config, err := s.Config.validated()
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	if s.ClientFactory == nil {
		return collector.CollectedGeneration{}, false, fmt.Errorf("OCI registry client factory is required")
	}
	if s.next >= len(config.Targets) {
		s.next = 0
		return collector.CollectedGeneration{}, false, nil
	}
	target := config.Targets[s.next]
	s.next++
	collected, err := s.scanTarget(ctx, config, target, "")
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	return collected, true, nil
}

func (s *Source) scanTarget(ctx context.Context, config Config, target TargetConfig, generationID string) (collector.CollectedGeneration, error) {
	start := time.Now()
	if s.Tracer != nil {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanOCIRegistryScan)
		span.SetAttributes(attribute.String("provider", string(target.Provider)))
		defer span.End()
	}
	result := "success"
	defer func() {
		if s.Instruments != nil {
			s.Instruments.OCIRegistryScanDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
				telemetry.AttrProvider(string(target.Provider)),
				telemetry.AttrResult(result),
			))
		}
	}()

	client, err := s.ClientFactory.Client(ctx, target)
	if err != nil {
		result = "failed"
		return collector.CollectedGeneration{}, fmt.Errorf("create OCI registry client: %w", err)
	}
	if err := s.recordAPICall(ctx, target, "ping", func(context.Context) error { return client.Ping(ctx) }); err != nil {
		result = "failed"
		return collector.CollectedGeneration{}, fmt.Errorf("ping OCI registry: %w", err)
	}
	tags, err := s.listReferences(ctx, client, target)
	if err != nil {
		result = "failed"
		return collector.CollectedGeneration{}, err
	}

	observedAt := s.now()
	scopeValue, generationValue, err := s.scopeAndGeneration(target, observedAt, tags, generationID)
	if err != nil {
		result = "failed"
		return collector.CollectedGeneration{}, err
	}
	envelopes, err := s.buildEnvelopes(ctx, client, target, config.CollectorInstanceID, generationValue.GenerationID, observedAt, tags)
	if err != nil {
		result = "failed"
		return collector.CollectedGeneration{}, err
	}
	if s.Logger != nil {
		s.Logger.InfoContext(ctx, "OCI registry scan completed",
			telemetry.PhaseAttr(telemetry.PhaseDiscovery),
			slog.String(telemetry.LogKeyScopeID, scopeValue.ScopeID),
			slog.String(telemetry.LogKeyGenerationID, generationValue.GenerationID),
			slog.String("provider", string(target.Provider)),
			slog.Int("reference_count", len(tags)),
			slog.Int("fact_count", len(envelopes)),
			slog.Float64("duration_seconds", time.Since(start).Seconds()),
		)
	}
	return collector.FactsFromSlice(scopeValue, generationValue, envelopes), nil
}

func (s *Source) listReferences(ctx context.Context, client RegistryClient, target TargetConfig) ([]string, error) {
	if len(target.References) > 0 {
		return append([]string(nil), target.References...), nil
	}
	var tags []string
	err := s.recordAPICall(ctx, target, "list_tags", func(context.Context) error {
		var err error
		tags, err = client.ListTags(ctx, target.Repository)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list OCI registry tags: %w", err)
	}
	slices.Sort(tags)
	tags = slices.Compact(tags)
	if len(tags) > target.TagLimit {
		tags = tags[:target.TagLimit]
	}
	if s.Instruments != nil {
		s.Instruments.OCIRegistryTagsObserved.Add(ctx, int64(len(tags)), metric.WithAttributes(
			telemetry.AttrProvider(string(target.Provider)),
			telemetry.AttrResult("success"),
		))
	}
	return tags, nil
}

func (s *Source) buildEnvelopes(
	ctx context.Context,
	client RegistryClient,
	target TargetConfig,
	collectorInstanceID string,
	generationID string,
	observedAt time.Time,
	references []string,
) ([]facts.Envelope, error) {
	envelopes := make([]facts.Envelope, 0, 1+(len(references)*4))
	repositoryEnvelope, err := ociregistry.NewRepositoryEnvelope(ociregistry.RepositoryObservation{
		Identity:            target.identity(),
		GenerationID:        generationID,
		CollectorInstanceID: collectorInstanceID,
		FencingToken:        target.FencingToken,
		ObservedAt:          observedAt,
		Visibility:          target.Visibility,
		AuthMode:            target.AuthMode,
		SourceURI:           target.SourceURI,
	})
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, repositoryEnvelope)

	for _, reference := range references {
		manifest, err := s.getManifest(ctx, client, target, reference)
		if err != nil {
			return nil, fmt.Errorf("get OCI manifest: %w", err)
		}
		mediaType, parsed, err := parseManifest(manifest)
		if err != nil {
			return nil, fmt.Errorf("parse OCI manifest: %w", err)
		}
		if s.Instruments != nil {
			s.Instruments.OCIRegistryManifestsObserved.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrProvider(string(target.Provider)),
				telemetry.AttrMediaFamily(mediaFamily(mediaType)),
			))
		}
		digest, digestWarning, ok := manifestDigest(manifest)
		if !ok {
			warning, warningErr := s.warningEnvelope(target, collectorInstanceID, generationID, observedAt, warningMissingManifestDigest, reference, "")
			if warningErr != nil {
				return nil, warningErr
			}
			envelopes = append(envelopes, warning)
			continue
		}
		if digestWarning != "" {
			warning, warningErr := s.warningEnvelope(target, collectorInstanceID, generationID, observedAt, digestWarning, reference, digest)
			if warningErr != nil {
				return nil, warningErr
			}
			envelopes = append(envelopes, warning)
		}

		descriptor := ociregistry.Descriptor{Digest: digest, MediaType: mediaType, SizeBytes: manifest.SizeBytes}
		tagEnvelope, err := ociregistry.NewTagObservationEnvelope(ociregistry.TagObservation{
			Repository:          target.identity(),
			Tag:                 reference,
			Digest:              digest,
			MediaType:           mediaType,
			GenerationID:        generationID,
			CollectorInstanceID: collectorInstanceID,
			FencingToken:        target.FencingToken,
			ObservedAt:          observedAt,
			SourceURI:           target.SourceURI,
		})
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, tagEnvelope)

		factsForManifest, err := s.manifestEnvelopes(target, collectorInstanceID, generationID, observedAt, reference, descriptor, parsed)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, factsForManifest...)
		referrerEnvelopes, err := s.referrerEnvelopes(ctx, client, target, collectorInstanceID, generationID, observedAt, descriptor)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, referrerEnvelopes...)
	}
	return envelopes, nil
}

func (s *Source) getManifest(ctx context.Context, client RegistryClient, target TargetConfig, reference string) (distribution.ManifestResponse, error) {
	var manifest distribution.ManifestResponse
	err := s.recordAPICall(ctx, target, "get_manifest", func(context.Context) error {
		var err error
		manifest, err = client.GetManifest(ctx, target.Repository, reference)
		return err
	})
	return manifest, err
}

func (s *Source) manifestEnvelopes(
	target TargetConfig,
	collectorInstanceID string,
	generationID string,
	observedAt time.Time,
	sourceTag string,
	descriptor ociregistry.Descriptor,
	parsed parsedManifest,
) ([]facts.Envelope, error) {
	switch mediaFamily(descriptor.MediaType) {
	case "image_index":
		envelope, err := ociregistry.NewImageIndexEnvelope(ociregistry.IndexObservation{
			Repository:          target.identity(),
			Descriptor:          descriptor,
			Manifests:           parsed.Manifests,
			GenerationID:        generationID,
			CollectorInstanceID: collectorInstanceID,
			FencingToken:        target.FencingToken,
			ObservedAt:          observedAt,
			SourceURI:           target.SourceURI,
		})
		return []facts.Envelope{envelope}, err
	default:
		envelopes := make([]facts.Envelope, 0, 2+len(parsed.Layers))
		manifestEnvelope, err := ociregistry.NewManifestEnvelope(ociregistry.ManifestObservation{
			Repository:          target.identity(),
			Descriptor:          descriptor,
			Config:              parsed.Config,
			Layers:              parsed.Layers,
			SourceTag:           sourceTag,
			GenerationID:        generationID,
			CollectorInstanceID: collectorInstanceID,
			FencingToken:        target.FencingToken,
			ObservedAt:          observedAt,
			SourceURI:           target.SourceURI,
		})
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, manifestEnvelope)
		for _, child := range append([]ociregistry.Descriptor{parsed.Config}, parsed.Layers...) {
			if strings.TrimSpace(child.Digest) == "" {
				continue
			}
			childEnvelope, err := ociregistry.NewDescriptorEnvelope(ociregistry.DescriptorObservation{
				Repository:          target.identity(),
				Descriptor:          child,
				GenerationID:        generationID,
				CollectorInstanceID: collectorInstanceID,
				FencingToken:        target.FencingToken,
				ObservedAt:          observedAt,
				SourceURI:           target.SourceURI,
			})
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, childEnvelope)
		}
		return envelopes, nil
	}
}

func (s *Source) referrerEnvelopes(
	ctx context.Context,
	client RegistryClient,
	target TargetConfig,
	collectorInstanceID string,
	generationID string,
	observedAt time.Time,
	subject ociregistry.Descriptor,
) ([]facts.Envelope, error) {
	var referrers distribution.ReferrersResponse
	err := s.recordAPICall(ctx, target, "list_referrers", func(context.Context) error {
		var err error
		referrers, err = client.ListReferrers(ctx, target.Repository, subject.Digest)
		return err
	})
	if err != nil {
		warning, warningErr := s.warningEnvelope(target, collectorInstanceID, generationID, observedAt, ociregistry.WarningUnsupportedReferrersAPI, err.Error(), subject.Digest)
		if warningErr != nil {
			return nil, warningErr
		}
		return []facts.Envelope{warning}, nil
	}
	envelopes := make([]facts.Envelope, 0, len(referrers.Referrers))
	for _, referrer := range referrers.Referrers {
		envelope, err := ociregistry.NewReferrerEnvelope(ociregistry.ReferrerObservation{
			Repository:          target.identity(),
			Subject:             subject,
			Referrer:            referrer,
			SourceAPIPath:       "/v2/<repository>/referrers/<digest>",
			GenerationID:        generationID,
			CollectorInstanceID: collectorInstanceID,
			FencingToken:        target.FencingToken,
			ObservedAt:          observedAt,
			SourceURI:           target.SourceURI,
		})
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
		if s.Instruments != nil {
			s.Instruments.OCIRegistryReferrersObserved.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrProvider(string(target.Provider)),
				telemetry.AttrArtifactFamily(artifactFamily(referrer.ArtifactType)),
			))
		}
	}
	return envelopes, nil
}

func (s *Source) warningEnvelope(
	target TargetConfig,
	collectorInstanceID string,
	generationID string,
	observedAt time.Time,
	code string,
	message string,
	digest string,
) (facts.Envelope, error) {
	repository := target.identity()
	return ociregistry.NewWarningEnvelope(ociregistry.WarningObservation{
		WarningKey:          code + ":" + message,
		WarningCode:         code,
		Severity:            ociregistry.SeverityInfo,
		Message:             message,
		Repository:          &repository,
		Digest:              digest,
		GenerationID:        generationID,
		CollectorInstanceID: collectorInstanceID,
		FencingToken:        target.FencingToken,
		ObservedAt:          observedAt,
		SourceURI:           target.SourceURI,
	})
}

func (s *Source) scopeAndGeneration(target TargetConfig, observedAt time.Time, references []string, generationID string) (scope.IngestionScope, scope.ScopeGeneration, error) {
	identity, err := ociregistry.NormalizeRepositoryIdentity(target.identity())
	if err != nil {
		return scope.IngestionScope{}, scope.ScopeGeneration{}, err
	}
	generationID = strings.TrimSpace(generationID)
	if generationID == "" {
		fingerprint := facts.StableID("OCIRegistryGeneration", map[string]any{
			"provider":   string(identity.Provider),
			"registry":   identity.Registry,
			"repository": identity.Repository,
			"refs":       references,
			"observed":   observedAt.UTC().Format(time.RFC3339Nano),
		})
		generationID = "oci-registry:" + fingerprint
	}
	scopeValue := scope.IngestionScope{
		ScopeID:       identity.ScopeID,
		SourceSystem:  ociregistry.CollectorKind,
		ScopeKind:     scope.KindContainerRegistryRepository,
		CollectorKind: scope.CollectorOCIRegistry,
		PartitionKey:  string(target.Provider),
		Metadata: map[string]string{
			"provider": string(target.Provider),
		},
	}
	generationValue := scope.ScopeGeneration{
		ScopeID:      identity.ScopeID,
		GenerationID: generationID,
		Status:       scope.GenerationStatusPending,
		ObservedAt:   observedAt,
		IngestedAt:   observedAt,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	return scopeValue, generationValue, generationValue.ValidateForScope(scopeValue)
}

func (s *Source) recordAPICall(ctx context.Context, target TargetConfig, operation string, call func(context.Context) error) error {
	if s.Tracer != nil {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanOCIRegistryAPICall)
		span.SetAttributes(attribute.String("provider", string(target.Provider)), attribute.String("operation", operation))
		defer span.End()
	}
	err := call(ctx)
	result := "success"
	if err != nil {
		result = "error"
	}
	if s.Instruments != nil {
		s.Instruments.OCIRegistryAPICalls.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrProvider(string(target.Provider)),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		))
	}
	return err
}

func (s *Source) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}
