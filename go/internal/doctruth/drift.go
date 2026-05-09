package doctruth

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/metric"
)

const (
	// ClaimTypeServiceDeployment identifies claims about how a service is deployed.
	ClaimTypeServiceDeployment = "service_deployment"
	// FindingTypeServiceDeploymentDrift identifies service deployment documentation drift findings.
	FindingTypeServiceDeploymentDrift FindingType = "service_deployment_drift"
)

// FindingType identifies a documentation truth finding family.
type FindingType string

// FindingStatus describes the comparable state of one documentation claim.
type FindingStatus string

const (
	// FindingStatusMatch means the documented deployment target is present in current truth.
	FindingStatusMatch FindingStatus = "match"
	// FindingStatusConflict means the documented deployment target conflicts with current truth.
	FindingStatusConflict FindingStatus = "conflict"
	// FindingStatusAmbiguous means Eshu cannot choose one current deployment truth.
	FindingStatusAmbiguous FindingStatus = "ambiguous"
	// FindingStatusUnsupported means the claim cannot be compared by this finding family.
	FindingStatusUnsupported FindingStatus = "unsupported"
	// FindingStatusStale means current truth exists but is stale.
	FindingStatusStale FindingStatus = "stale"
	// FindingStatusBuilding means current truth is still being built.
	FindingStatusBuilding FindingStatus = "building"
)

// TruthLevel is the item-level truth label for a documentation finding.
type TruthLevel string

const (
	// TruthLevelExact means the finding is backed by fresh comparable truth.
	TruthLevelExact TruthLevel = "exact"
	// TruthLevelDerived means the finding is bounded but not exact.
	TruthLevelDerived TruthLevel = "derived"
)

// FreshnessState describes the current graph truth freshness used by a finding.
type FreshnessState string

const (
	// FreshnessFresh means the supplied graph truth is current.
	FreshnessFresh FreshnessState = "fresh"
	// FreshnessStale means the supplied graph truth may lag source truth.
	FreshnessStale FreshnessState = "stale"
	// FreshnessBuilding means the supplied graph truth is not ready yet.
	FreshnessBuilding FreshnessState = "building"
	// FreshnessUnavailable means the supplied graph truth is unavailable.
	FreshnessUnavailable FreshnessState = "unavailable"
)

const (
	unsupportedClaimType             = "unsupported_claim_type"
	unsupportedMissingGraphTruth     = "missing_graph_truth"
	unsupportedMissingDocumentedPath = "missing_documented_deployment"
	unsupportedMissingServiceID      = "missing_service_identity"
	unsupportedSubjectMismatch       = "subject_truth_mismatch"
	ambiguousSubjectMention          = "ambiguous_subject_mention"
	ambiguousDeploymentMention       = "ambiguous_deployment_mention"
)

// ServiceDeploymentTruth is the caller-supplied current Eshu truth for one service.
type ServiceDeploymentTruth struct {
	ServiceID         string
	DeploymentRefs    []facts.DocumentationEvidenceRef
	EvidenceRefs      []facts.DocumentationEvidenceRef
	FreshnessState    FreshnessState
	ObservedAt        time.Time
	AmbiguityReasons  []string
	UnsupportedReason string
}

// DeploymentDriftInput contains one documentation claim and its comparable truth.
type DeploymentDriftInput struct {
	SourceSystem string
	Claim        facts.DocumentationClaimCandidatePayload
	Mentions     []facts.DocumentationEntityMentionPayload
	Truth        ServiceDeploymentTruth
}

// DeploymentDriftFinding is a read-only finding for a documentation deployment claim.
type DeploymentDriftFinding struct {
	FindingID               string
	FindingType             FindingType
	Status                  FindingStatus
	TruthLevel              TruthLevel
	FreshnessState          FreshnessState
	DocumentID              string
	RevisionID              string
	SectionID               string
	ClaimID                 string
	ClaimText               string
	ClaimHash               string
	ExcerptHash             string
	ServiceID               string
	SubjectMentionID        string
	DocumentedDeploymentIDs []string
	CurrentDeploymentIDs    []string
	EvidenceRefs            []facts.DocumentationEvidenceRef
	AmbiguityReasons        []string
	UnsupportedReason       string
	ObservedAt              time.Time
}

