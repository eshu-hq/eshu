package awsruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/checkpoint"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ClaimedSource resolves one AWS workflow claim into a collected generation.
type ClaimedSource struct {
	Config      Config
	Credentials CredentialProvider
	Scanners    ScannerFactory
	Clock       func() time.Time
	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	Limiter     *AccountLimiter
	Checkpoints CheckpointStore
	ScanStatus  ScanStatusStore
}

type claimTarget struct {
	AccountID   string `json:"account_id"`
	Region      string `json:"region"`
	ServiceKind string `json:"service_kind"`
}

// NextClaimed implements collector.ClaimedSource for AWS cloud work.
func (s ClaimedSource) NextClaimed(
	ctx context.Context,
	item workflow.WorkItem,
) (collected collector.CollectedGeneration, found bool, err error) {
	if err := s.validate(item); err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	target, err := s.targetForClaim(item)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	releaseSlot, err := s.acquireAccountSlot(ctx, target)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	defer releaseSlot()
	if s.Tracer != nil {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanAWSCollectorClaimProcess)
		span.SetAttributes(
			telemetry.AttrAccount(target.AccountID),
			telemetry.AttrRegion(target.Region),
			telemetry.AttrService(target.ServiceKind),
		)
		defer span.End()
	}
	boundary := s.boundary(item, target)
	scopeValue, generationValue, err := s.scopeAndGeneration(item, target)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	if err := s.expireStaleCheckpoints(ctx, boundary); err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	if err := s.startScanStatus(ctx, boundary); err != nil {
		return collector.CollectedGeneration{}, false, err
	}

	lease, err := s.acquireCredentials(ctx, target, item.LeaseExpiresAt.UTC())
	if err != nil {
		envelope, warningErr := awscloud.NewWarningEnvelope(awscloud.WarningObservation{
			Boundary:    boundary,
			WarningKind: WarningAssumeRoleFailed,
			ErrorClass:  "credential_acquisition_failed",
			Message:     err.Error(),
			Attributes: map[string]any{
				"credential_mode": string(target.Credentials.Mode),
			},
		})
		if warningErr != nil {
			return collector.CollectedGeneration{}, false, warningErr
		}
		s.recordAssumeRoleFailure(ctx, target)
		envelopes := []facts.Envelope{envelope}
		if err := s.observeScanStatus(ctx, boundary, awscloud.APICallStats{}, envelopes, err); err != nil {
			return collector.CollectedGeneration{}, false, err
		}
		return collector.FactsFromSlice(scopeValue, generationValue, []facts.Envelope{envelope}), true, nil
	}
	defer func() {
		if releaseErr := lease.Release(); releaseErr != nil && err == nil {
			err = fmt.Errorf("release AWS credential lease: %w", releaseErr)
		}
	}()

	scanner, err := s.Scanners.Scanner(ctx, target, boundary, lease)
	if err != nil {
		return collector.CollectedGeneration{}, false, fmt.Errorf("create AWS service scanner: %w", err)
	}
	apiRecorder := awscloud.NewAPICallStatsRecorder(boundary)
	scanCtx := awscloud.ContextWithAPICallRecorder(ctx, apiRecorder)
	envelopes, err := s.scanService(scanCtx, target, boundary, scanner)
	if err != nil {
		if statusErr := s.observeScanStatus(ctx, boundary, apiRecorder.Snapshot(), nil, err); statusErr != nil {
			return collector.CollectedGeneration{}, false, statusErr
		}
		return collector.CollectedGeneration{}, false, err
	}
	if err := s.observeScanStatus(ctx, boundary, apiRecorder.Snapshot(), envelopes, nil); err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	return collector.FactsFromSlice(scopeValue, generationValue, envelopes), true, nil
}

func (s ClaimedSource) expireStaleCheckpoints(ctx context.Context, boundary awscloud.Boundary) error {
	if s.Checkpoints == nil {
		return nil
	}
	_, err := s.Checkpoints.ExpireStale(ctx, checkpoint.ScopeFromBoundary(boundary))
	if err != nil {
		return fmt.Errorf("expire stale AWS pagination checkpoints: %w", err)
	}
	return nil
}

