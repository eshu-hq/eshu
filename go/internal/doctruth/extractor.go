package doctruth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// MentionHintStructuredSection identifies a mention supplied by structured
	// documentation metadata instead of broad prose inference.
	MentionHintStructuredSection = "structured_section"

	mentionSourceText = "section_text"
	mentionSourceLink = "section_link"

	claimSuppressionAmbiguousSubject  = "ambiguous_subject"
	claimSuppressionAmbiguousObject   = "ambiguous_object"
	claimSuppressionUnresolvedSubject = "unresolved_subject"
	claimSuppressionUnresolvedObject  = "unresolved_object"
)

// Entity is a known Eshu entity that documentation mentions can resolve to.
type Entity struct {
	Kind        string
	ID          string
	DisplayName string
	Aliases     []string
	URIs        []string
	CodePaths   []string
}

// MentionHint is a deterministic mention candidate supplied by a caller.
type MentionHint struct {
	Text string
	Kind string
	From string
}

// ClaimHint is a caller-supplied structured claim candidate.
type ClaimHint struct {
	ClaimID        string
	ClaimType      string
	ClaimText      string
	SubjectText    string
	SubjectKind    string
	ObjectMentions []MentionHint
	SourceMetadata map[string]string
}

// SectionInput is one bounded documentation section revision to extract from.
type SectionInput struct {
	ScopeID        string
	GenerationID   string
	SourceSystem   string
	DocumentID     string
	RevisionID     string
	SectionID      string
	CanonicalURI   string
	ExcerptHash    string
	SourceStartRef string
	SourceEndRef   string
	Text           string
	Links          []facts.DocumentationLinkPayload
	MentionHints   []MentionHint
	ClaimHints     []ClaimHint
	ObservedAt     time.Time
}

// Options configures optional extraction dependencies.
type Options struct {
	Instruments *telemetry.Instruments
	Logger      *slog.Logger
}

// Report summarizes extraction outcomes using bounded counts.
type Report struct {
	MentionsExact              int
	MentionsAmbiguous          int
	MentionsUnmatched          int
	ClaimCandidates            int
	ClaimsSuppressedAmbiguous  int
	ClaimsSuppressedUnresolved int
}

// Result contains emitted facts and the bounded extraction report.
type Result struct {
	Envelopes []facts.Envelope
	Report    Report
}

// Extractor resolves documentation mentions against a known entity catalog.
type Extractor struct {
	aliasIndex  map[entityKey][]facts.DocumentationEvidenceRef
	uriIndex    map[string][]facts.DocumentationEvidenceRef
	instruments *telemetry.Instruments
	logger      *slog.Logger
}

var reservedSourceMetadataKeys = map[string]struct{}{
	"source_start_ref": {},
	"source_end_ref":   {},
}

type entityKey struct {
	kind string
	text string
}

type mentionCandidate struct {
	text string
	kind string
	from string
}

type mentionResolution struct {
	payload facts.DocumentationEntityMentionPayload
}

// NewExtractor constructs an extractor. The returned extractor is safe for
// concurrent use when Options dependencies are also safe for concurrent use.
func NewExtractor(entities []Entity, options Options) *Extractor {
	extractor := &Extractor{
		aliasIndex:  map[entityKey][]facts.DocumentationEvidenceRef{},
		uriIndex:    map[string][]facts.DocumentationEvidenceRef{},
		instruments: options.Instruments,
		logger:      options.Logger,
	}
	for _, entity := range entities {
		extractor.addEntity(entity)
	}
	return extractor
}

// Extract emits mention and claim candidate facts from one documentation
// section. It returns an error when required section identity is missing.
func (e *Extractor) Extract(ctx context.Context, section SectionInput) (Result, error) {
	if err := validateSection(section); err != nil {
		return Result{}, err
	}

	candidates := e.mentionCandidates(section)
	mentions := make([]mentionResolution, 0, len(candidates))
	mentionByKey := map[entityKey]mentionResolution{}
	report := Report{}
	for _, candidate := range candidates {
		mention := e.resolveMention(section, candidate)
		mentions = append(mentions, mention)
		mentionByKey[entityKey{kind: mention.payload.MentionKind, text: normalizeText(mention.payload.MentionText)}] = mention
		switch mention.payload.ResolutionStatus {
		case facts.DocumentationMentionResolutionExact:
			report.MentionsExact++
		case facts.DocumentationMentionResolutionAmbiguous:
			report.MentionsAmbiguous++
		default:
			report.MentionsUnmatched++
		}
		e.recordMention(ctx, section.SourceSystem, mention.payload.ResolutionStatus)
	}

	envelopes := make([]facts.Envelope, 0, len(mentions)+len(section.ClaimHints))
	for _, mention := range mentions {
		envelope, err := e.envelope(section, facts.DocumentationEntityMentionFactKind, facts.DocumentationEntityMentionStableID(mention.payload), mention.payload)
		if err != nil {
			return Result{}, err
		}
		envelopes = append(envelopes, envelope)
	}

	for _, hint := range section.ClaimHints {
		claim, status, suppressionOutcome := e.claimCandidate(section, hint, mentionByKey)
		switch status {
		case facts.DocumentationMentionResolutionExact:
			envelope, err := e.envelope(section, facts.DocumentationClaimCandidateFactKind, facts.DocumentationClaimCandidateStableID(claim), claim)
			if err != nil {
				return Result{}, err
			}
			envelopes = append(envelopes, envelope)
			report.ClaimCandidates++
			e.recordClaim(ctx, section.SourceSystem, "emitted")
		case facts.DocumentationMentionResolutionAmbiguous:
			report.ClaimsSuppressedAmbiguous++
			e.recordSuppressedClaim(ctx, section.SourceSystem, suppressionOutcome)
		default:
			report.ClaimsSuppressedUnresolved++
			e.recordSuppressedClaim(ctx, section.SourceSystem, suppressionOutcome)
		}
	}

	e.logCompletion(ctx, section, report)
	return Result{Envelopes: envelopes, Report: report}, nil
}