// DriftOptions configures optional drift-finding dependencies.
type DriftOptions struct {
	Instruments *telemetry.Instruments
	Logger      *slog.Logger
}

// DeploymentDriftAnalyzer compares documentation deployment claims with supplied Eshu truth.
type DeploymentDriftAnalyzer struct {
	instruments *telemetry.Instruments
	logger      *slog.Logger
}

type driftReport struct {
	Matches     int
	Conflicts   int
	Ambiguous   int
	Unsupported int
	Stale       int
	Building    int
}

// NewDeploymentDriftAnalyzer constructs a deployment drift analyzer.
func NewDeploymentDriftAnalyzer(options DriftOptions) *DeploymentDriftAnalyzer {
	return &DeploymentDriftAnalyzer{
		instruments: options.Instruments,
		logger:      options.Logger,
	}
}

// FindServiceDeploymentDrift compares service_deployment claims against current deployment truth.
func (a *DeploymentDriftAnalyzer) FindServiceDeploymentDrift(ctx context.Context, inputs []DeploymentDriftInput) []DeploymentDriftFinding {
	startedAt := time.Now()
	findings := make([]DeploymentDriftFinding, 0, len(inputs))
	report := driftReport{}
	for _, input := range inputs {
		finding := a.findServiceDeploymentDrift(input)
		findings = append(findings, finding)
		report.add(finding.Status)
		a.recordFinding(ctx, input.SourceSystem, finding.Status)
	}
	a.recordDuration(ctx, startedAt, inputs, report)
	a.logCompletion(ctx, report)
	return findings
}

func (a *DeploymentDriftAnalyzer) findServiceDeploymentDrift(input DeploymentDriftInput) DeploymentDriftFinding {
	claim := input.Claim
	finding := DeploymentDriftFinding{
		FindingID:        deploymentDriftFindingID(claim),
		FindingType:      FindingTypeServiceDeploymentDrift,
		Status:           FindingStatusMatch,
		TruthLevel:       TruthLevelExact,
		FreshnessState:   normalizeFreshness(input.Truth.FreshnessState),
		DocumentID:       claim.DocumentID,
		RevisionID:       claim.RevisionID,
		SectionID:        claim.SectionID,
		ClaimID:          claim.ClaimID,
		ClaimText:        claim.ClaimText,
		ClaimHash:        claim.ClaimHash,
		ExcerptHash:      claim.ExcerptHash,
		ServiceID:        strings.TrimSpace(input.Truth.ServiceID),
		SubjectMentionID: claim.SubjectMentionID,
		EvidenceRefs:     append([]facts.DocumentationEvidenceRef{}, claim.EvidenceRefs...),
		ObservedAt:       input.Truth.ObservedAt,
	}
	finding.EvidenceRefs = appendRefs(finding.EvidenceRefs, input.Truth.EvidenceRefs)

	mentions := mentionsByID(input.Mentions)
	if claim.ClaimType != ClaimTypeServiceDeployment {
		return markUnsupported(finding, unsupportedClaimType)
	}
	subjectMention := mentions[claim.SubjectMentionID]
	if subjectMention.ResolutionStatus == facts.DocumentationMentionResolutionAmbiguous {
		finding.AmbiguityReasons = append(finding.AmbiguityReasons, ambiguousSubjectMention)
		return markAmbiguous(finding)
	}
	if subjectMention.ResolutionStatus != facts.DocumentationMentionResolutionExact {
		return markUnsupported(finding, unsupportedMissingServiceID)
	}
	serviceID := exactMentionEntityID(subjectMention)
	if serviceID != "" {
		finding.ServiceID = serviceID
	}
	if finding.ServiceID == "" {
		return markUnsupported(finding, unsupportedMissingServiceID)
	}
	if input.Truth.ServiceID != "" && serviceID != "" && serviceID != input.Truth.ServiceID {
		return markUnsupported(finding, unsupportedSubjectMismatch)
	}

	var ambiguousMentions []string
	finding.DocumentedDeploymentIDs, ambiguousMentions = documentedDeploymentIDs(claim, mentions)
	finding.CurrentDeploymentIDs = evidenceRefIDs(input.Truth.DeploymentRefs)
	finding.AmbiguityReasons = uniqueSortedStrings(append(finding.AmbiguityReasons, input.Truth.AmbiguityReasons...))
	finding.AmbiguityReasons = uniqueSortedStrings(append(finding.AmbiguityReasons, ambiguousMentions...))
	if input.Truth.UnsupportedReason != "" {
		return markUnsupported(finding, input.Truth.UnsupportedReason)
	}
	if len(finding.CurrentDeploymentIDs) == 0 {
		return markUnsupported(finding, unsupportedMissingGraphTruth)
	}
	if len(finding.AmbiguityReasons) > 0 {
		return markAmbiguous(finding)
	}
	if len(finding.DocumentedDeploymentIDs) == 0 {
		return markUnsupported(finding, unsupportedMissingDocumentedPath)
	}
	switch finding.FreshnessState {
	case FreshnessStale:
		finding.Status = FindingStatusStale
		finding.TruthLevel = TruthLevelDerived
		return finding
	case FreshnessBuilding:
		finding.Status = FindingStatusBuilding
		finding.TruthLevel = TruthLevelDerived
		return finding
	case FreshnessUnavailable:
		return markUnsupported(finding, unsupportedMissingGraphTruth)
	}
	if !documentedDeploymentsMatchTruth(finding.DocumentedDeploymentIDs, finding.CurrentDeploymentIDs) {
		finding.Status = FindingStatusConflict
	}
	return finding
}

