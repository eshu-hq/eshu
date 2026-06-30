// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// ExtensionSchemaVersionDefault is the default provider-specific extension
// schema version stamped on a gcp_cloud_resource fact when the observation does
// not supply its own version. The extension object is versioned independently
// from the fact schema so safe control-plane metadata can evolve without a fact
// schema bump.
const ExtensionSchemaVersionDefault = "1.0.0"

// NewCloudResourceEnvelope builds the durable gcp_cloud_resource fact for one
// Cloud Asset Inventory resource observation.
//
// Raw provider identity (the CAI full resource name) is preserved verbatim for
// exact reducer joins, alongside normalized asset type, project id/number,
// folder and organization ancestors, location, and source timestamps. Label
// values named for fingerprinting are replaced with keyed redaction markers; the
// versioned extension object carries only safe control-plane metadata. The
// builder never accepts or persists raw IAM policy JSON, secret values, object
// contents, or data-plane records.
func NewCloudResourceEnvelope(boundary Boundary, obs ResourceObservation, key redact.Key) (facts.Envelope, error) {
	if err := validateBoundary(boundary); err != nil {
		return facts.Envelope{}, err
	}
	fullName := strings.TrimSpace(obs.Name)
	if fullName == "" {
		return facts.Envelope{}, fmt.Errorf("gcp resource observation requires full_resource_name")
	}
	assetType := strings.TrimSpace(obs.AssetType)
	if assetType == "" {
		return facts.Envelope{}, fmt.Errorf("gcp resource observation requires asset_type")
	}

	ancestry := NormalizeAncestry(obs.Ancestors)
	projectID := strings.TrimSpace(ProjectIDFromFullName(fullName))
	updateTime := obs.UpdateTime.UTC()

	stableKey := facts.StableID(facts.GCPCloudResourceFactKind, map[string]any{
		"asset_type":         assetType,
		"content_family":     boundary.ContentFamily,
		"full_resource_name": fullName,
		"update_time":        stableTimeKey(updateTime),
	})

	labels := FingerprintLabelValues(obs.Labels, fingerprintKeys(obs.LabelFingerprint), key)

	payload := map[string]any{
		"collector_instance_id":    boundary.CollectorInstanceID,
		"parent_scope_kind":        string(boundary.ParentScopeKind),
		"parent_scope_id":          boundary.ParentScopeID,
		"asset_type_family":        boundary.AssetTypeFamily,
		"content_family":           boundary.ContentFamily,
		"location_bucket":          boundary.LocationBucket,
		"full_resource_name":       fullName,
		"asset_type":               assetType,
		"display_name":             strings.TrimSpace(obs.DisplayName),
		"state":                    strings.TrimSpace(obs.State),
		"location":                 strings.TrimSpace(obs.Location),
		"project_id":               projectID,
		"project_number":           ancestry.ProjectNumber,
		"folder_numbers":           ancestry.FolderNumbers,
		"organization_number":      ancestry.OrganizationNumber,
		"ancestors":                ancestry.Chain,
		"labels":                   labels,
		"read_time":                timeOrNil(boundary.ReadTime),
		"update_time":              timeOrNil(updateTime),
		"update_time_present":      !updateTime.IsZero(),
		"redaction_policy_version": RedactionPolicyVersion,
		"extension":                extensionObject(obs),
		"attributes":               cloneAnyMap(obs.Attributes),
		"correlation_anchors":      cloneStrings(obs.CorrelationAnchors),
	}

	return newEnvelope(
		boundary,
		facts.GCPCloudResourceFactKind,
		facts.GCPCloudResourceSchemaVersion,
		stableKey,
		sourceRecordID(obs.SourceRecordID, fullName),
		obs.SourceURI,
		payload,
	), nil
}

