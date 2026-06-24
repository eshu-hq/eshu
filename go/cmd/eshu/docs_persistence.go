// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

type docsPersistenceFactory func(context.Context) (docsVerifyPersistence, func() error, error)

type docsVerifyDeps struct {
	openPersistence docsPersistenceFactory
	commandTruth    func() []doctruth.CommandTruth
	now             func() time.Time
}

type docsVerifyPersistence interface {
	CurrentGeneration(context.Context, string) (docsPersistedGeneration, bool, error)
	ListFactEnvelopes(context.Context, string, string, []string) ([]facts.Envelope, error)
	CommitScopeGeneration(context.Context, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope) error
}

type docsPersistedGeneration struct {
	GenerationID  string
	FreshnessHint string
}

type docsVerifyPersistenceSummary struct {
	Enabled       bool   `json:"enabled"`
	Persisted     bool   `json:"persisted"`
	Skipped       bool   `json:"skipped"`
	ScopeID       string `json:"scope_id,omitempty"`
	GenerationID  string `json:"generation_id,omitempty"`
	FreshnessHint string `json:"freshness_hint,omitempty"`
	Repository    string `json:"repository,omitempty"`
}

type docsVerifyPostgresPersistence struct {
	ingestion postgres.IngestionStore
	facts     postgres.FactStore
}

const docsVerifyFreshnessVersion = "docs-verify-v1"

func defaultDocsVerifyDeps() docsVerifyDeps {
	return docsVerifyDeps{
		openPersistence: openDocsVerifyPostgresPersistence,
		commandTruth:    func() []doctruth.CommandTruth { return commandTruthFromCobra(rootCmd) },
		now:             func() time.Time { return time.Now().UTC() },
	}
}

func openDocsVerifyPostgresPersistence(ctx context.Context) (docsVerifyPersistence, func() error, error) {
	db, err := runtimecfg.OpenPostgres(ctx, os.Getenv)
	if err != nil {
		return nil, nil, err
	}
	sqlDB := postgres.SQLDB{DB: db}
	persistence := docsVerifyPostgresPersistence{
		ingestion: postgres.NewIngestionStore(sqlDB),
		facts:     postgres.NewFactStore(sqlDB),
	}
	return persistence, db.Close, nil
}

func (p docsVerifyPostgresPersistence) CurrentGeneration(
	ctx context.Context,
	scopeID string,
) (docsPersistedGeneration, bool, error) {
	current, found, err := p.ingestion.CurrentScopeGeneration(ctx, scopeID)
	if err != nil || !found {
		return docsPersistedGeneration{}, found, err
	}
	return docsPersistedGeneration{
		GenerationID:  current.GenerationID,
		FreshnessHint: current.FreshnessHint,
	}, true, nil
}

func (p docsVerifyPostgresPersistence) ListFactEnvelopes(
	ctx context.Context,
	scopeID string,
	generationID string,
	kinds []string,
) ([]facts.Envelope, error) {
	return p.facts.ListFactsByKind(ctx, scopeID, generationID, kinds)
}

func (p docsVerifyPostgresPersistence) CommitScopeGeneration(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	return p.ingestion.CommitScopeGeneration(ctx, scopeValue, generation, factStream)
}

func prepareDocsVerifyPersistence(
	ctx context.Context,
	opts docsVerifyOptions,
	inventory docsInventory,
	deps docsVerifyDeps,
) (docsVerifyPersistence, func() error, docsVerifyPersistenceSummary, error) {
	summary := docsVerifyPersistenceSummary{}
	if !opts.Persist {
		return nil, nil, summary, nil
	}
	if deps.openPersistence == nil {
		return nil, nil, summary, fmt.Errorf("documentation persistence is not configured")
	}
	scopeID := docsVerifyScopeID(opts.Path, opts.Scope)
	freshness := docsInventoryFreshnessHint(inventory.Documents, opts.MaxDocumentBytes, opts.Limit, opts.ImageTruth)
	generationID := docsVerifyGenerationID(scopeID, freshness)
	summary = docsVerifyPersistenceSummary{
		Enabled:       true,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FreshnessHint: freshness,
		Repository:    strings.TrimSpace(opts.Repo),
	}
	persistence, closePersistence, err := deps.openPersistence(ctx)
	if err != nil {
		return nil, nil, summary, fmt.Errorf("open documentation persistence: %w", err)
	}
	current, found, err := persistence.CurrentGeneration(ctx, scopeID)
	if err != nil {
		if closePersistence != nil {
			_ = closePersistence()
		}
		return nil, nil, summary, fmt.Errorf("check documentation persistence freshness: %w", err)
	}
	if found && current.FreshnessHint == freshness {
		summary.Skipped = true
		summary.GenerationID = current.GenerationID
	}
	return persistence, closePersistence, summary, nil
}

