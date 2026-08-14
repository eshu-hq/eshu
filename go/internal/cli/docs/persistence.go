// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// freshnessVersion namespaces the freshness hint. Bump it when the fingerprint
// inputs change, so previously persisted generations stop matching and get
// re-verified instead of silently replaying a hint computed a different way.
const freshnessVersion = "docs-verify-v1"

// PersistenceFactory opens the persistence backend, returning the store, a
// close function, and any open error. The CLI wrapper supplies it because
// opening Postgres means reading the DSN from the process environment.
type PersistenceFactory func(context.Context) (Persistence, func() error, error)

// Persistence is the storage surface a verify run needs: read the current
// generation for a scope, read back the facts of a generation, and commit a
// new generation's facts.
type Persistence interface {
	CurrentGeneration(context.Context, string) (PersistedGeneration, bool, error)
	ListFactEnvelopes(context.Context, string, string, []string) ([]facts.Envelope, error)
	CommitScopeGeneration(context.Context, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope) error
}

// PersistedGeneration is the stored generation a freshness check compares
// against.
type PersistedGeneration struct {
	GenerationID  string
	FreshnessHint string
}

// PersistenceSummary reports what persistence did on this run. Enabled follows
// --persist. Skipped means the stored generation was still fresh and its
// findings were replayed instead of re-verified. Persisted means this run
// committed a new generation.
type PersistenceSummary struct {
	Enabled       bool   `json:"enabled"`
	Persisted     bool   `json:"persisted"`
	Skipped       bool   `json:"skipped"`
	ScopeID       string `json:"scope_id,omitempty"`
	GenerationID  string `json:"generation_id,omitempty"`
	FreshnessHint string `json:"freshness_hint,omitempty"`
	Repository    string `json:"repository,omitempty"`
}

// PostgresPersistence is the Postgres-backed Persistence implementation.
type PostgresPersistence struct {
	ingestion postgres.IngestionStore
	facts     *postgres.FactStore
}

// NewPostgresPersistence wraps an open database handle as a Persistence. It
// does not open, ping, or close the handle -- the caller that opened it owns
// its lifetime.
func NewPostgresPersistence(db *sql.DB) PostgresPersistence {
	sqlDB := postgres.SQLDB{DB: db}
	return PostgresPersistence{
		ingestion: postgres.NewIngestionStore(sqlDB),
		facts:     postgres.NewFactStore(sqlDB),
	}
}

// CurrentGeneration reports the scope's current generation and whether one
// exists.
func (p PostgresPersistence) CurrentGeneration(
	ctx context.Context,
	scopeID string,
) (PersistedGeneration, bool, error) {
	current, found, err := p.ingestion.CurrentScopeGeneration(ctx, scopeID)
	if err != nil || !found {
		return PersistedGeneration{}, found, err //nolint:wrapcheck // preparePersistence wraps this as "check documentation persistence freshness"; wrapping here would double the context in the operator-visible message.
	}
	return PersistedGeneration{
		GenerationID:  current.GenerationID,
		FreshnessHint: current.FreshnessHint,
	}, true, nil
}

// ListFactEnvelopes reads the stored facts of a generation, filtered to kinds.
func (p PostgresPersistence) ListFactEnvelopes(
	ctx context.Context,
	scopeID string,
	generationID string,
	kinds []string,
) ([]facts.Envelope, error) {
	return p.facts.ListFactsByKind(ctx, scopeID, generationID, kinds) //nolint:wrapcheck // resultFromPersisted wraps this as "load persisted documentation verification facts".
}

// CommitScopeGeneration commits a generation and its streamed facts.
func (p PostgresPersistence) CommitScopeGeneration(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	return p.ingestion.CommitScopeGeneration(ctx, scopeValue, generation, factStream) //nolint:wrapcheck // commitResult wraps this as "persist documentation verification facts".
}

