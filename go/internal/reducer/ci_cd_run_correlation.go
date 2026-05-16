package reducer

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// CICDRunCorrelationOutcome names the reducer decision for one CI/CD run.
type CICDRunCorrelationOutcome string

const (
	// CICDRunCorrelationExact means the run has an explicit artifact digest
	// that joins to exactly one reducer-owned container-image identity row.
	CICDRunCorrelationExact CICDRunCorrelationOutcome = "exact"
	// CICDRunCorrelationDerived means the run has bounded provider evidence,
	// but not enough artifact identity to claim a canonical target.
	CICDRunCorrelationDerived CICDRunCorrelationOutcome = "derived"
	// CICDRunCorrelationAmbiguous means the run's artifact evidence matches
	// multiple possible canonical targets.
	CICDRunCorrelationAmbiguous CICDRunCorrelationOutcome = "ambiguous"
	// CICDRunCorrelationUnresolved means the run is valid evidence but lacks
	// the repository/commit anchors required for correlation.
	CICDRunCorrelationUnresolved CICDRunCorrelationOutcome = "unresolved"
	// CICDRunCorrelationRejected means the run only offered unsafe evidence,
	// such as shell text that hints at deployment without an artifact anchor.
	CICDRunCorrelationRejected CICDRunCorrelationOutcome = "rejected"
)

// CICDRunCorrelationDecision records the bounded reducer decision for one run.
type CICDRunCorrelationDecision struct {
	Provider         string
	RunID            string
	RunAttempt       string
	RepositoryID     string
	CommitSHA        string
	Environment      string
	ArtifactDigest   string
	ImageRef         string
	Outcome          CICDRunCorrelationOutcome
	Reason           string
	ProvenanceOnly   bool
	CanonicalWrites  int
	EvidenceFactIDs  []string
	CanonicalTarget  string
	CorrelationKind  string
	SourceLayerKinds []string
}

// CICDRunCorrelationWrite carries decisions for durable publication.
type CICDRunCorrelationWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	Decisions    []CICDRunCorrelationDecision
}

// CICDRunCorrelationWriteResult summarizes durable CI/CD correlation writes.
type CICDRunCorrelationWriteResult struct {
	CanonicalWrites int
	FactsWritten    int
	EvidenceSummary string
}

// CICDRunCorrelationWriter persists reducer-owned CI/CD run correlations.
type CICDRunCorrelationWriter interface {
	WriteCICDRunCorrelations(context.Context, CICDRunCorrelationWrite) (CICDRunCorrelationWriteResult, error)
}

type activeCICDRunCorrelationFactLoader interface {
	ListActiveCICDRunCorrelationFacts(ctx context.Context, digests []string) ([]facts.Envelope, error)
}

// CICDRunCorrelationHandler joins CI/CD run facts with reducer-owned artifact
// identity evidence and publishes one durable decision per provider run.
type CICDRunCorrelationHandler struct {
	FactLoader  FactLoader
	Writer      CICDRunCorrelationWriter
	Instruments *telemetry.Instruments
}

// Handle executes one CI/CD run correlation reducer intent.
func (h CICDRunCorrelationHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainCICDRunCorrelation {
		return Result{}, fmt.Errorf("ci_cd_run_correlation handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("ci/cd run correlation fact loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("ci/cd run correlation writer is required")
	}

	envelopes, err := loadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, cicdRunCorrelationFactKinds())
	if err != nil {
		return Result{}, fmt.Errorf("load ci/cd run correlation facts: %w", err)
	}
	active, err := h.loadActiveCICDRunCorrelationFacts(ctx, ciArtifactDigests(envelopes))
	if err != nil {
		return Result{}, fmt.Errorf("load active ci/cd artifact identity facts: %w", err)
	}
	envelopes = append(envelopes, active...)

	decisions := BuildCICDRunCorrelationDecisions(envelopes)
	counts := cicdRunCorrelationCounts(decisions)
	writeResult, err := h.Writer.WriteCICDRunCorrelations(ctx, CICDRunCorrelationWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
		Cause:        intent.Cause,
		Decisions:    decisions,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write ci/cd run correlations: %w", err)
	}
	h.emitCounters(ctx, counts)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainCICDRunCorrelation,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: cicdRunCorrelationSummary(len(decisions), counts, writeResult.CanonicalWrites),
		CanonicalWrites: writeResult.CanonicalWrites,
	}, nil
}

func (h CICDRunCorrelationHandler) loadActiveCICDRunCorrelationFacts(
	ctx context.Context,
	digests []string,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activeCICDRunCorrelationFactLoader)
	if !ok {
		return nil, nil
	}
	envelopes, err := loader.ListActiveCICDRunCorrelationFacts(ctx, digests)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return envelopes, nil
}

