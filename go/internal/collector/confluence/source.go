package confluence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Source emits one Confluence documentation source generation at a time.
type Source struct {
	Client          Client
	Config          SourceConfig
	Logger          *slog.Logger
	Instruments     *telemetry.Instruments
	TruthExtractor  *doctruth.Extractor
	TruthClaimHints func(Page, facts.DocumentationSectionPayload) []doctruth.ClaimHint

	drained bool
}

func (s *Source) next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	if s.Client == nil {
		s.recordSyncFailure(ctx, "configuration")
		return collector.CollectedGeneration{}, false, errors.New("confluence client is required")
	}
	observedAt := s.Config.now()

	pages, spaceValue, failureCount, err := s.collectPages(ctx)
	if err != nil {
		s.recordSyncFailure(ctx, "source_read")
		return collector.CollectedGeneration{}, false, err
	}
	pages = latestCurrentPages(pages)

	scopeValue := s.ingestionScope(spaceValue)
	generationValue := scope.ScopeGeneration{
		GenerationID:  generationID(scopeValue.ScopeID, pages),
		ScopeID:       scopeValue.ScopeID,
		ObservedAt:    observedAt,
		IngestedAt:    observedAt,
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: freshnessHint(pages),
	}
	envelopes, err := s.factEnvelopes(ctx, scopeValue, generationValue, spaceValue, pages, failureCount)
	if err != nil {
		s.recordSyncFailure(ctx, "fact_build")
		return collector.CollectedGeneration{}, false, err
	}
	s.recordFactMetrics(ctx, envelopes)
	if s.Logger != nil {
		s.Logger.InfoContext(
			ctx,
			"confluence sync completed",
			slog.String("scope_id", scopeValue.ScopeID),
			slog.Int("page_count", len(pages)),
			slog.Int("failure_count", failureCount),
			slog.String("freshness_hint", generationValue.FreshnessHint),
		)
	}

	s.drained = true
	return collector.FactsFromSlice(scopeValue, generationValue, envelopes), true, nil
}

func (s *Source) ingestionScope(spaceValue Space) scope.IngestionScope {
	sourceID := documentationSourceID(s.Config, spaceValue)
	return scope.IngestionScope{
		ScopeID:       sourceID,
		SourceSystem:  "confluence",
		ScopeKind:     scope.KindDocumentationSource,
		CollectorKind: scope.CollectorDocumentation,
		PartitionKey:  s.Config.boundedID(),
		Metadata: map[string]string{
			"base_url":     s.Config.BaseURL,
			"space_id":     spaceValue.ID,
			"space_key":    firstNonEmpty(spaceValue.Key, s.Config.SpaceKey),
			"root_page_id": s.Config.RootPageID,
		},
	}
}

