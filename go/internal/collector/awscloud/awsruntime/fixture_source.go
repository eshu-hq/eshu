package awsruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// FixtureSource implements collector.Source for fully offline AWS cloud
// replay. Each Next call yields one CollectedGeneration for the next configured
// scope by converting the declarative FixtureResource and FixtureRelationship
// entries into the same aws_resource and aws_relationship envelopes the live
// scanners emit, then returning ok=false when the batch is exhausted so the
// collector.Service poll loop waits for the next poll and restarts the batch.
//
// FixtureSource performs no AWS API calls, requires no credentials, and uses no
// AWS SDK types. It is single-goroutine per collector.Service; it is not safe
// for concurrent Next calls.
type FixtureSource struct {
	// Config is the declarative offline estate. It is required.
	Config FixtureConfig
	// Clock supplies the observation time stamped on emitted facts. Optional;
	// nil uses time.Now. The derived generation id never depends on the clock so
	// re-ingest stays idempotent and CI stays reproducible.
	Clock func() time.Time

	scopeIndex int
	drained    bool
}

// FixtureConfig carries the offline AWS estate one FixtureSource replays. It is
// the runtime form of the declarative fixture document; the command-side loader
// maps the JSON file onto it.
type FixtureConfig struct {
	// CollectorInstanceID is the configured runtime instance id stamped on every
	// emitted fact's boundary. Required.
	CollectorInstanceID string
	// Scopes declares the bounded AWS scopes to replay, one per Next call.
	Scopes []FixtureScope
}

// FixtureScope is one offline `(account, region, service)` estate slice plus the
// resources and relationships replayed for it.
type FixtureScope struct {
	// AccountID is the 12-digit AWS account id. Required.
	AccountID string
	// Region is the AWS region. Required.
	Region string
	// ServiceKind is the bounded AWS service family (for example s3). Required.
	ServiceKind string
	// ScopeID optionally overrides the derived `aws:<account>:<region>:<service>`
	// scope id. When empty the derived id is used so the replay scope identity
	// matches the live collector's identity.
	ScopeID string
	// GenerationID optionally pins the generation id. When empty a deterministic
	// id is derived from the scope id so re-ingest is idempotent.
	GenerationID string
	// FencingToken fences the scope's generation. Values <= 0 default to 1.
	FencingToken int64
	// Resources are the aws_resource observations replayed for this scope.
	Resources []FixtureResource
	// Relationships are the aws_relationship observations replayed for this scope.
	Relationships []FixtureRelationship
}

// FixtureResource mirrors awscloud.ResourceObservation minus the per-scope
// Boundary, which FixtureSource supplies from the resolved scope identity.
type FixtureResource struct {
	ARN                string            `json:"arn"`
	ResourceID         string            `json:"resource_id"`
	ResourceType       string            `json:"resource_type"`
	Name               string            `json:"name"`
	State              string            `json:"state"`
	Tags               map[string]string `json:"tags"`
	Attributes         map[string]any    `json:"attributes"`
	CorrelationAnchors []string          `json:"correlation_anchors"`
	SourceURI          string            `json:"source_uri"`
	SourceRecordID     string            `json:"source_record_id"`
}

// FixtureRelationship mirrors awscloud.RelationshipObservation minus the
// per-scope Boundary, which FixtureSource supplies from the resolved scope
// identity.
type FixtureRelationship struct {
	RelationshipType string         `json:"relationship_type"`
	SourceResourceID string         `json:"source_resource_id"`
	SourceARN        string         `json:"source_arn"`
	TargetResourceID string         `json:"target_resource_id"`
	TargetARN        string         `json:"target_arn"`
	TargetType       string         `json:"target_type"`
	Attributes       map[string]any `json:"attributes"`
	SourceURI        string         `json:"source_uri"`
	SourceRecordID   string         `json:"source_record_id"`
}

// Validate reports the first structural problem in the offline config so an
// empty or malformed estate fails fast instead of silently emitting no facts.
func (c FixtureConfig) Validate() error {
	if strings.TrimSpace(c.CollectorInstanceID) == "" {
		return errors.New("aws fixture config requires collector_instance_id")
	}
	if len(c.Scopes) == 0 {
		return errors.New("aws fixture config requires at least one scope")
	}
	for i := range c.Scopes {
		if err := c.Scopes[i].validate(); err != nil {
			return fmt.Errorf("aws fixture scope %d: %w", i, err)
		}
	}
	return nil
}

func (s FixtureScope) validate() error {
	switch {
	case strings.TrimSpace(s.AccountID) == "":
		return errors.New("requires account_id")
	case strings.TrimSpace(s.Region) == "":
		return errors.New("requires region")
	case strings.TrimSpace(s.ServiceKind) == "":
		return errors.New("requires service_kind")
	case len(s.Resources) == 0 && len(s.Relationships) == 0:
		return errors.New("requires at least one resource or relationship")
	default:
		return nil
	}
}

// resolvedScopeID returns the configured scope id or the derived
// `aws:<account>:<region>:<service>` identity used by the live collector.
func (s FixtureScope) resolvedScopeID() string {
	if id := strings.TrimSpace(s.ScopeID); id != "" {
		return id
	}
	return strings.Join([]string{
		"aws",
		strings.TrimSpace(s.AccountID),
		strings.TrimSpace(s.Region),
		strings.TrimSpace(s.ServiceKind),
	}, ":")
}