// preparePersistence resolves the scope, freshness hint, and generation id for
// this run and opens the store. When persistence is off it returns a zero
// summary and no store. When the stored generation's freshness hint still
// matches the current inventory it marks the summary Skipped, which tells
// Verify to replay stored findings instead of re-verifying.
func preparePersistence(
	ctx context.Context,
	opts VerifyOptions,
	inventory Inventory,
	deps Deps,
) (Persistence, func() error, PersistenceSummary, error) {
	summary := PersistenceSummary{}
	if !opts.Persist {
		return nil, nil, summary, nil
	}
	if deps.OpenPersistence == nil {
		return nil, nil, summary, fmt.Errorf("documentation persistence is not configured")
	}
	scopeID := ScopeID(opts.Path, opts.Scope)
	freshness := InventoryFreshnessHint(inventory.Documents, opts.MaxDocumentBytes, opts.Limit, opts.ImageTruth)
	generation := deriveGenerationID(scopeID, freshness)
	summary = PersistenceSummary{
		Enabled:       true,
		ScopeID:       scopeID,
		GenerationID:  generation,
		FreshnessHint: freshness,
		Repository:    strings.TrimSpace(opts.Repo),
	}
	persistence, closePersistence, err := deps.OpenPersistence(ctx)
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

// resultFromPersisted rebuilds a verification result from the stored findings
// and evidence packets of a generation.
func resultFromPersisted(
	ctx context.Context,
	persistence Persistence,
	summary PersistenceSummary,
) (doctruth.VerificationResult, error) {
	envelopes, err := persistence.ListFactEnvelopes(ctx, summary.ScopeID, summary.GenerationID, []string{
		facts.DocumentationFindingFactKind,
		facts.DocumentationEvidencePacketFactKind,
	})
	if err != nil {
		return doctruth.VerificationResult{}, fmt.Errorf("load persisted documentation verification facts: %w", err)
	}
	return resultFromEnvelopes(envelopes), nil
}

// resultFromEnvelopes decodes stored fact envelopes back into findings,
// evidence packets, and the derived summary counters. An envelope missing its
// identity field is dropped rather than counted.
func resultFromEnvelopes(envelopes []facts.Envelope) doctruth.VerificationResult {
	result := doctruth.VerificationResult{Envelopes: envelopes}
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.DocumentationFindingFactKind:
			finding := findingFromPayload(envelope.Payload)
			if finding.FindingID != "" {
				result.Findings = append(result.Findings, finding)
				addFindingStatus(&result.Summary, finding.Status)
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

// commitResult streams the result's fact envelopes into a new scope
// generation.
func commitResult(
	ctx context.Context,
	persistence Persistence,
	summary PersistenceSummary,
	result doctruth.VerificationResult,
	now func() time.Time,
) error {
	scopeValue := verifyScope(summary)
	generation := verifyGeneration(scopeValue.ScopeID, summary.GenerationID, summary.FreshnessHint, now)
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

// verifyScope builds the ingestion scope documentation facts are committed
// under. The optional repository selector is recorded as scope metadata.
func verifyScope(summary PersistenceSummary) scope.IngestionScope {
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

// verifyGeneration builds the pending snapshot generation for a commit. A nil
// now falls back to the wall clock.
func verifyGeneration(scopeID, generationID, freshness string, now func() time.Time) scope.ScopeGeneration {
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

// ScopeID returns the ingestion scope id for a verify run: the explicit
// --scope value when given, otherwise one derived from the absolute scan path
// so the same tree keeps the same scope across runs and working directories.
func ScopeID(path string, explicit string) string {
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

// deriveGenerationID derives the generation id from the scope and its
// freshness hint, so an unchanged inventory maps back to the same generation.
func deriveGenerationID(scopeID, freshness string) string {
	return "docs-verify-generation:" + facts.StableID("documentation-verify-generation", map[string]any{
		"scope_id":       scopeID,
		"freshness_hint": freshness,
	})
}

// InventoryFreshnessHint fingerprints an inventory plus the bounds that
// produced it. The scan bounds and image truth source are part of the
// fingerprint on purpose: the same documents scanned with a different
// --max-bytes, --limit, or image truth source can produce different findings,
// so those runs must not be treated as a cache hit for one another.
func InventoryFreshnessHint(
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
		Version:          freshnessVersion,
		MaxDocumentBytes: maxDocumentBytes,
		Limit:            limit,
		ImageTruth:       NormalizeImageTruthMode(imageTruth),
		Documents:        fingerprints,
	})
	if err != nil {
		return "sha256:"
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// applyInventorySummary overwrites the document counters on a replayed result
// with what this run actually inventoried. Stored findings carry no scan
// counters of their own, so without this a skipped run would report zero
// documents scanned.
func applyInventorySummary(result *doctruth.VerificationResult, inventory Inventory) {
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

// addFindingStatus folds one replayed finding's status into the summary
// counters.
func addFindingStatus(s *doctruth.VerificationSummary, status string) {
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

// findingFromPayload decodes a stored finding fact payload.
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

// packetFromPayload decodes a stored evidence packet fact payload, keeping the
// whole payload as the packet body.
func packetFromPayload(payload map[string]any) doctruth.VerificationEvidencePacket {
	return doctruth.VerificationEvidencePacket{
		PacketID:      stringPayload(payload, "packet_id"),
		PacketVersion: stringPayload(payload, "packet_version"),
		FindingID:     stringPayload(payload, "finding_id"),
		Payload:       payload,
	}
}

// stringPayload reads a trimmed string field from a fact payload, yielding
// empty for a missing key or a non-string value.
func stringPayload(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}
