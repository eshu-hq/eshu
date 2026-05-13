package packageruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	// DocumentFormatNative routes metadata directly to the ecosystem parser.
	DocumentFormatNative = "native"
	// DocumentFormatArtifactoryPackage routes metadata through the Artifactory
	// package wrapper before package-native parsing.
	DocumentFormatArtifactoryPackage = "artifactory_package"
)

// MetadataProvider fetches one bounded package metadata document for a target.
type MetadataProvider interface {
	FetchMetadata(context.Context, TargetConfig) (MetadataDocument, error)
}

// MetadataDocument is the parser input returned by a registry client.
type MetadataDocument struct {
	Body         []byte
	SourceURI    string
	DocumentType string
	ObservedAt   time.Time
}

// SourceConfig configures a claim-driven package-registry source.
type SourceConfig struct {
	CollectorInstanceID string
	Targets             []TargetConfig
	Provider            MetadataProvider
	Now                 func() time.Time
	Tracer              trace.Tracer
	Instruments         *telemetry.Instruments
}

// TargetConfig couples shared package-registry identity with runtime-only
// provider material such as metadata endpoints and resolved credentials.
type TargetConfig struct {
	Base           packageregistry.TargetConfig
	MetadataURL    string
	DocumentFormat string
	Username       string
	Password       string
	BearerToken    string
}

// ClaimedSource implements collector.ClaimedSource for package registries.
type ClaimedSource struct {
	collectorInstanceID string
	targets             map[string]TargetConfig
	provider            MetadataProvider
	parserRegistry      packageregistry.MetadataParserRegistry
	now                 func() time.Time
	tracer              trace.Tracer
	instruments         *telemetry.Instruments
}

// NewClaimedSource validates config and builds a claim-driven package-registry
// source.
func NewClaimedSource(config SourceConfig) (*ClaimedSource, error) {
	if config.Provider == nil {
		return nil, fmt.Errorf("package registry metadata provider is required")
	}
	baseTargets := make([]packageregistry.TargetConfig, 0, len(config.Targets))
	for _, target := range config.Targets {
		baseTargets = append(baseTargets, target.Base)
	}
	validated, err := packageregistry.RuntimeConfig{
		CollectorInstanceID: config.CollectorInstanceID,
		Targets:             baseTargets,
	}.Validate()
	if err != nil {
		return nil, err
	}
	targets := make(map[string]TargetConfig, len(validated.Targets))
	for i, base := range validated.Targets {
		target := config.Targets[i]
		target.Base = base
		target.MetadataURL = strings.TrimRight(strings.TrimSpace(target.MetadataURL), "/")
		documentFormat, err := normalizeDocumentFormat(target.DocumentFormat)
		if err != nil {
			return nil, fmt.Errorf("target %d: %w", i, err)
		}
		target.DocumentFormat = documentFormat
		if _, exists := targets[base.ScopeID]; exists {
			return nil, fmt.Errorf("duplicate package registry target scope_id %q", base.ScopeID)
		}
		targets[base.ScopeID] = target
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &ClaimedSource{
		collectorInstanceID: validated.CollectorInstanceID,
		targets:             targets,
		provider:            config.Provider,
		parserRegistry:      packageregistry.DefaultMetadataParserRegistry(),
		now:                 now,
		tracer:              config.Tracer,
		instruments:         config.Instruments,
	}, nil
}

// NextClaimed collects the claimed package-registry target named by item.ScopeID.
func (s *ClaimedSource) NextClaimed(
	ctx context.Context,
	item workflow.WorkItem,
) (collector.CollectedGeneration, bool, error) {
	if strings.TrimSpace(item.CollectorInstanceID) != s.collectorInstanceID {
		return collector.CollectedGeneration{}, false, fmt.Errorf(
			"package registry work item collector_instance_id %q does not match source %q",
			item.CollectorInstanceID,
			s.collectorInstanceID,
		)
	}
	if item.CollectorKind != "" && item.CollectorKind != scope.CollectorPackageRegistry {
		return collector.CollectedGeneration{}, false, fmt.Errorf(
			"package registry source cannot collect %q work items",
			item.CollectorKind,
		)
	}
	if strings.TrimSpace(item.GenerationID) == "" {
		return collector.CollectedGeneration{}, false, fmt.Errorf("package registry work item generation_id is required")
	}
	target, ok := s.targets[strings.TrimSpace(item.ScopeID)]
	if !ok {
		return collector.CollectedGeneration{}, false, fmt.Errorf("package registry target scope_id %q is not configured", item.ScopeID)
	}
	startedAt := time.Now()
	observeCtx, span := s.startObserve(ctx, target)
	defer span.End()

	document, err := s.fetchMetadata(observeCtx, target)
	if err != nil {
		s.recordObserve(observeCtx, target, "error", startedAt)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return collector.CollectedGeneration{}, false, fmt.Errorf("fetch package registry metadata: %w", err)
	}
	collected, err := s.collectDocument(observeCtx, item, target, document)
	if err != nil {
		s.recordObserve(observeCtx, target, "error", startedAt)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return collector.CollectedGeneration{}, false, err
	}
	s.recordGenerationLag(observeCtx, target, document, startedAt)
	s.recordObserve(observeCtx, target, "success", startedAt)
	return collected, true, nil
}

func (s *ClaimedSource) startObserve(ctx context.Context, target TargetConfig) (context.Context, trace.Span) {
	if s.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	attrs := []attribute.KeyValue{
		attribute.String(telemetry.MetricDimensionProvider, target.Base.Provider),
		attribute.String(telemetry.MetricDimensionEcosystem, string(target.Base.Ecosystem)),
	}
	return s.tracer.Start(ctx, telemetry.SpanPackageRegistryObserve, trace.WithAttributes(attrs...))
}

func (s *ClaimedSource) fetchMetadata(ctx context.Context, target TargetConfig) (MetadataDocument, error) {
	fetchCtx := ctx
	var span trace.Span
	if s.tracer != nil {
		fetchCtx, span = s.tracer.Start(ctx, telemetry.SpanPackageRegistryFetch)
		defer span.End()
	}
	document, err := s.provider.FetchMetadata(fetchCtx, target)
	statusClass := "success"
	if err != nil {
		statusClass = "error"
		if errors.Is(err, ErrRateLimited) {
			statusClass = "rate_limited"
			if s.instruments != nil {
				s.instruments.PackageRegistryRateLimited.Add(ctx, 1, metric.WithAttributes(
					attribute.String(telemetry.MetricDimensionEcosystem, string(target.Base.Ecosystem)),
				))
			}
		}
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
	}
	if s.instruments != nil {
		s.instruments.PackageRegistryRequests.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionEcosystem, string(target.Base.Ecosystem)),
			attribute.String(telemetry.MetricDimensionStatusClass, statusClass),
		))
	}
	return document, err
}