func (s *Source) factEnvelopes(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generationValue scope.ScopeGeneration,
	spaceValue Space,
	pages []Page,
	failureCount int,
) ([]facts.Envelope, error) {
	sourcePayload := facts.DocumentationSourcePayload{
		SourceID:     scopeValue.ScopeID,
		SourceSystem: "confluence",
		ExternalID:   firstNonEmpty(spaceValue.ID, s.Config.RootPageID),
		DisplayName:  firstNonEmpty(spaceValue.Name, spaceValue.Key, s.Config.RootPageID),
		BaseURI:      s.Config.BaseURL,
		SourceType:   sourceType(s.Config),
		Labels:       nonEmptyStrings(firstNonEmpty(spaceValue.Key, s.Config.SpaceKey)),
		ACLSummary: &facts.DocumentationACLSummary{
			Visibility:    "credential_viewable",
			IsPartial:     true,
			PartialReason: "confluence_source_restrictions_not_collected",
		},
		SourceMetadata: map[string]string{
			"page_count":    strconv.Itoa(len(pages)),
			"failure_count": strconv.Itoa(failureCount),
			"sync_status":   syncStatus(failureCount),
		},
	}
	sourceEnvelope, err := envelope(scopeValue, generationValue, facts.DocumentationSourceFactKind, facts.DocumentationSourceStableID(sourcePayload), sourcePayload, s.Config.BaseURL, sourcePayload.ExternalID)
	if err != nil {
		return nil, err
	}
	out := []facts.Envelope{sourceEnvelope}

	for _, page := range pages {
		documentPayload := documentPayload(scopeValue.ScopeID, s.Config.BaseURL, page)
		documentEnvelope, err := envelope(scopeValue, generationValue, facts.DocumentationDocumentFactKind, facts.DocumentationDocumentStableID(documentPayload), documentPayload, documentPayload.CanonicalURI, page.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, documentEnvelope)
		sections := sectionsForPage(page)
		links := linksForPage(page, sections)
		for _, section := range sections {
			sectionEnvelope, err := envelope(
				scopeValue,
				generationValue,
				facts.DocumentationSectionFactKind,
				facts.DocumentationSectionStableID(section),
				sectionPayloadMap(section),
				documentPayload.CanonicalURI,
				page.ID,
			)
			if err != nil {
				return nil, err
			}
			out = append(out, sectionEnvelope)
		}
		for _, link := range links {
			linkEnvelope, err := envelope(scopeValue, generationValue, facts.DocumentationLinkFactKind, facts.DocumentationLinkStableID(link), link, documentPayload.CanonicalURI, page.ID)
			if err != nil {
				return nil, err
			}
			out = append(out, linkEnvelope)
		}
		if s.TruthExtractor != nil {
			truthEnvelopes, err := s.documentationTruthEnvelopes(ctx, scopeValue, generationValue, documentPayload, sections, links, page)
			if err != nil {
				return nil, err
			}
			out = append(out, truthEnvelopes...)
		}
	}
	return out, nil
}

func (s *Source) documentationTruthEnvelopes(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generationValue scope.ScopeGeneration,
	documentPayload facts.DocumentationDocumentPayload,
	sections []facts.DocumentationSectionPayload,
	links []facts.DocumentationLinkPayload,
	page Page,
) ([]facts.Envelope, error) {
	out := []facts.Envelope{}
	for _, section := range sections {
		result, err := s.TruthExtractor.Extract(ctx, doctruth.SectionInput{
			ScopeID:        scopeValue.ScopeID,
			GenerationID:   generationValue.GenerationID,
			SourceSystem:   scopeValue.SourceSystem,
			DocumentID:     section.DocumentID,
			RevisionID:     section.RevisionID,
			SectionID:      section.SectionID,
			CanonicalURI:   documentPayload.CanonicalURI,
			ExcerptHash:    section.ExcerptHash,
			SourceStartRef: section.SourceStartRef,
			SourceEndRef:   section.SourceEndRef,
			Text:           plainText(page.Body.Storage.Value),
			Links:          linksForSection(links, section.SectionID),
			ClaimHints:     s.claimHints(page, section),
			ObservedAt:     generationValue.ObservedAt,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, result.Envelopes...)
	}
	return out, nil
}

func (s *Source) claimHints(page Page, section facts.DocumentationSectionPayload) []doctruth.ClaimHint {
	if s.TruthClaimHints == nil {
		return nil
	}
	return s.TruthClaimHints(page, section)
}

func linksForSection(links []facts.DocumentationLinkPayload, sectionID string) []facts.DocumentationLinkPayload {
	out := make([]facts.DocumentationLinkPayload, 0, len(links))
	for _, link := range links {
		if link.SectionID == sectionID {
			out = append(out, link)
		}
	}
	return out
}

func mergePageDetails(listed Page, detail Page) Page {
	if detail.ID == "" {
		detail.ID = listed.ID
	}
	if detail.Status == "" {
		detail.Status = listed.Status
	}
	if detail.Title == "" {
		detail.Title = listed.Title
	}
	if detail.SpaceID == "" {
		detail.SpaceID = listed.SpaceID
	}
	if detail.ParentID == "" {
		detail.ParentID = listed.ParentID
	}
	if detail.OwnerID == "" {
		detail.OwnerID = listed.OwnerID
	}
	if detail.AuthorID == "" {
		detail.AuthorID = listed.AuthorID
	}
	if detail.Version.Number == 0 {
		detail.Version = listed.Version
	}
	if detail.Body.Storage.Value == "" {
		detail.Body = listed.Body
	}
	if len(pageLabels(detail)) == 0 {
		detail.Labels = listed.Labels
		detail.LabelSet = listed.LabelSet
	}
	if detail.Links.Base == "" {
		detail.Links.Base = listed.Links.Base
	}
	if detail.Links.WebUI == "" {
		detail.Links.WebUI = listed.Links.WebUI
	}
	return detail
}

func envelope(scopeValue scope.IngestionScope, generationValue scope.ScopeGeneration, kind string, key string, payload any, uri string, recordID string) (facts.Envelope, error) {
	payloadMap, err := payloadToMap(payload)
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("convert %s payload: %w", kind, err)
	}
	return facts.Envelope{
		FactID: facts.StableID("ConfluenceDocumentationFact", map[string]any{
			"fact_kind":     kind,
			"stable_key":    key,
			"scope_id":      scopeValue.ScopeID,
			"generation_id": generationValue.GenerationID,
		}),
		ScopeID:          scopeValue.ScopeID,
		GenerationID:     generationValue.GenerationID,
		FactKind:         kind,
		StableFactKey:    key,
		SchemaVersion:    schemaVersionForFactKind(kind),
		CollectorKind:    string(scope.CollectorDocumentation),
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       generationValue.ObservedAt,
		Payload:          payloadMap,
		SourceRef: facts.Ref{
			SourceSystem:   "confluence",
			ScopeID:        scopeValue.ScopeID,
			GenerationID:   generationValue.GenerationID,
			FactKey:        key,
			SourceURI:      uri,
			SourceRecordID: recordID,
		},
	}, nil
}