func docsVerifyResultFromPersisted(
	ctx context.Context,
	persistence docsVerifyPersistence,
	summary docsVerifyPersistenceSummary,
) (doctruth.VerificationResult, error) {
	envelopes, err := persistence.ListFactEnvelopes(ctx, summary.ScopeID, summary.GenerationID, []string{
		facts.DocumentationFindingFactKind,
		facts.DocumentationEvidencePacketFactKind,
	})
	if err != nil {
		return doctruth.VerificationResult{}, fmt.Errorf("load persisted documentation verification facts: %w", err)
	}
	return docsVerifyResultFromEnvelopes(envelopes), nil
}

func docsVerifyResultFromEnvelopes(envelopes []facts.Envelope) doctruth.VerificationResult {
	result := doctruth.VerificationResult{Envelopes: envelopes}
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.DocumentationFindingFactKind:
			finding := findingFromPayload(envelope.Payload)
			if finding.FindingID != "" {
				result.Findings = append(result.Findings, finding)
				addDocsVerifyFindingStatus(&result.Summary, finding.Status)
			}
		case facts.DocumentationEvidencePacketFactKind:
			packet := packetFromPayload(envelope.Payload)
			if packet.PacketID != "" {
				result.EvidencePackets = append(result.EvidencePackets, packet)
			}
		}
	}
	result.Summary.EvidencePackets = len(result.EvidencePackets)
	result.Summary.DocumentationFindings = len(result.Findings)
	return result
}

func commitDocsVerifyResult(
	ctx context.Context,
	persistence docsVerifyPersistence,
	summary docsVerifyPersistenceSummary,
	result doctruth.VerificationResult,
	now func() time.Time,
) error {
	scopeValue := docsVerifyScope(summary)
	generation := docsVerifyGeneration(scopeValue.ScopeID, summary.GenerationID, summary.FreshnessHint, now)
	stream := make(chan facts.Envelope)
	go func() {
		defer close(stream)
		for _, envelope := range result.Envelopes {
			stream <- envelope
		}
	}()
	if err := persistence.CommitScopeGeneration(ctx, scopeValue, generation, stream); err != nil {
		return fmt.Errorf("persist documentation verification facts: %w", err)
	}
	return nil
}

func docsVerifyScope(summary docsVerifyPersistenceSummary) scope.IngestionScope {
	metadata := map[string]string{}
	if summary.Repository != "" {
		metadata["repo"] = summary.Repository
	}
	return scope.IngestionScope{
		ScopeID:       summary.ScopeID,
		SourceSystem:  "local_docs",
		ScopeKind:     scope.KindDocumentationSource,
		CollectorKind: scope.CollectorDocumentation,
		PartitionKey:  summary.ScopeID,
		Metadata:      metadata,
	}
}

func docsVerifyGeneration(scopeID, generationID, freshness string, now func() time.Time) scope.ScopeGeneration {
	observedAt := time.Now().UTC()
	if now != nil {
		observedAt = now().UTC()
	}
	return scope.ScopeGeneration{
		GenerationID:  generationID,
		ScopeID:       scopeID,
		ObservedAt:    observedAt,
		IngestedAt:    observedAt,
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: freshness,
	}
}

func docsVerifyScopeID(path string, explicit string) string {
	if value := strings.TrimSpace(explicit); value != "" {
		return value
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = filepath.Clean(path)
	}
	return "docs-verify:" + facts.StableID("documentation-verify-scope", map[string]any{
		"path": fileURI(absolute),
	})
}