func markUnsupported(finding DeploymentDriftFinding, reason string) DeploymentDriftFinding {
	finding.Status = FindingStatusUnsupported
	finding.TruthLevel = TruthLevelDerived
	finding.UnsupportedReason = reason
	return finding
}

func markAmbiguous(finding DeploymentDriftFinding) DeploymentDriftFinding {
	finding.Status = FindingStatusAmbiguous
	finding.TruthLevel = TruthLevelDerived
	finding.AmbiguityReasons = uniqueSortedStrings(finding.AmbiguityReasons)
	return finding
}

func deploymentDriftFindingID(claim facts.DocumentationClaimCandidatePayload) string {
	return "finding:" + facts.StableID(string(FindingTypeServiceDeploymentDrift), map[string]any{
		"document_id":  claim.DocumentID,
		"revision_id":  claim.RevisionID,
		"section_id":   claim.SectionID,
		"claim_id":     claim.ClaimID,
		"claim_hash":   claim.ClaimHash,
		"excerpt_hash": claim.ExcerptHash,
	})
}

func mentionsByID(mentions []facts.DocumentationEntityMentionPayload) map[string]facts.DocumentationEntityMentionPayload {
	out := make(map[string]facts.DocumentationEntityMentionPayload, len(mentions))
	for _, mention := range mentions {
		if strings.TrimSpace(mention.MentionID) == "" {
			continue
		}
		out[mention.MentionID] = mention
	}
	return out
}

func exactMentionEntityID(mention facts.DocumentationEntityMentionPayload) string {
	if mention.ResolutionStatus != facts.DocumentationMentionResolutionExact || len(mention.CandidateRefs) != 1 {
		return ""
	}
	return strings.TrimSpace(mention.CandidateRefs[0].ID)
}

