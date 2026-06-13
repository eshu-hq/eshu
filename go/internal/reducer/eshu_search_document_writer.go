package reducer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// eshuSearchDocumentRetireQuery removes search-document facts in one generation
// that are not in the freshly written set, so a source row dropped within a
// generation retires its document. An empty written set matches the empty array
// and retires every document for the generation.
const eshuSearchDocumentRetireQuery = `
DELETE FROM fact_records
WHERE fact_kind = $1
  AND scope_id = $2
  AND generation_id = $3
  AND fact_id <> ALL($4::text[])
`

type eshuSearchDocumentExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// EshuSearchDocumentWrite is the complete curated document set for one scope and
// generation. The writer treats it as authoritative: documents are upserted and
// any prior document for the generation that is absent is retired.
type EshuSearchDocumentWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Documents    []searchdocs.Document
}

// EshuSearchDocumentWriteResult reports how many documents were written and how
// many stale documents were retired.
type EshuSearchDocumentWriteResult struct {
	CanonicalWrites int
	Retired         int
}

// PostgresEshuSearchDocumentWriter persists curated search documents into the
// shared fact store as derived, generation-scoped records.
type PostgresEshuSearchDocumentWriter struct {
	DB  eshuSearchDocumentExecer
	Now func() time.Time
}

// WriteEshuSearchDocuments upserts each curated document as a derived fact and
// retires any document in the same generation that is no longer present. Upserts
// are idempotent by fact_id, so a retry of the same generation converges.
func (w PostgresEshuSearchDocumentWriter) WriteEshuSearchDocuments(
	ctx context.Context,
	write EshuSearchDocumentWrite,
) (EshuSearchDocumentWriteResult, error) {
	if w.DB == nil {
		return EshuSearchDocumentWriteResult{}, fmt.Errorf("eshu search document database is required")
	}
	scopeID := strings.TrimSpace(write.ScopeID)
	generationID := strings.TrimSpace(write.GenerationID)
	if scopeID == "" || generationID == "" {
		return EshuSearchDocumentWriteResult{}, fmt.Errorf("eshu search document write requires scope and generation")
	}

	now := reducerWriterNow(w.Now)
	writtenIDs := make([]string, 0, len(write.Documents))
	for _, doc := range write.Documents {
		documentID := strings.TrimSpace(doc.ID)
		if documentID == "" {
			return EshuSearchDocumentWriteResult{}, fmt.Errorf("eshu search document requires a document id")
		}
		factID := eshuSearchDocumentFactID(scopeID, generationID, documentID)
		payloadJSON, err := json.Marshal(eshuSearchDocumentPayload(write, doc, factID))
		if err != nil {
			return EshuSearchDocumentWriteResult{}, fmt.Errorf("marshal eshu search document payload: %w", err)
		}
		if _, err := w.DB.ExecContext(
			ctx,
			canonicalReducerFactInsertQuery,
			factID,
			scopeID,
			generationID,
			EshuSearchDocumentFactKind,
			eshuSearchDocumentStableFactKey(scopeID, generationID, documentID),
			reducerFactCollectorKind(write.SourceSystem),
			facts.SourceConfidenceInferred,
			strings.TrimSpace(write.SourceSystem),
			strings.TrimSpace(write.IntentID),
			nil,
			nil,
			now,
			now,
			false,
			payloadJSON,
		); err != nil {
			return EshuSearchDocumentWriteResult{}, fmt.Errorf("write eshu search document fact: %w", err)
		}
		writtenIDs = append(writtenIDs, factID)
	}

	retireResult, err := w.DB.ExecContext(
		ctx,
		eshuSearchDocumentRetireQuery,
		EshuSearchDocumentFactKind,
		scopeID,
		generationID,
		writtenIDs,
	)
	if err != nil {
		return EshuSearchDocumentWriteResult{}, fmt.Errorf("retire stale eshu search documents: %w", err)
	}
	retired := 0
	if retireResult != nil {
		if affected, affErr := retireResult.RowsAffected(); affErr == nil && affected > 0 {
			retired = int(affected)
		}
	}

	return EshuSearchDocumentWriteResult{CanonicalWrites: len(writtenIDs), Retired: retired}, nil
}

// eshuSearchDocumentFactID derives the deterministic fact id for one document.
func eshuSearchDocumentFactID(scopeID string, generationID string, documentID string) string {
	return EshuSearchDocumentFactKind + ":" + facts.StableID(
		EshuSearchDocumentFactKind,
		eshuSearchDocumentIdentity(scopeID, generationID, documentID),
	)
}

// eshuSearchDocumentStableFactKey is the human-traceable uniqueness key.
func eshuSearchDocumentStableFactKey(scopeID string, generationID string, documentID string) string {
	return strings.Join([]string{
		"eshu_search_document",
		scopeID,
		generationID,
		documentID,
	}, ":")
}

func eshuSearchDocumentIdentity(scopeID string, generationID string, documentID string) map[string]any {
	return map[string]any{
		"scope_id":      scopeID,
		"generation_id": generationID,
		"document_id":   documentID,
	}
}

// eshuSearchDocumentPayload is the JSON fact payload for one curated document.
// It records the durable identity alongside the document so a reader can both
// filter and reconstruct the document without a join to the source tables.
func eshuSearchDocumentPayload(write EshuSearchDocumentWrite, doc searchdocs.Document, factID string) map[string]any {
	return map[string]any{
		"reducer_domain": string(DomainEshuSearchDocument),
		"scope_id":       strings.TrimSpace(write.ScopeID),
		"generation_id":  strings.TrimSpace(write.GenerationID),
		"fact_id":        factID,
		"document_id":    strings.TrimSpace(doc.ID),
		"repo_id":        strings.TrimSpace(doc.RepoID),
		"source_kind":    string(doc.SourceKind),
		"document":       doc,
	}
}