func (s ClaimedSource) acquireAccountSlot(ctx context.Context, target Target) (func(), error) {
	if s.Limiter == nil {
		return func() {}, nil
	}
	return s.Limiter.Acquire(ctx, target.AccountID)
}

func (s ClaimedSource) acquireCredentials(
	ctx context.Context,
	target Target,
	leaseExpiresAt time.Time,
) (CredentialLease, error) {
	if s.Tracer != nil {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanAWSCredentialsAssumeRole)
		span.SetAttributes(awsTargetAttributes(target)...)
		defer span.End()
	}
	return s.Credentials.Acquire(ctx, target, leaseExpiresAt)
}

func (s ClaimedSource) scanService(
	ctx context.Context,
	target Target,
	boundary awscloud.Boundary,
	scanner ServiceScanner,
) ([]facts.Envelope, error) {
	startedAt := time.Now()
	if s.Tracer != nil {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanAWSServiceScan)
		span.SetAttributes(awsTargetAttributes(target)...)
		defer span.End()
	}
	result := "success"
	defer func() {
		if s.Instruments != nil {
			s.Instruments.AWSScanDuration.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(
				telemetry.AttrService(target.ServiceKind),
				telemetry.AttrAccount(target.AccountID),
				telemetry.AttrRegion(target.Region),
				telemetry.AttrResult(result),
			))
		}
	}()
	envelopes, err := scanner.Scan(ctx, boundary)
	if err != nil {
		result = "error"
		return nil, err
	}
	s.recordEmissionCounts(ctx, target, envelopes)
	return envelopes, nil
}

func (s ClaimedSource) recordEmissionCounts(ctx context.Context, target Target, envelopes []facts.Envelope) {
	if s.Instruments == nil {
		return
	}
	relationshipCount := int64(0)
	tagObservationCount := int64(0)
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.AWSResourceFactKind:
			resourceType, _ := envelope.Payload["resource_type"].(string)
			s.Instruments.AWSResourcesEmitted.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrService(target.ServiceKind),
				telemetry.AttrAccount(target.AccountID),
				telemetry.AttrRegion(target.Region),
				telemetry.AttrResourceType(resourceType),
			))
		case facts.AWSRelationshipFactKind:
			relationshipCount++
		case facts.AWSTagObservationFactKind:
			tagObservationCount++
		}
	}
	if relationshipCount > 0 {
		s.Instruments.AWSRelationshipsEmitted.Add(ctx, relationshipCount, metric.WithAttributes(
			telemetry.AttrService(target.ServiceKind),
			telemetry.AttrAccount(target.AccountID),
			telemetry.AttrRegion(target.Region),
		))
	}
	if tagObservationCount > 0 {
		s.Instruments.AWSTagObservationsEmitted.Add(ctx, tagObservationCount, metric.WithAttributes(
			telemetry.AttrService(target.ServiceKind),
			telemetry.AttrAccount(target.AccountID),
			telemetry.AttrRegion(target.Region),
		))
	}
}

func (s ClaimedSource) recordAssumeRoleFailure(ctx context.Context, target Target) {
	if s.Instruments == nil {
		return
	}
	s.Instruments.AWSAssumeRoleFailed.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrAccount(target.AccountID),
	))
}

func (s ClaimedSource) validate(item workflow.WorkItem) error {
	if strings.TrimSpace(s.Config.CollectorInstanceID) == "" {
		return fmt.Errorf("aws collector instance id is required")
	}
	if item.CollectorKind != scope.CollectorAWS {
		return fmt.Errorf("claimed collector_kind %q must be %q", item.CollectorKind, scope.CollectorAWS)
	}
	if strings.TrimSpace(item.SourceSystem) != string(scope.CollectorAWS) {
		return fmt.Errorf("claimed source_system %q must be %q", item.SourceSystem, scope.CollectorAWS)
	}
	if item.CollectorInstanceID != s.Config.CollectorInstanceID {
		return fmt.Errorf("claimed collector_instance_id %q must be %q", item.CollectorInstanceID, s.Config.CollectorInstanceID)
	}
	if item.CurrentFencingToken <= 0 {
		return fmt.Errorf("claimed AWS fencing token must be positive")
	}
	if strings.TrimSpace(item.GenerationID) == "" || strings.TrimSpace(item.SourceRunID) == "" {
		return fmt.Errorf("claimed AWS generation identity is required")
	}
	if item.GenerationID != item.SourceRunID {
		return fmt.Errorf("claimed source_run_id %q must match generation_id %q", item.SourceRunID, item.GenerationID)
	}
	if s.Credentials == nil {
		return fmt.Errorf("aws credential provider is required")
	}
	if s.Scanners == nil {
		return fmt.Errorf("aws scanner factory is required")
	}
	return nil
}