func docsVerifyGenerationID(scopeID, freshness string) string {
	return "docs-verify-generation:" + facts.StableID("documentation-verify-generation", map[string]any{
		"scope_id":       scopeID,
		"freshness_hint": freshness,
	})
}

func docsInventoryFreshnessHint(
	documents []doctruth.DocumentInput,
	maxDocumentBytes int,
	limit int,
	imageTruth string,
) string {
	type docFingerprint struct {
		Path       string `json:"path"`
		SourceURI  string `json:"source_uri"`
		RevisionID string `json:"revision_id"`
		Truncated  bool   `json:"truncated"`
	}
	type freshnessInput struct {
		Version          string           `json:"version"`
		MaxDocumentBytes int              `json:"max_document_bytes"`
		Limit            int              `json:"limit"`
		ImageTruth       string           `json:"image_truth"`
		Documents        []docFingerprint `json:"documents"`
	}
	fingerprints := make([]docFingerprint, 0, len(documents))
	for _, doc := range documents {
		fingerprints = append(fingerprints, docFingerprint{
			Path:       doc.Path,
			SourceURI:  doc.SourceURI,
			RevisionID: doc.RevisionID,
			Truncated:  doc.ContentTruncated,
		})
	}
	sort.Slice(fingerprints, func(i, j int) bool {
		if fingerprints[i].SourceURI == fingerprints[j].SourceURI {
			return fingerprints[i].Path < fingerprints[j].Path
		}
		return fingerprints[i].SourceURI < fingerprints[j].SourceURI
	})
	encoded, err := json.Marshal(freshnessInput{
		Version:          docsVerifyFreshnessVersion,
		MaxDocumentBytes: maxDocumentBytes,
		Limit:            limit,
		ImageTruth:       normalizedDocsVerifyImageTruth(imageTruth),
		Documents:        fingerprints,
	})
	if err != nil {
		return "sha256:"
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func applyDocsVerifyInventorySummary(result *doctruth.VerificationResult, inventory docsInventory) {
	truncated := inventory.Truncated
	bytesScanned := 0
	for _, doc := range inventory.Documents {
		bytesScanned += len(doc.Content)
		if doc.ContentTruncated {
			truncated = true
		}
	}
	result.Summary.DocumentsScanned = len(inventory.Documents)
	result.Summary.BytesScanned = bytesScanned
	result.Truncated = result.Truncated || truncated
}

func addDocsVerifyFindingStatus(s *doctruth.VerificationSummary, status string) {
	s.ClaimsChecked++
	switch status {
	case doctruth.VerificationStatusValid:
		s.Valid++
	case doctruth.VerificationStatusContradicted:
		s.Contradicted++
	case doctruth.VerificationStatusMissingEvidence:
		s.MissingEvidence++
	case doctruth.VerificationStatusUnsupportedClaimType:
		s.UnsupportedClaimType++
	}
}

func findingFromPayload(payload map[string]any) doctruth.VerificationFinding {
	return doctruth.VerificationFinding{
		FindingID:        stringPayload(payload, "finding_id"),
		FindingVersion:   stringPayload(payload, "finding_version"),
		FindingType:      stringPayload(payload, "finding_type"),
		Status:           stringPayload(payload, "status"),
		TruthLevel:       stringPayload(payload, "truth_level"),
		FreshnessState:   stringPayload(payload, "freshness_state"),
		SourceID:         stringPayload(payload, "source_id"),
		DocumentID:       stringPayload(payload, "document_id"),
		SectionID:        stringPayload(payload, "section_id"),
		ClaimID:          stringPayload(payload, "claim_id"),
		ClaimType:        stringPayload(payload, "claim_type"),
		ClaimText:        stringPayload(payload, "claim_text"),
		NormalizedClaim:  stringPayload(payload, "normalized_claim"),
		Summary:          stringPayload(payload, "summary"),
		EvidencePacketID: stringPayload(payload, "evidence_packet_id"),
	}
}

func packetFromPayload(payload map[string]any) doctruth.VerificationEvidencePacket {
	return doctruth.VerificationEvidencePacket{
		PacketID:      stringPayload(payload, "packet_id"),
		PacketVersion: stringPayload(payload, "packet_version"),
		FindingID:     stringPayload(payload, "finding_id"),
		Payload:       payload,
	}
}

func stringPayload(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}