// NewCollectionWarningEnvelope builds the durable gcp_collection_warning fact
// for one explicit partial, unsupported, stale, quota, permission-hidden, or
// redaction coverage outcome.
func NewCollectionWarningEnvelope(obs WarningObservation) (facts.Envelope, error) {
	if err := validateBoundary(obs.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	if !ValidWarningKind(obs.WarningKind) {
		return facts.Envelope{}, fmt.Errorf("gcp warning observation has invalid warning_kind %q", obs.WarningKind)
	}
	if !ValidOutcome(obs.Outcome) {
		return facts.Envelope{}, fmt.Errorf("gcp warning observation has invalid outcome %q", obs.Outcome)
	}

	stableKey := facts.StableID(facts.GCPCollectionWarningFactKind, map[string]any{
		"asset_type_family": obs.Boundary.AssetTypeFamily,
		"content_family":    obs.Boundary.ContentFamily,
		"generation_id":     obs.Boundary.GenerationID,
		"outcome":           obs.Outcome,
		"parent_scope_id":   obs.Boundary.ParentScopeID,
		"warning_kind":      obs.WarningKind,
	})

	payload := map[string]any{
		"collector_instance_id": obs.Boundary.CollectorInstanceID,
		"parent_scope_kind":     string(obs.Boundary.ParentScopeKind),
		"parent_scope_id":       obs.Boundary.ParentScopeID,
		"asset_type_family":     obs.Boundary.AssetTypeFamily,
		"content_family":        obs.Boundary.ContentFamily,
		"location_bucket":       obs.Boundary.LocationBucket,
		"warning_kind":          obs.WarningKind,
		"outcome":               obs.Outcome,
		"reason":                strings.TrimSpace(obs.Reason),
		"retryable":             obs.Retryable,
		"hidden_count":          obs.HiddenCount,
		"read_time":             timeOrNil(obs.Boundary.ReadTime),
	}

	return newEnvelope(
		obs.Boundary,
		facts.GCPCollectionWarningFactKind,
		facts.GCPCollectionWarningSchemaVersion,
		stableKey,
		sourceRecordID(obs.SourceRecordID, obs.WarningKind+":"+obs.Outcome),
		obs.SourceURI,
		payload,
	), nil
}

func newEnvelope(
	boundary Boundary,
	factKind string,
	schemaVersion string,
	stableKey string,
	sourceRecordID string,
	sourceURI string,
	payload map[string]any,
) facts.Envelope {
	observedAt := boundary.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	return facts.Envelope{
		FactID:           gcpFactID(factKind, stableKey, boundary.ScopeID, boundary.GenerationID),
		ScopeID:          boundary.ScopeID,
		GenerationID:     boundary.GenerationID,
		FactKind:         factKind,
		StableFactKey:    stableKey,
		SchemaVersion:    schemaVersion,
		CollectorKind:    CollectorKind,
		FencingToken:     boundary.FencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       observedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   CollectorKind,
			ScopeID:        boundary.ScopeID,
			GenerationID:   boundary.GenerationID,
			FactKey:        stableKey,
			SourceURI:      strings.TrimSpace(sourceURI),
			SourceRecordID: sourceRecordID,
		},
	}
}

func gcpFactID(factKind, stableFactKey, scopeID, generationID string) string {
	return facts.StableID("GCPFact", map[string]any{
		"fact_kind":       factKind,
		"generation_id":   generationID,
		"scope_id":        scopeID,
		"stable_fact_key": stableFactKey,
	})
}

func validateBoundary(boundary Boundary) error {
	switch {
	case strings.TrimSpace(boundary.CollectorInstanceID) == "":
		return fmt.Errorf("gcp observation requires collector_instance_id")
	case !boundary.ParentScopeKind.Valid():
		return fmt.Errorf("gcp observation has invalid parent_scope_kind %q", boundary.ParentScopeKind)
	case strings.TrimSpace(boundary.ParentScopeID) == "":
		return fmt.Errorf("gcp observation requires parent_scope_id")
	case strings.TrimSpace(boundary.ScopeID) == "":
		return fmt.Errorf("gcp observation requires scope_id")
	case strings.TrimSpace(boundary.GenerationID) == "":
		return fmt.Errorf("gcp observation requires generation_id")
	case boundary.FencingToken <= 0:
		return fmt.Errorf("gcp observation fencing_token must be positive")
	default:
		return nil
	}
}

func extensionObject(obs ResourceObservation) map[string]any {
	version := strings.TrimSpace(obs.ExtensionVersion)
	if version == "" {
		version = ExtensionSchemaVersionDefault
	}
	ext := map[string]any{"schema_version": version}
	for k, v := range obs.Extension {
		if k == "schema_version" {
			continue
		}
		ext[k] = v
	}
	return ext
}

// cloneAnyMap returns a shallow copy of a bounded attribute map, or nil when the
// input is empty, so an empty extraction omits the attributes field rather than
// emitting an empty object. The extractor only places scalars and string slices
// in the map, so a shallow copy is sufficient to decouple the payload from the
// observation.
func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

func fingerprintKeys(input map[string]string) []string {
	if len(input) == 0 {
		return nil
	}
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	return keys
}

func sourceRecordID(candidate, fallback string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate != "" {
		return candidate
	}
	return strings.TrimSpace(fallback)
}

func stableTimeKey(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func timeOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}