func (s *ClaimedSource) collectDocument(
	ctx context.Context,
	item workflow.WorkItem,
	target TargetConfig,
	document MetadataDocument,
) (collector.CollectedGeneration, error) {
	observedAt := document.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = s.now().UTC()
	}
	sourceURI := safeSourceURI(firstNonBlank(document.SourceURI, target.Base.SourceURI, target.MetadataURL))
	parserCtx := packageregistry.MetadataParserContext{
		Ecosystem:           target.Base.Ecosystem,
		Registry:            target.Base.Registry,
		ScopeID:             target.Base.ScopeID,
		GenerationID:        item.GenerationID,
		CollectorInstanceID: s.collectorInstanceID,
		FencingToken:        item.CurrentFencingToken,
		ObservedAt:          observedAt,
		SourceURI:           sourceURI,
		Visibility:          target.Base.Visibility,
	}
	parsed, err := s.parseMetadata(parserCtx, target, document.Body)
	if err != nil {
		if s.instruments != nil {
			s.instruments.PackageRegistryParseFailures.Add(ctx, 1, metric.WithAttributes(
				attribute.String(telemetry.MetricDimensionEcosystem, string(target.Base.Ecosystem)),
				attribute.String(telemetry.MetricDimensionDocumentType, firstNonBlank(document.DocumentType, string(target.Base.Ecosystem))),
			))
		}
		return collector.CollectedGeneration{}, fmt.Errorf("parse package registry %s metadata: %w", target.Base.Ecosystem, err)
	}
	parsed, err = boundedParsedMetadata(target.Base, parsed)
	if err != nil {
		return collector.CollectedGeneration{}, err
	}
	envs, err := envelopesFromParsedMetadata(parsed)
	if err != nil {
		return collector.CollectedGeneration{}, err
	}
	s.recordFactEnvelopes(ctx, target, envs)
	ingestionScope := scope.IngestionScope{
		ScopeID:       target.Base.ScopeID,
		SourceSystem:  string(scope.CollectorPackageRegistry),
		ScopeKind:     scope.KindPackageRegistry,
		CollectorKind: scope.CollectorPackageRegistry,
		PartitionKey:  fmt.Sprintf("%s:%s", target.Base.Provider, target.Base.Ecosystem),
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
	return collector.FactsFromSlice(ingestionScope, generation, envs), nil
}

func (s *ClaimedSource) parseMetadata(
	ctx packageregistry.MetadataParserContext,
	target TargetConfig,
	document []byte,
) (packageregistry.ParsedMetadata, error) {
	switch target.DocumentFormat {
	case "", DocumentFormatNative:
		return s.parserRegistry.Parse(ctx, document)
	case DocumentFormatArtifactoryPackage:
		return packageregistry.ParseArtifactoryPackageMetadata(ctx, document)
	default:
		return packageregistry.ParsedMetadata{}, fmt.Errorf("unsupported package registry document_format %q", target.DocumentFormat)
	}
}