func documentedDeploymentIDs(
	claim facts.DocumentationClaimCandidatePayload,
	mentions map[string]facts.DocumentationEntityMentionPayload,
) ([]string, []string) {
	ids := make([]string, 0, len(claim.ObjectMentionIDs))
	ambiguityReasons := []string{}
	for _, mentionID := range claim.ObjectMentionIDs {
		mention := mentions[mentionID]
		if mention.ResolutionStatus == facts.DocumentationMentionResolutionAmbiguous {
			ambiguityReasons = append(ambiguityReasons, ambiguousDeploymentMention)
			continue
		}
		id := exactMentionEntityID(mention)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	return uniqueSortedStrings(ids), uniqueSortedStrings(ambiguityReasons)
}

func evidenceRefIDs(refs []facts.DocumentationEvidenceRef) []string {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref.ID) == "" {
			continue
		}
		ids = append(ids, ref.ID)
	}
	return uniqueSortedStrings(ids)
}

func documentedDeploymentsMatchTruth(documentedIDs []string, currentIDs []string) bool {
	current := make(map[string]struct{}, len(currentIDs))
	for _, id := range currentIDs {
		current[id] = struct{}{}
	}
	for _, id := range documentedIDs {
		if _, ok := current[id]; !ok {
			return false
		}
	}
	return true
}

func normalizeFreshness(freshness FreshnessState) FreshnessState {
	if freshness == "" {
		return FreshnessFresh
	}
	return freshness
}

func uniqueSortedStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func (r *driftReport) add(status FindingStatus) {
	switch status {
	case FindingStatusMatch:
		r.Matches++
	case FindingStatusConflict:
		r.Conflicts++
	case FindingStatusAmbiguous:
		r.Ambiguous++
	case FindingStatusUnsupported:
		r.Unsupported++
	case FindingStatusStale:
		r.Stale++
	case FindingStatusBuilding:
		r.Building++
	}
}

func (a *DeploymentDriftAnalyzer) recordFinding(ctx context.Context, sourceSystem string, status FindingStatus) {
	if a.instruments == nil {
		return
	}
	a.instruments.DocumentationDriftFindings.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrSourceSystem(sourceSystem),
		telemetry.AttrOutcome(string(status)),
	))
}

func (a *DeploymentDriftAnalyzer) recordDuration(ctx context.Context, startedAt time.Time, inputs []DeploymentDriftInput, report driftReport) {
	if a.instruments == nil {
		return
	}
	if len(inputs) == 0 {
		return
	}
	outcome := "none"
	if report.Conflicts > 0 {
		outcome = string(FindingStatusConflict)
	} else if report.Unsupported > 0 {
		outcome = string(FindingStatusUnsupported)
	} else if report.Ambiguous > 0 {
		outcome = string(FindingStatusAmbiguous)
	} else if report.Stale > 0 {
		outcome = string(FindingStatusStale)
	} else if report.Building > 0 {
		outcome = string(FindingStatusBuilding)
	} else if report.Matches > 0 {
		outcome = string(FindingStatusMatch)
	}
	a.instruments.DocumentationDriftGenerationDuration.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(
		telemetry.AttrSourceSystem(driftSourceSystem(inputs)),
		telemetry.AttrOutcome(outcome),
	))
}

func driftSourceSystem(inputs []DeploymentDriftInput) string {
	out := ""
	for _, input := range inputs {
		sourceSystem := strings.TrimSpace(input.SourceSystem)
		if sourceSystem == "" {
			continue
		}
		if out == "" {
			out = sourceSystem
			continue
		}
		if out != sourceSystem {
			return "mixed"
		}
	}
	if out == "" {
		return "unknown"
	}
	return out
}

func (a *DeploymentDriftAnalyzer) logCompletion(ctx context.Context, report driftReport) {
	if a.logger == nil {
		return
	}
	a.logger.InfoContext(ctx,
		"documentation drift generation completed",
		telemetry.EventAttr("documentation.drift.completed"),
		telemetry.PhaseAttr(telemetry.PhaseProjection),
		slog.String("finding_type", string(FindingTypeServiceDeploymentDrift)),
		slog.Int("matches", report.Matches),
		slog.Int("conflicts", report.Conflicts),
		slog.Int("ambiguous", report.Ambiguous),
		slog.Int("unsupported", report.Unsupported),
		slog.Int("stale", report.Stale),
		slog.Int("building", report.Building),
	)
}
