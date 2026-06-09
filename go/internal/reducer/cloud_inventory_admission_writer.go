package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// cloudInventoryAdmissionFactKind is the reducer-owned canonical CloudResource
// read-model fact kind emitted by the shared cloud-inventory admission path.
const cloudInventoryAdmissionFactKind = "reducer_cloud_resource_identity"

// PostgresCloudInventoryAdmissionWriter persists admitted canonical
// CloudResource identity rows into the shared fact store. It is idempotent by
// canonical uid within the scope generation: the fact id is derived from the
// uid, scope, and generation, so reducer retries and concurrent workers upsert
// the same row through ON CONFLICT instead of duplicating canonical truth.
type PostgresCloudInventoryAdmissionWriter struct {
	// DB executes the canonical reducer fact upsert.
	DB workloadIdentityExecer
	// Now supplies the observed/ingested timestamp; defaults to time.Now.
	Now func() time.Time
}

// WriteCloudInventoryAdmission stores one durable canonical fact per admitted
// resource. Records that did not resolve into a uid are not persisted as
// canonical rows; the caller's summary counts them instead so ambiguous,
// unsupported, and unresolved identities are never fabricated into truth.
func (w PostgresCloudInventoryAdmissionWriter) WriteCloudInventoryAdmission(
	ctx context.Context,
	write CloudInventoryAdmissionWrite,
) (CloudInventoryAdmissionWriteResult, error) {
	if w.DB == nil {
		return CloudInventoryAdmissionWriteResult{}, fmt.Errorf("cloud inventory admission database is required")
	}

	now := reducerWriterNow(w.Now)
	canonicalIDs := make([]string, 0, len(write.Resources))
	for _, resource := range write.Resources {
		payloadJSON, err := json.Marshal(cloudInventoryAdmissionPayload(write, resource))
		if err != nil {
			return CloudInventoryAdmissionWriteResult{}, fmt.Errorf("marshal cloud inventory admission payload: %w", err)
		}

		if _, err := w.DB.ExecContext(
			ctx,
			canonicalReducerFactInsertQuery,
			cloudInventoryAdmissionFactID(write, resource),
			write.ScopeID,
			write.GenerationID,
			cloudInventoryAdmissionFactKind,
			cloudInventoryAdmissionStableFactKey(write, resource),
			reducerFactCollectorKind(resource.Provider),
			facts.SourceConfidenceInferred,
			write.SourceSystem,
			write.IntentID,
			nil,
			nil,
			now,
			now,
			false,
			payloadJSON,
		); err != nil {
			return CloudInventoryAdmissionWriteResult{}, fmt.Errorf("write cloud inventory admission fact: %w", err)
		}
		canonicalIDs = append(canonicalIDs, resource.CloudResourceUID)
	}

	return CloudInventoryAdmissionWriteResult{
		CanonicalIDs:    canonicalIDs,
		CanonicalWrites: len(canonicalIDs),
		EvidenceSummary: fmt.Sprintf("wrote cloud inventory canonical identities %d", len(canonicalIDs)),
	}, nil
}

// cloudInventoryAdmissionStableFactKey is the idempotency key inside the scope
// generation. The canonical uid partitions it, so two workers admitting the
// same resource derive the same key and converge.
func cloudInventoryAdmissionStableFactKey(write CloudInventoryAdmissionWrite, resource AdmittedCloudResource) string {
	return strings.Join([]string{
		"cloud_resource_identity",
		strings.TrimSpace(write.ScopeID),
		strings.TrimSpace(write.GenerationID),
		strings.TrimSpace(resource.CloudResourceUID),
	}, ":")
}

// cloudInventoryAdmissionFactID derives the deterministic fact id used as the
// ON CONFLICT key. It hashes the stable fact key so the same admitted uid in
// the same generation always upserts one row regardless of worker or retry.
func cloudInventoryAdmissionFactID(write CloudInventoryAdmissionWrite, resource AdmittedCloudResource) string {
	return cloudInventoryAdmissionFactKind + ":" + facts.StableID(
		cloudInventoryAdmissionFactKind,
		map[string]any{
			"scope_id":           strings.TrimSpace(write.ScopeID),
			"generation_id":      strings.TrimSpace(write.GenerationID),
			"cloud_resource_uid": strings.TrimSpace(resource.CloudResourceUID),
		},
	)
}

// cloudInventoryAdmissionPayload builds the canonical CloudResource read-model
// payload. It preserves the evidence layer flags and management origin so a
// reader can see that an observed fact did not overwrite declared truth.
func cloudInventoryAdmissionPayload(
	write CloudInventoryAdmissionWrite,
	resource AdmittedCloudResource,
) map[string]any {
	return map[string]any{
		"reducer_domain":        string(DomainCloudInventoryAdmission),
		"intent_id":             write.IntentID,
		"scope_id":              write.ScopeID,
		"generation_id":         write.GenerationID,
		"source_system":         write.SourceSystem,
		"cause":                 write.Cause,
		"cloud_resource_uid":    resource.CloudResourceUID,
		"provider":              resource.Provider,
		"raw_identity":          resource.RawIdentity,
		"resource_type":         resource.ResourceType,
		"source_fact_kinds":     resource.FactKinds,
		"management_origin":     string(resource.ManagementOrigin),
		"has_declared_evidence": resource.HasDeclaredEvidence,
		"has_applied_evidence":  resource.HasAppliedEvidence,
		"has_observed_evidence": resource.HasObservedEvidence,
		"admitted_count":        write.Summary.Admitted,
		"ambiguous_count":       write.Summary.Ambiguous,
		"unsupported_count":     write.Summary.Unsupported,
		"unresolved_count":      write.Summary.Unresolved,
		"publication_fact_kind": cloudInventoryAdmissionFactKind,
		"source_layers": []string{
			string(SourceLayerDeclared),
			string(SourceLayerApplied),
			string(SourceLayerObserved),
		},
	}
}
