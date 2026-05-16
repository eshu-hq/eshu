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

// SBOMAttachmentStatus names the reducer decision for one SBOM or attestation
// document attachment.
type SBOMAttachmentStatus string

const (
	// SBOMAttachmentAttachedVerified means subject digest matched and
	// verification passed under a named or reported policy.
	SBOMAttachmentAttachedVerified SBOMAttachmentStatus = "attached_verified"
	// SBOMAttachmentAttachedUnverified means the subject matched but
	// verification failed or explicitly reported unverified.
	SBOMAttachmentAttachedUnverified SBOMAttachmentStatus = "attached_unverified"
	// SBOMAttachmentAttachedParseOnly means the subject matched and parsing
	// succeeded, but verification material was absent or unsupported.
	SBOMAttachmentAttachedParseOnly SBOMAttachmentStatus = "attached_parse_only"
	// SBOMAttachmentSubjectMismatch means referrer and document subjects
	// disagree, so the document is not attached.
	SBOMAttachmentSubjectMismatch SBOMAttachmentStatus = "subject_mismatch"
	// SBOMAttachmentUnknownSubject means the document parsed but had no digest
	// subject that Eshu can attach to an image.
	SBOMAttachmentUnknownSubject SBOMAttachmentStatus = "unknown_subject"
	// SBOMAttachmentUnparseable means the source document could not be parsed
	// into stable facts.
	SBOMAttachmentUnparseable SBOMAttachmentStatus = "unparseable"
)

// SBOMAttestationAttachmentDecision records one reducer attachment decision.
type SBOMAttestationAttachmentDecision struct {
	DocumentID         string
	DocumentDigest     string
	SubjectDigest      string
	AttachmentStatus   SBOMAttachmentStatus
	ParseStatus        string
	VerificationStatus string
	VerificationPolicy string
	ArtifactKind       string
	Format             string
	SpecVersion        string
	Reason             string
	CanonicalWrites    int
	ComponentCount     int
	ComponentEvidence  []map[string]string
	WarningSummaries   []string
	EvidenceFactIDs    []string
	SourceLayerKinds   []string
}

// SBOMAttestationAttachmentWrite carries decisions for durable publication.
type SBOMAttestationAttachmentWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	Decisions    []SBOMAttestationAttachmentDecision
}

// SBOMAttestationAttachmentWriteResult summarizes durable publication.
type SBOMAttestationAttachmentWriteResult struct {
	CanonicalWrites int
	FactsWritten    int
	EvidenceSummary string
}

// SBOMAttestationAttachmentWriter persists reducer-owned attachment facts.
type SBOMAttestationAttachmentWriter interface {
	WriteSBOMAttestationAttachments(
		context.Context,
		SBOMAttestationAttachmentWrite,
	) (SBOMAttestationAttachmentWriteResult, error)
}

type activeSBOMAttestationAttachmentFactLoader interface {
	ListActiveSBOMAttestationAttachmentFacts(ctx context.Context, digests []string) ([]facts.Envelope, error)
}

// SBOMAttestationAttachmentHandler attaches SBOM and attestation documents to
// image digests only when subject evidence is explicit.
type SBOMAttestationAttachmentHandler struct {
	FactLoader  FactLoader
	Writer      SBOMAttestationAttachmentWriter
	Instruments *telemetry.Instruments
}

// Handle executes one SBOM/attestation attachment reducer intent.
func (h SBOMAttestationAttachmentHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainSBOMAttestationAttachment {
		return Result{}, fmt.Errorf("sbom_attestation_attachment handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("sbom attestation attachment fact loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("sbom attestation attachment writer is required")
	}

	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		sbomAttestationAttachmentFactKinds(),
	)
	if err != nil {
		return Result{}, fmt.Errorf("load sbom attestation attachment facts: %w", err)
	}
	active, err := h.loadActiveSBOMAttestationAttachmentFacts(ctx, sbomAttachmentSubjectDigests(envelopes))
	if err != nil {
		return Result{}, fmt.Errorf("load active sbom attachment subject facts: %w", err)
	}
	envelopes = append(envelopes, active...)

	decisions := BuildSBOMAttestationAttachmentDecisions(envelopes)
	counts := sbomAttestationAttachmentCounts(decisions)
	writeResult, err := h.Writer.WriteSBOMAttestationAttachments(ctx, SBOMAttestationAttachmentWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
		Cause:        intent.Cause,
		Decisions:    decisions,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write sbom attestation attachments: %w", err)
	}
	h.emitCounters(ctx, counts)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainSBOMAttestationAttachment,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: sbomAttestationAttachmentSummary(len(decisions), counts, writeResult.CanonicalWrites),
		CanonicalWrites: writeResult.CanonicalWrites,
	}, nil
}