func (h CICDRunCorrelationHandler) emitCounters(ctx context.Context, counts map[CICDRunCorrelationOutcome]int) {
	if h.Instruments == nil {
		return
	}
	for _, outcome := range cicdRunCorrelationOutcomes() {
		if counts[outcome] == 0 {
			continue
		}
		h.Instruments.CICDRunCorrelations.Add(ctx, int64(counts[outcome]), metric.WithAttributes(
			telemetry.AttrDomain(string(DomainCICDRunCorrelation)),
			telemetry.AttrOutcome(string(outcome)),
		))
	}
}

// BuildCICDRunCorrelationDecisions classifies provider runs without turning
// CI success or shell text into deployment truth.
func BuildCICDRunCorrelationDecisions(envelopes []facts.Envelope) []CICDRunCorrelationDecision {
	runs := map[string]*cicdRunEvidence{}
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.CICDRunFactKind:
			key := cicdRunKey(envelope.Payload)
			if key == "" {
				continue
			}
			ev := ensureCICDRunEvidence(runs, key)
			ev.run = envelope
		case facts.CICDArtifactFactKind:
			if key := cicdRunKey(envelope.Payload); key != "" {
				ev := ensureCICDRunEvidence(runs, key)
				ev.artifacts = append(ev.artifacts, envelope)
			}
		case facts.CICDEnvironmentObservationFactKind:
			if key := cicdRunKey(envelope.Payload); key != "" {
				ev := ensureCICDRunEvidence(runs, key)
				ev.environments = append(ev.environments, envelope)
			}
		case facts.CICDTriggerEdgeFactKind:
			if key := cicdRunKey(envelope.Payload); key != "" {
				ev := ensureCICDRunEvidence(runs, key)
				ev.triggers = append(ev.triggers, envelope)
			}
		case facts.CICDStepFactKind:
			if key := cicdRunKey(envelope.Payload); key != "" && payloadString(envelope.Payload, "deployment_hint_source") == "shell" {
				ev := ensureCICDRunEvidence(runs, key)
				ev.shellOnly = append(ev.shellOnly, envelope)
			}
		}
	}
	imageIndex := buildCICDImageIdentityIndex(envelopes)
	decisions := make([]CICDRunCorrelationDecision, 0, len(runs))
	for _, ev := range runs {
		if ev.run.FactID == "" {
			continue
		}
		decisions = append(decisions, classifyCICDRunEvidence(ev, imageIndex))
	}
	sort.SliceStable(decisions, func(i, j int) bool {
		return decisions[i].Provider+decisions[i].RunID < decisions[j].Provider+decisions[j].RunID
	})
	return decisions
}

func cicdRunCorrelationFactKinds() []string {
	return []string{
		facts.CICDRunFactKind,
		facts.CICDArtifactFactKind,
		facts.CICDEnvironmentObservationFactKind,
		facts.CICDTriggerEdgeFactKind,
		facts.CICDStepFactKind,
	}
}

func cicdRunCorrelationOutcomes() []CICDRunCorrelationOutcome {
	return []CICDRunCorrelationOutcome{
		CICDRunCorrelationExact,
		CICDRunCorrelationDerived,
		CICDRunCorrelationAmbiguous,
		CICDRunCorrelationUnresolved,
		CICDRunCorrelationRejected,
	}
}

func cicdRunCorrelationCounts(decisions []CICDRunCorrelationDecision) map[CICDRunCorrelationOutcome]int {
	counts := make(map[CICDRunCorrelationOutcome]int, len(cicdRunCorrelationOutcomes()))
	for _, decision := range decisions {
		counts[decision.Outcome]++
	}
	return counts
}

func cicdRunCorrelationSummary(evaluated int, counts map[CICDRunCorrelationOutcome]int, canonicalWrites int) string {
	return fmt.Sprintf(
		"ci/cd run correlation evaluated=%d exact=%d derived=%d ambiguous=%d unresolved=%d rejected=%d canonical_writes=%d",
		evaluated,
		counts[CICDRunCorrelationExact],
		counts[CICDRunCorrelationDerived],
		counts[CICDRunCorrelationAmbiguous],
		counts[CICDRunCorrelationUnresolved],
		counts[CICDRunCorrelationRejected],
		canonicalWrites,
	)
}

func cicdRunCorrelationCanonicalWrites(decisions []CICDRunCorrelationDecision) int {
	total := 0
	for _, decision := range decisions {
		total += decision.CanonicalWrites
	}
	return total
}

func ciArtifactDigests(envelopes []facts.Envelope) []string {
	var digests []string
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.CICDArtifactFactKind {
			continue
		}
		digests = append(digests, payloadString(envelope.Payload, "artifact_digest"))
	}
	return uniqueSortedStrings(digests)
}

type cicdRunEvidence struct {
	run          facts.Envelope
	artifacts    []facts.Envelope
	environments []facts.Envelope
	triggers     []facts.Envelope
	shellOnly    []facts.Envelope
}