func (s *ClaimedSource) recordObserve(ctx context.Context, target TargetConfig, result string, startedAt time.Time) {
	if s.instruments == nil {
		return
	}
	s.instruments.PackageRegistryObserveDuration.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, target.Base.Provider),
		attribute.String(telemetry.MetricDimensionEcosystem, string(target.Base.Ecosystem)),
		attribute.String(telemetry.MetricDimensionResult, result),
	))
}

func (s *ClaimedSource) recordFactEnvelopes(ctx context.Context, target TargetConfig, envs []facts.Envelope) {
	if s.instruments == nil {
		return
	}
	counts := map[string]int64{}
	for _, envelope := range envs {
		counts[envelope.FactKind]++
	}
	for factKind, count := range counts {
		s.instruments.PackageRegistryFactsEmitted.Add(ctx, count, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionEcosystem, string(target.Base.Ecosystem)),
			attribute.String(telemetry.MetricDimensionFactKind, factKind),
		))
	}
}

func (s *ClaimedSource) recordGenerationLag(
	ctx context.Context,
	target TargetConfig,
	document MetadataDocument,
	startedAt time.Time,
) {
	if s.instruments == nil || document.ObservedAt.IsZero() {
		return
	}
	lag := startedAt.UTC().Sub(document.ObservedAt.UTC()).Seconds()
	if lag < 0 {
		lag = 0
	}
	s.instruments.PackageRegistryGenerationLag.Record(ctx, lag, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionEcosystem, string(target.Base.Ecosystem)),
	))
}

func envelopesFromParsedMetadata(parsed packageregistry.ParsedMetadata) ([]facts.Envelope, error) {
	envs := make([]facts.Envelope, 0,
		len(parsed.Packages)+len(parsed.Versions)+len(parsed.Dependencies)+
			len(parsed.Artifacts)+len(parsed.SourceHints)+len(parsed.Vulnerables)+
			len(parsed.Events)+len(parsed.Hosting)+len(parsed.Warnings),
	)
	var err error
	for _, observation := range parsed.Packages {
		envelope, buildErr := packageregistry.NewPackageEnvelope(observation)
		envs, err = appendEnvelope(envs, envelope, buildErr)
		if err != nil {
			return nil, err
		}
	}
	for _, observation := range parsed.Versions {
		envelope, buildErr := packageregistry.NewPackageVersionEnvelope(observation)
		envs, err = appendEnvelope(envs, envelope, buildErr)
		if err != nil {
			return nil, err
		}
	}
	for _, observation := range parsed.Dependencies {
		envelope, buildErr := packageregistry.NewPackageDependencyEnvelope(observation)
		envs, err = appendEnvelope(envs, envelope, buildErr)
		if err != nil {
			return nil, err
		}
	}
	for _, observation := range parsed.Artifacts {
		envelope, buildErr := packageregistry.NewPackageArtifactEnvelope(observation)
		envs, err = appendEnvelope(envs, envelope, buildErr)
		if err != nil {
			return nil, err
		}
	}
	for _, observation := range parsed.SourceHints {
		envelope, buildErr := packageregistry.NewSourceHintEnvelope(observation)
		envs, err = appendEnvelope(envs, envelope, buildErr)
		if err != nil {
			return nil, err
		}
	}
	for _, observation := range parsed.Vulnerables {
		envelope, buildErr := packageregistry.NewVulnerabilityHintEnvelope(observation)
		envs, err = appendEnvelope(envs, envelope, buildErr)
		if err != nil {
			return nil, err
		}
	}
	for _, observation := range parsed.Events {
		envelope, buildErr := packageregistry.NewRegistryEventEnvelope(observation)
		envs, err = appendEnvelope(envs, envelope, buildErr)
		if err != nil {
			return nil, err
		}
	}
	for _, observation := range parsed.Hosting {
		envelope, buildErr := packageregistry.NewRepositoryHostingEnvelope(observation)
		envs, err = appendEnvelope(envs, envelope, buildErr)
		if err != nil {
			return nil, err
		}
	}
	for _, observation := range parsed.Warnings {
		envelope, buildErr := packageregistry.NewWarningEnvelope(observation)
		envs, err = appendEnvelope(envs, envelope, buildErr)
		if err != nil {
			return nil, err
		}
	}
	return envs, nil
}

func appendEnvelope(envs []facts.Envelope, envelope facts.Envelope, err error) ([]facts.Envelope, error) {
	if err != nil {
		return nil, err
	}
	return append(envs, envelope), nil
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeDocumentFormat(format string) (string, error) {
	normalized := strings.TrimSpace(format)
	if normalized == "" {
		return DocumentFormatNative, nil
	}
	switch normalized {
	case DocumentFormatNative, DocumentFormatArtifactoryPackage:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported package registry document_format %q", normalized)
	}
}