func (s ClaimedSource) targetForClaim(item workflow.WorkItem) (Target, error) {
	claim, err := parseClaimTarget(item.AcceptanceUnitID)
	if err != nil {
		return Target{}, err
	}
	for _, candidate := range s.Config.Targets {
		if strings.TrimSpace(candidate.AccountID) != claim.AccountID {
			continue
		}
		if !containsString(candidate.AllowedRegions, claim.Region) {
			continue
		}
		if !containsString(candidate.AllowedServices, claim.ServiceKind) {
			continue
		}
		return Target{
			AccountID:   claim.AccountID,
			Region:      claim.Region,
			ServiceKind: claim.ServiceKind,
			Credentials: candidate.Credentials,
		}, nil
	}
	return Target{}, fmt.Errorf(
		"aws claim target account=%q region=%q service_kind=%q is not authorized for collector instance %q",
		claim.AccountID,
		claim.Region,
		claim.ServiceKind,
		s.Config.CollectorInstanceID,
	)
}

func parseClaimTarget(raw string) (claimTarget, error) {
	var decoded claimTarget
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &decoded); err != nil {
		return claimTarget{}, fmt.Errorf("decode AWS claim target: %w", err)
	}
	decoded.AccountID = strings.TrimSpace(decoded.AccountID)
	decoded.Region = strings.TrimSpace(decoded.Region)
	decoded.ServiceKind = strings.TrimSpace(decoded.ServiceKind)
	switch {
	case decoded.AccountID == "":
		return claimTarget{}, fmt.Errorf("aws claim target requires account_id")
	case decoded.Region == "":
		return claimTarget{}, fmt.Errorf("aws claim target requires region")
	case decoded.ServiceKind == "":
		return claimTarget{}, fmt.Errorf("aws claim target requires service_kind")
	default:
		return decoded, nil
	}
}

func (s ClaimedSource) boundary(item workflow.WorkItem, target Target) awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           target.AccountID,
		Region:              target.Region,
		ServiceKind:         target.ServiceKind,
		ScopeID:             item.ScopeID,
		GenerationID:        item.GenerationID,
		CollectorInstanceID: item.CollectorInstanceID,
		FencingToken:        item.CurrentFencingToken,
		ObservedAt:          s.now(),
	}
}

func (s ClaimedSource) scopeAndGeneration(
	item workflow.WorkItem,
	target Target,
) (scope.IngestionScope, scope.ScopeGeneration, error) {
	observedAt := s.now()
	scopeValue := scope.IngestionScope{
		ScopeID:       item.ScopeID,
		SourceSystem:  string(scope.CollectorAWS),
		ScopeKind:     scope.KindRegion,
		CollectorKind: scope.CollectorAWS,
		PartitionKey:  target.AccountID + ":" + target.Region + ":" + target.ServiceKind,
		Metadata: map[string]string{
			"account_id":   target.AccountID,
			"region":       target.Region,
			"service_kind": target.ServiceKind,
		},
	}
	generationValue := scope.ScopeGeneration{
		ScopeID:      item.ScopeID,
		GenerationID: item.GenerationID,
		Status:       scope.GenerationStatusPending,
		ObservedAt:   observedAt,
		IngestedAt:   observedAt,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	return scopeValue, generationValue, generationValue.ValidateForScope(scopeValue)
}

func (s ClaimedSource) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func containsString(values []string, candidate string) bool {
	return slices.ContainsFunc(values, func(value string) bool {
		return strings.TrimSpace(value) == candidate
	})
}

func awsTargetAttributes(target Target) []attribute.KeyValue {
	return []attribute.KeyValue{
		telemetry.AttrAccount(target.AccountID),
		telemetry.AttrRegion(target.Region),
		telemetry.AttrService(target.ServiceKind),
	}
}