func (e *Extractor) addEntity(entity Entity) {
	if strings.TrimSpace(entity.Kind) == "" || strings.TrimSpace(entity.ID) == "" {
		return
	}
	ref := facts.DocumentationEvidenceRef{
		Kind:       strings.TrimSpace(entity.Kind),
		ID:         strings.TrimSpace(entity.ID),
		Confidence: facts.SourceConfidenceDerived,
	}
	aliases := entityAliases(entity)
	for _, alias := range aliases {
		key := entityKey{kind: ref.Kind, text: normalizeText(alias)}
		e.aliasIndex[key] = appendUniqueRef(e.aliasIndex[key], ref)
	}
	for _, uri := range entity.URIs {
		normalized := normalizeURI(uri)
		if normalized == "" {
			continue
		}
		e.uriIndex[normalized] = appendUniqueRef(e.uriIndex[normalized], ref)
	}
}

func (e *Extractor) mentionCandidates(section SectionInput) []mentionCandidate {
	seen := map[entityKey]struct{}{}
	out := []mentionCandidate{}
	add := func(candidate mentionCandidate) {
		candidate.text = strings.TrimSpace(candidate.text)
		candidate.kind = strings.TrimSpace(candidate.kind)
		if candidate.text == "" || candidate.kind == "" {
			return
		}
		key := entityKey{kind: candidate.kind, text: normalizeText(candidate.text)}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, candidate)
	}

	for _, hint := range section.MentionHints {
		add(mentionCandidate{text: hint.Text, kind: hint.Kind, from: hint.From})
	}
	for _, hint := range section.ClaimHints {
		add(mentionCandidate{text: hint.SubjectText, kind: hint.SubjectKind, from: MentionHintStructuredSection})
		for _, object := range hint.ObjectMentions {
			add(mentionCandidate{text: object.Text, kind: object.Kind, from: firstNonEmpty(object.From, MentionHintStructuredSection)})
		}
	}
	for key := range e.aliasIndex {
		if containsToken(section.Text, key.text) {
			add(mentionCandidate{text: key.text, kind: key.kind, from: mentionSourceText})
		}
	}
	for _, link := range section.Links {
		refs := e.uriIndex[normalizeURI(link.TargetURI)]
		for _, ref := range refs {
			add(mentionCandidate{text: link.TargetURI, kind: ref.Kind, from: mentionSourceLink})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].kind == out[j].kind {
			return out[i].text < out[j].text
		}
		return out[i].kind < out[j].kind
	})
	return out
}

func (e *Extractor) resolveMention(section SectionInput, candidate mentionCandidate) mentionResolution {
	refs := append([]facts.DocumentationEvidenceRef{}, e.aliasIndex[entityKey{
		kind: candidate.kind,
		text: normalizeText(candidate.text),
	}]...)
	if linkRefs := e.uriIndex[normalizeURI(candidate.text)]; len(linkRefs) > 0 {
		refs = appendRefs(refs, linkRefs)
	}
	sortRefs(refs)

	status := facts.DocumentationMentionResolutionUnmatched
	if len(refs) == 1 {
		status = facts.DocumentationMentionResolutionExact
	}
	if len(refs) > 1 {
		status = facts.DocumentationMentionResolutionAmbiguous
	}

	payload := facts.DocumentationEntityMentionPayload{
		DocumentID:       section.DocumentID,
		RevisionID:       section.RevisionID,
		SectionID:        section.SectionID,
		MentionID:        mentionID(section, candidate),
		MentionText:      candidate.text,
		MentionKind:      candidate.kind,
		ResolutionStatus: status,
		CandidateRefs:    refs,
		ExcerptHash:      section.ExcerptHash,
		SourceMetadata: map[string]string{
			"mention_source":   candidate.from,
			"source_start_ref": section.SourceStartRef,
			"source_end_ref":   section.SourceEndRef,
		},
	}
	return mentionResolution{payload: payload}
}