func payloadToMap(payload any) (map[string]any, error) {
	if payloadMap, ok := payload.(map[string]any); ok {
		return payloadMap, nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func schemaVersionForFactKind(kind string) string {
	if kind == facts.DocumentationSectionFactKind {
		return facts.DocumentationSectionFactSchemaVersion
	}
	return facts.DocumentationFactSchemaVersion
}

func latestCurrentPages(pages []Page) []Page {
	latest := map[string]Page{}
	for _, page := range pages {
		if page.Status != "" && page.Status != "current" {
			continue
		}
		if existing, ok := latest[page.ID]; !ok || page.Version.Number > existing.Version.Number {
			latest[page.ID] = page
		}
	}
	out := make([]Page, 0, len(latest))
	for _, page := range latest {
		out = append(out, page)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func generationID(scopeID string, pages []Page) string {
	identity := map[string]any{"scope_id": scopeID, "page_count": len(pages)}
	for _, page := range pages {
		identity["page:"+page.ID] = page.Version.Number
	}
	return facts.StableID("ConfluenceDocumentationGeneration", identity)
}

func freshnessHint(pages []Page) string {
	latest := ""
	for _, page := range pages {
		if page.Version.CreatedAt > latest {
			latest = page.Version.CreatedAt
		}
	}
	return latest
}

func hashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func pageLimit(limit int) int {
	if limit <= 0 {
		return defaultPageLimit
	}
	return limit
}

func syncStatus(failureCount int) string {
	if failureCount > 0 {
		return "partial"
	}
	return "complete"
}

func sourceType(config SourceConfig) string {
	if config.SpaceID != "" {
		return "confluence_space"
	}
	return "confluence_page_tree"
}

func documentationSourceID(config SourceConfig, spaceValue Space) string {
	return "doc-source:confluence:" + tenantID(config.BaseURL) + ":" + firstNonEmpty(spaceValue.ID, spaceValue.Key, config.RootPageID)
}

func tenantID(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil || strings.TrimSpace(parsed.Host) == "" {
		return facts.StableID("ConfluenceTenant", map[string]any{"base_url": baseURL})
	}
	return strings.ToLower(parsed.Host)
}

func firstSpaceID(pages []Page) string {
	for _, page := range pages {
		if strings.TrimSpace(page.SpaceID) != "" {
			return page.SpaceID
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}
