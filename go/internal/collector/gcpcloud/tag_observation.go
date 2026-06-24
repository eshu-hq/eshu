// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// redactionReasonTagValue marks a fingerprinted GCP tag/label value.
const redactionReasonTagValue = "gcp_tag_value"

// TagObservation is one GCP resource's tag/label evidence. The collector keeps
// the tag keys (not sensitive) and fingerprints every tag value with the
// redaction key; tag value text never reaches durable facts.
type TagObservation struct {
	// Boundary carries the scope and generation contract fields.
	Boundary Boundary
	// FullResourceName is the CAI full resource name carrying the tags.
	FullResourceName string
	// AssetType is the CAI asset type of the resource.
	AssetType string
	// Tags maps tag key -> raw tag value; every value is fingerprinted.
	Tags map[string]string
	// SourceKind is the bounded tag source (e.g. "direct", "effective").
	SourceKind string
	// InheritanceState maps tag key -> bounded inheritance state. It is used for
	// effective tag evidence to distinguish direct from inherited bindings.
	InheritanceState map[string]string
	// UpdateTime is the read/update time.
	UpdateTime time.Time
	// SourceRecordID overrides the default record id.
	SourceRecordID string
	// SourceURI is the bounded source URI.
	SourceURI string
}

// NewTagObservationEnvelope builds the durable gcp_tag_observation fact for one
// resource's tags. Tag keys are preserved; every tag value is fingerprinted with
// the redaction key, so no tag value text crosses into durable facts. The stable
// fact key is derived from the resource identity and content family, so the
// resource's tag fact upserts in place as tags change.
//
// It fails closed on a missing resource name, a missing asset type, no usable
// tags, or a zero redaction key.
func NewTagObservationEnvelope(obs TagObservation, key redact.Key) (facts.Envelope, error) {
	if err := validateBoundary(obs.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	if key.IsZero() {
		return facts.Envelope{}, fmt.Errorf("gcp tag observation requires a redaction key")
	}
	fullName := strings.TrimSpace(obs.FullResourceName)
	if fullName == "" {
		return facts.Envelope{}, fmt.Errorf("gcp tag observation requires full_resource_name")
	}
	assetType := strings.TrimSpace(obs.AssetType)
	if assetType == "" {
		return facts.Envelope{}, fmt.Errorf("gcp tag observation requires asset_type")
	}
	fingerprints, tagKeys := fingerprintTagValues(obs.Tags, key)
	if len(fingerprints) == 0 {
		return facts.Envelope{}, fmt.Errorf("gcp tag observation requires at least one tag")
	}

	stableKey := facts.StableID(facts.GCPTagObservationFactKind, map[string]any{
		"full_resource_name": fullName,
		"asset_type":         assetType,
		"content_family":     obs.Boundary.ContentFamily,
		"source_kind":        strings.TrimSpace(obs.SourceKind),
	})

	payload := map[string]any{
		"collector_instance_id":    obs.Boundary.CollectorInstanceID,
		"parent_scope_kind":        string(obs.Boundary.ParentScopeKind),
		"parent_scope_id":          obs.Boundary.ParentScopeID,
		"asset_type_family":        obs.Boundary.AssetTypeFamily,
		"content_family":           obs.Boundary.ContentFamily,
		"location_bucket":          obs.Boundary.LocationBucket,
		"full_resource_name":       fullName,
		"asset_type":               assetType,
		"project_id":               strings.TrimSpace(ProjectIDFromFullName(fullName)),
		"tag_keys":                 tagKeys,
		"tag_value_fingerprints":   fingerprints,
		"source_kind":              strings.TrimSpace(obs.SourceKind),
		"read_time":                timeOrNil(obs.Boundary.ReadTime),
		"update_time":              timeOrNil(obs.UpdateTime.UTC()),
		"redaction_policy_version": RedactionPolicyVersion,
	}
	if inheritance := cleanInheritanceState(obs.InheritanceState, fingerprints); len(inheritance) > 0 {
		payload["tag_inheritance_state"] = inheritance
	}

	return newEnvelope(
		obs.Boundary,
		facts.GCPTagObservationFactKind,
		facts.GCPTagObservationSchemaVersion,
		stableKey,
		sourceRecordID(obs.SourceRecordID, fullName),
		obs.SourceURI,
		payload,
	), nil
}

// fingerprintTagValues fingerprints every non-blank-keyed tag value and returns
// the fingerprint map plus the sorted tag keys.
func fingerprintTagValues(tags map[string]string, key redact.Key) (map[string]string, []string) {
	if len(tags) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(tags))
	for k, v := range tags {
		trimmedKey := strings.TrimSpace(k)
		if trimmedKey == "" {
			continue
		}
		out[trimmedKey] = redact.String(v, redactionReasonTagValue, "gcp_tag:"+trimmedKey, key).Marker
	}
	if len(out) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return out, keys
}

func cleanInheritanceState(states map[string]string, fingerprints map[string]string) map[string]string {
	if len(states) == 0 || len(fingerprints) == 0 {
		return nil
	}
	out := make(map[string]string, len(states))
	for key, state := range states {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := fingerprints[key]; !ok {
			continue
		}
		state = strings.TrimSpace(state)
		switch state {
		case "direct", "inherited":
			out[key] = state
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