func ensureCICDRunEvidence(runs map[string]*cicdRunEvidence, key string) *cicdRunEvidence {
	if runs[key] == nil {
		runs[key] = &cicdRunEvidence{}
	}
	return runs[key]
}

type cicdImageIdentity struct {
	factID       string
	repositoryID string
	imageRef     string
	digest       string
}

func buildCICDImageIdentityIndex(envelopes []facts.Envelope) map[string][]cicdImageIdentity {
	index := map[string][]cicdImageIdentity{}
	for _, envelope := range envelopes {
		if envelope.FactKind != containerImageIdentityFactKind {
			continue
		}
		digest := payloadString(envelope.Payload, "digest")
		if digest == "" {
			continue
		}
		index[digest] = append(index[digest], cicdImageIdentity{
			factID:       envelope.FactID,
			repositoryID: payloadString(envelope.Payload, "repository_id"),
			imageRef:     payloadString(envelope.Payload, "image_ref"),
			digest:       digest,
		})
	}
	return index
}

func classifyCICDRunEvidence(ev *cicdRunEvidence, imageIndex map[string][]cicdImageIdentity) CICDRunCorrelationDecision {
	run := ev.run.Payload
	decision := CICDRunCorrelationDecision{
		Provider:         payloadString(run, "provider"),
		RunID:            payloadString(run, "run_id"),
		RunAttempt:       defaultCICDRunAttempt(payloadString(run, "run_attempt")),
		RepositoryID:     payloadString(run, "repository_id"),
		CommitSHA:        payloadString(run, "commit_sha"),
		Outcome:          CICDRunCorrelationDerived,
		Reason:           "run has provider evidence but no explicit artifact identity anchor",
		ProvenanceOnly:   true,
		CorrelationKind:  "run_evidence",
		SourceLayerKinds: []string{"reported"},
		EvidenceFactIDs:  []string{ev.run.FactID},
	}
	if len(ev.environments) > 0 {
		decision.Environment = payloadString(ev.environments[0].Payload, "environment")
		decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, ev.environments[0].FactID)
	}
	for _, trigger := range ev.triggers {
		decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, trigger.FactID)
	}
	if decision.RepositoryID == "" || decision.CommitSHA == "" {
		decision.Outcome = CICDRunCorrelationUnresolved
		decision.Reason = "run is missing repository_id or commit_sha anchor"
		return decision
	}
	if len(ev.shellOnly) > 0 {
		decision.Outcome = CICDRunCorrelationRejected
		decision.Reason = "shell-only deployment hint suppressed without artifact identity"
		decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, ev.shellOnly[0].FactID)
		return decision
	}
	for _, artifact := range ev.artifacts {
		digest := payloadString(artifact.Payload, "artifact_digest")
		if digest == "" {
			continue
		}
		decision.ArtifactDigest = digest
		decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, artifact.FactID)
		matches := imageIndex[digest]
		if repoMatches := cicdImageMatchesForRepository(matches, decision.RepositoryID); len(repoMatches) > 0 {
			matches = repoMatches
		}
		switch len(matches) {
		case 0:
			continue
		case 1:
			decision.Outcome = CICDRunCorrelationExact
			decision.Reason = "artifact digest matches one container image identity row"
			decision.ProvenanceOnly = false
			decision.CanonicalWrites = 1
			decision.CanonicalTarget = "container_image"
			decision.CorrelationKind = "artifact_image"
			decision.ImageRef = matches[0].imageRef
			decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, matches[0].factID)
			decision.SourceLayerKinds = []string{"reported", "observed_resource"}
			return decision
		default:
			decision.Outcome = CICDRunCorrelationAmbiguous
			decision.Reason = "artifact digest matches multiple container image identity rows"
			decision.CorrelationKind = "artifact_image"
			for _, match := range matches {
				decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, match.factID)
			}
			return decision
		}
	}
	return decision
}

func cicdImageMatchesForRepository(matches []cicdImageIdentity, repositoryID string) []cicdImageIdentity {
	if repositoryID == "" {
		return nil
	}
	out := make([]cicdImageIdentity, 0, len(matches))
	for _, match := range matches {
		if match.repositoryID == repositoryID {
			out = append(out, match)
		}
	}
	return out
}

func cicdRunKey(payload map[string]any) string {
	provider := payloadString(payload, "provider")
	runID := payloadString(payload, "run_id")
	if provider == "" || runID == "" {
		return ""
	}
	return provider + ":" + runID + ":" + defaultCICDRunAttempt(payloadString(payload, "run_attempt"))
}

func defaultCICDRunAttempt(attempt string) string {
	if strings.TrimSpace(attempt) == "" {
		return "1"
	}
	return strings.TrimSpace(attempt)
}