func (e *Extractor) claimCandidate(
	section SectionInput,
	hint ClaimHint,
	mentions map[entityKey]mentionResolution,
) (facts.DocumentationClaimCandidatePayload, string, string) {
	mention, ok := mentions[entityKey{kind: hint.SubjectKind, text: normalizeText(hint.SubjectText)}]
	if !ok {
		return facts.DocumentationClaimCandidatePayload{}, facts.DocumentationMentionResolutionUnmatched, claimSuppressionUnresolvedSubject
	}
	if mention.payload.ResolutionStatus != facts.DocumentationMentionResolutionExact {
		return facts.DocumentationClaimCandidatePayload{}, mention.payload.ResolutionStatus, claimSuppressionAmbiguousSubject
	}
	objectMentionIDs := make([]string, 0, len(hint.ObjectMentions))
	objectStatus := facts.DocumentationMentionResolutionExact
	for _, object := range hint.ObjectMentions {
		objectMention, ok := mentions[entityKey{kind: object.Kind, text: normalizeText(object.Text)}]
		if !ok {
			objectStatus = facts.DocumentationMentionResolutionUnmatched
			continue
		}
		if objectMention.payload.ResolutionStatus == facts.DocumentationMentionResolutionAmbiguous {
			return facts.DocumentationClaimCandidatePayload{}, facts.DocumentationMentionResolutionAmbiguous, claimSuppressionAmbiguousObject
		}
		if objectMention.payload.ResolutionStatus != facts.DocumentationMentionResolutionExact {
			objectStatus = objectMention.payload.ResolutionStatus
			continue
		}
		objectMentionIDs = append(objectMentionIDs, objectMention.payload.MentionID)
	}
	if objectStatus != facts.DocumentationMentionResolutionExact {
		return facts.DocumentationClaimCandidatePayload{}, objectStatus, claimSuppressionUnresolvedObject
	}

	sourceMetadata := map[string]string{
		"source_start_ref": section.SourceStartRef,
		"source_end_ref":   section.SourceEndRef,
	}
	for key, value := range hint.SourceMetadata {
		if _, reserved := reservedSourceMetadataKeys[key]; reserved {
			sourceMetadata["hint."+key] = value
			continue
		}
		sourceMetadata[key] = value
	}
	payload := facts.DocumentationClaimCandidatePayload{
		DocumentID:       section.DocumentID,
		RevisionID:       section.RevisionID,
		SectionID:        section.SectionID,
		ClaimID:          firstNonEmpty(hint.ClaimID, claimID(section, hint)),
		ClaimType:        hint.ClaimType,
		ClaimText:        hint.ClaimText,
		ClaimHash:        textHash(hint.ClaimText),
		ExcerptHash:      section.ExcerptHash,
		SubjectMentionID: mention.payload.MentionID,
		ObjectMentionIDs: objectMentionIDs,
		Authority:        facts.DocumentationClaimAuthorityDocumentEvidence,
		EvidenceRefs: []facts.DocumentationEvidenceRef{{
			Kind:       "document_section",
			ID:         section.SectionID,
			URI:        section.CanonicalURI,
			Confidence: facts.SourceConfidenceObserved,
		}},
		SourceMetadata: sourceMetadata,
	}
	return payload, facts.DocumentationMentionResolutionExact, ""
}

func (e *Extractor) envelope(section SectionInput, kind string, stableKey string, payload any) (facts.Envelope, error) {
	payloadMap, err := payloadToMap(payload)
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("convert %s payload: %w", kind, err)
	}
	observedAt := section.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	return facts.Envelope{
		FactID: facts.StableID("DocumentationExtractionFact", map[string]any{
			"fact_kind":     kind,
			"stable_key":    stableKey,
			"scope_id":      section.ScopeID,
			"generation_id": section.GenerationID,
		}),
		ScopeID:          section.ScopeID,
		GenerationID:     section.GenerationID,
		FactKind:         kind,
		StableFactKey:    stableKey,
		SchemaVersion:    facts.DocumentationFactSchemaVersion,
		CollectorKind:    string(scope.CollectorDocumentation),
		SourceConfidence: facts.SourceConfidenceDerived,
		ObservedAt:       observedAt,
		Payload:          payloadMap,
		SourceRef: facts.Ref{
			SourceSystem:   section.SourceSystem,
			ScopeID:        section.ScopeID,
			GenerationID:   section.GenerationID,
			FactKey:        stableKey,
			SourceURI:      section.CanonicalURI,
			SourceRecordID: section.SectionID,
		},
	}, nil
}

func validateSection(section SectionInput) error {
	switch {
	case strings.TrimSpace(section.ScopeID) == "":
		return errors.New("scope id is required")
	case strings.TrimSpace(section.GenerationID) == "":
		return errors.New("generation id is required")
	case strings.TrimSpace(section.SourceSystem) == "":
		return errors.New("source system is required")
	case strings.TrimSpace(section.DocumentID) == "":
		return errors.New("document id is required")
	case strings.TrimSpace(section.RevisionID) == "":
		return errors.New("revision id is required")
	case strings.TrimSpace(section.SectionID) == "":
		return errors.New("section id is required")
	case strings.TrimSpace(section.ExcerptHash) == "":
		return errors.New("excerpt hash is required")
	default:
		return nil
	}
}

func normalizeURI(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return value
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String()
}