// resolvedGenerationID returns the configured generation id or a deterministic
// id derived only from the resolved scope id. The clock never participates so
// two fresh sources with identical config derive the same generation id.
func (s FixtureScope) resolvedGenerationID() string {
	if id := strings.TrimSpace(s.GenerationID); id != "" {
		return id
	}
	return facts.StableID("AWSFixtureGeneration", map[string]any{
		"scope_id": s.resolvedScopeID(),
	})
}

func (s FixtureScope) resolvedFencingToken() int64 {
	if s.FencingToken <= 0 {
		return 1
	}
	return s.FencingToken
}

// Next emits the next configured scope generation. It returns ok=false when the
// configured scope batch is exhausted so collector.Service can wait for the
// next poll, then restarts the batch on the following poll.
func (s *FixtureSource) Next(_ context.Context) (collector.CollectedGeneration, bool, error) {
	if err := s.Config.Validate(); err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	if s.drained {
		s.drained = false
		s.scopeIndex = 0
		return collector.CollectedGeneration{}, false, nil
	}
	if s.scopeIndex >= len(s.Config.Scopes) {
		s.drained = true
		return collector.CollectedGeneration{}, false, nil
	}

	scopeCfg := s.Config.Scopes[s.scopeIndex]
	s.scopeIndex++
	if s.scopeIndex >= len(s.Config.Scopes) {
		s.drained = true
	}

	collected, err := s.collectScope(scopeCfg)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	return collected, true, nil
}

// collectScope builds the deterministic envelope set for one offline scope.
func (s *FixtureSource) collectScope(scopeCfg FixtureScope) (collector.CollectedGeneration, error) {
	observedAt := s.now()
	boundary := awscloud.Boundary{
		AccountID:           strings.TrimSpace(scopeCfg.AccountID),
		Region:              strings.TrimSpace(scopeCfg.Region),
		ServiceKind:         strings.TrimSpace(scopeCfg.ServiceKind),
		ScopeID:             scopeCfg.resolvedScopeID(),
		GenerationID:        scopeCfg.resolvedGenerationID(),
		CollectorInstanceID: strings.TrimSpace(s.Config.CollectorInstanceID),
		FencingToken:        scopeCfg.resolvedFencingToken(),
		ObservedAt:          observedAt,
	}

	envelopes := make([]facts.Envelope, 0, len(scopeCfg.Resources)+len(scopeCfg.Relationships))
	for i := range scopeCfg.Resources {
		envelope, err := awscloud.NewResourceEnvelope(scopeCfg.Resources[i].observation(boundary))
		if err != nil {
			return collector.CollectedGeneration{}, fmt.Errorf("build aws fixture resource %d for scope %q: %w", i, boundary.ScopeID, err)
		}
		envelopes = append(envelopes, envelope)
	}
	for i := range scopeCfg.Relationships {
		envelope, err := awscloud.NewRelationshipEnvelope(scopeCfg.Relationships[i].observation(boundary))
		if err != nil {
			return collector.CollectedGeneration{}, fmt.Errorf("build aws fixture relationship %d for scope %q: %w", i, boundary.ScopeID, err)
		}
		envelopes = append(envelopes, envelope)
	}

	scopeValue, generationValue := s.scopeAndGeneration(boundary, observedAt)
	return collector.FactsFromSlice(scopeValue, generationValue, envelopes), nil
}

func (r FixtureResource) observation(boundary awscloud.Boundary) awscloud.ResourceObservation {
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                r.ARN,
		ResourceID:         r.ResourceID,
		ResourceType:       r.ResourceType,
		Name:               r.Name,
		State:              r.State,
		Tags:               r.Tags,
		Attributes:         r.Attributes,
		CorrelationAnchors: r.CorrelationAnchors,
		SourceURI:          r.SourceURI,
		SourceRecordID:     r.SourceRecordID,
	}
}

func (r FixtureRelationship) observation(boundary awscloud.Boundary) awscloud.RelationshipObservation {
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: r.RelationshipType,
		SourceResourceID: r.SourceResourceID,
		SourceARN:        r.SourceARN,
		TargetResourceID: r.TargetResourceID,
		TargetARN:        r.TargetARN,
		TargetType:       r.TargetType,
		Attributes:       r.Attributes,
		SourceURI:        r.SourceURI,
		SourceRecordID:   r.SourceRecordID,
	}
}

func (s *FixtureSource) scopeAndGeneration(
	boundary awscloud.Boundary,
	observedAt time.Time,
) (scope.IngestionScope, scope.ScopeGeneration) {
	scopeValue := scope.IngestionScope{
		ScopeID:       boundary.ScopeID,
		SourceSystem:  awscloud.CollectorKind,
		ScopeKind:     scope.KindRegion,
		CollectorKind: scope.CollectorAWS,
		PartitionKey:  boundary.ScopeID,
		Metadata: map[string]string{
			"collector_instance_id": boundary.CollectorInstanceID,
			"account_id":            boundary.AccountID,
			"region":                boundary.Region,
			"service_kind":          boundary.ServiceKind,
		},
	}
	generationValue := scope.ScopeGeneration{
		GenerationID: boundary.GenerationID,
		ScopeID:      boundary.ScopeID,
		ObservedAt:   observedAt,
		IngestedAt:   observedAt,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	return scopeValue, generationValue
}

func (s *FixtureSource) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}
