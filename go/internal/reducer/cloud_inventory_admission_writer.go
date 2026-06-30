// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	rows := make([]reducerFactRow, 0, len(write.Resources))
	for _, resource := range write.Resources {
		payloadJSON, err := json.Marshal(cloudInventoryAdmissionPayload(write, resource))
		if err != nil {
			return CloudInventoryAdmissionWriteResult{}, fmt.Errorf("marshal cloud inventory admission payload: %w", err)
		}
		rows = append(rows, reducerFactRow{
			FactID:        cloudInventoryAdmissionFactID(write, resource),
			ScopeID:       write.ScopeID,
			GenerationID:  write.GenerationID,
			FactKind:      cloudInventoryAdmissionFactKind,
			StableFactKey: cloudInventoryAdmissionStableFactKey(write, resource),
			// collector_kind varies per resource because each admitted resource
			// can come from a different provider, so it is set per row rather
			// than hoisted out of the loop.
			CollectorKind:    reducerFactCollectorKind(resource.Provider),
			SourceConfidence: facts.SourceConfidenceInferred,
			SourceSystem:     write.SourceSystem,
			SourceFactKey:    write.IntentID,
			ObservedAt:       now,
			IngestedAt:       now,
			Payload:          string(payloadJSON),
		})
		canonicalIDs = append(canonicalIDs, resource.CloudResourceUID)
	}
	// Bounded chunked bulk insert: admitted canonical identities are upserted in
	// O(N/batchSize) round-trips instead of one ExecContext per resource.
	if err := reducerBatchInsertFacts(ctx, w.DB, rows); err != nil {
		return CloudInventoryAdmissionWriteResult{}, fmt.Errorf("write cloud inventory admission fact: %w", err)
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
	payload := cloudInventoryAdmissionBasePayload(write, resource)
	if len(resource.TagValueFingerprints) > 0 {
		// Tag value fingerprints are keyed markers, never raw values, so they are
		// safe to persist and surface for value-blind tag correlation.
		payload["tag_value_fingerprints"] = resource.TagValueFingerprints
	}
	if len(resource.IdentityPolicyEvidence) > 0 {
		payload["identity_policy_evidence"] = cloudIdentityPolicyEvidencePayload(resource.IdentityPolicyEvidence)
	}
	if resource.IdentityPolicyEvidenceTruncated {
		payload["identity_policy_evidence_truncated"] = true
	}
	if len(resource.ResourceChangeEvidence) > 0 {
		payload["resource_change_freshness"] = cloudResourceChangeEvidencePayload(resource.ResourceChangeEvidence)
	}
	if resource.ResourceChangeEvidenceTruncated {
		payload["resource_change_freshness_truncated"] = true
	}
	if len(resource.Attributes) > 0 {
		payload["attributes"] = resource.Attributes
	}
	return payload
}

func cloudIdentityPolicyEvidencePayload(records []CloudIdentityPolicyEvidence) []map[string]string {
	out := make([]map[string]string, 0, len(records))
	for _, record := range records {
		row := map[string]string{}
		addNonBlankIdentityPolicyField(row, "evidence_key", record.EvidenceKey)
		addNonBlankIdentityPolicyField(row, "identity_type", record.IdentityType)
		addNonBlankIdentityPolicyField(row, "role_class", record.RoleClass)
		addNonBlankIdentityPolicyField(row, "principal_fingerprint", record.PrincipalFingerprint)
		addNonBlankIdentityPolicyField(row, "client_fingerprint", record.ClientFingerprint)
		addNonBlankIdentityPolicyField(row, "object_fingerprint", record.ObjectFingerprint)
		addNonBlankIdentityPolicyField(row, "tenant_fingerprint", record.TenantFingerprint)
		if len(row) > 0 {
			out = append(out, row)
		}
	}
	return out
}

func addNonBlankIdentityPolicyField(row map[string]string, key string, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		row[key] = value
	}
}

func cloudResourceChangeEvidencePayload(evidence []CloudResourceChangeEvidence) []map[string]any {
	out := make([]map[string]any, 0, len(evidence))
	for _, row := range evidence {
		out = append(out, map[string]any{
			"evidence_key":               row.EvidenceKey,
			"change_type":                row.ChangeType,
			"change_time":                row.ChangeTime.UTC().Format(time.RFC3339Nano),
			"operation":                  row.Operation,
			"client_type":                row.ClientType,
			"actor_class":                row.ActorClass,
			"actor_fingerprint":          row.ActorFingerprint,
			"changed_property_paths":     row.ChangedPropertyPaths,
			"changed_property_truncated": row.ChangedPropertyTruncated,
			"tombstone_candidate":        row.TombstoneCandidate,
		})
	}
	return out
}

func cloudInventoryAdmissionBasePayload(
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