func (h SBOMAttestationAttachmentHandler) loadActiveSBOMAttestationAttachmentFacts(
	ctx context.Context,
	digests []string,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activeSBOMAttestationAttachmentFactLoader)
	if !ok {
		return nil, nil
	}
	envelopes, err := loader.ListActiveSBOMAttestationAttachmentFacts(ctx, digests)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return envelopes, nil
}

func (h SBOMAttestationAttachmentHandler) emitCounters(
	ctx context.Context,
	counts map[SBOMAttachmentStatus]int,
) {
	if h.Instruments == nil {
		return
	}
	for _, status := range sbomAttachmentStatuses() {
		if counts[status] == 0 {
			continue
		}
		h.Instruments.SBOMAttestationAttachments.Add(ctx, int64(counts[status]), metric.WithAttributes(
			telemetry.AttrDomain(string(DomainSBOMAttestationAttachment)),
			telemetry.AttrOutcome(string(status)),
		))
	}
}

// BuildSBOMAttestationAttachmentDecisions classifies documents without turning
// parse validity or vulnerability component evidence into trust.
func BuildSBOMAttestationAttachmentDecisions(envelopes []facts.Envelope) []SBOMAttestationAttachmentDecision {
	index := buildSBOMAttachmentIndex(envelopes)
	decisions := make([]SBOMAttestationAttachmentDecision, 0, len(index.documents))
	for _, doc := range index.documents {
		decisions = append(decisions, classifySBOMAttachmentDocument(doc, index))
	}
	sort.SliceStable(decisions, func(i, j int) bool {
		return decisions[i].DocumentID < decisions[j].DocumentID
	})
	return decisions
}

func sbomAttestationAttachmentFactKinds() []string {
	return []string{
		facts.SBOMDocumentFactKind,
		facts.SBOMComponentFactKind,
		facts.SBOMDependencyRelationshipFactKind,
		facts.SBOMExternalReferenceFactKind,
		facts.AttestationStatementFactKind,
		facts.AttestationSLSAProvenanceFactKind,
		facts.AttestationSignatureVerificationFactKind,
		facts.SBOMWarningFactKind,
	}
}

func sbomAttachmentStatuses() []SBOMAttachmentStatus {
	return []SBOMAttachmentStatus{
		SBOMAttachmentAttachedVerified,
		SBOMAttachmentAttachedUnverified,
		SBOMAttachmentAttachedParseOnly,
		SBOMAttachmentSubjectMismatch,
		SBOMAttachmentUnknownSubject,
		SBOMAttachmentUnparseable,
	}
}

func sbomAttestationAttachmentCounts(
	decisions []SBOMAttestationAttachmentDecision,
) map[SBOMAttachmentStatus]int {
	counts := make(map[SBOMAttachmentStatus]int, len(sbomAttachmentStatuses()))
	for _, decision := range decisions {
		counts[decision.AttachmentStatus]++
	}
	return counts
}

func sbomAttestationAttachmentSummary(
	evaluated int,
	counts map[SBOMAttachmentStatus]int,
	canonicalWrites int,
) string {
	return fmt.Sprintf(
		"sbom attestation attachments evaluated=%d attached_verified=%d attached_unverified=%d attached_parse_only=%d subject_mismatch=%d unknown_subject=%d unparseable=%d canonical_writes=%d",
		evaluated,
		counts[SBOMAttachmentAttachedVerified],
		counts[SBOMAttachmentAttachedUnverified],
		counts[SBOMAttachmentAttachedParseOnly],
		counts[SBOMAttachmentSubjectMismatch],
		counts[SBOMAttachmentUnknownSubject],
		counts[SBOMAttachmentUnparseable],
		canonicalWrites,
	)
}

func sbomAttestationAttachmentCanonicalWrites(
	decisions []SBOMAttestationAttachmentDecision,
) int {
	total := 0
	for _, decision := range decisions {
		total += decision.CanonicalWrites
	}
	return total
}

func sbomAttachmentSubjectDigests(envelopes []facts.Envelope) []string {
	var digests []string
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.SBOMDocumentFactKind:
			digests = append(digests, payloadString(envelope.Payload, "subject_digest"))
		case facts.AttestationStatementFactKind:
			digests = append(digests, firstPayloadString(envelope.Payload, "subject_digest", "subject_digests"))
		}
	}
	return uniqueSortedStrings(digests)
}

func normalizedVerificationStatus(raw string) string {
	status := strings.ToLower(strings.TrimSpace(raw))
	switch status {
	case "verified", "success", "succeeded", "pass":
		return "passed"
	case "failure", "rejected", "error":
		return "failed"
	default:
		return status
	}
}
