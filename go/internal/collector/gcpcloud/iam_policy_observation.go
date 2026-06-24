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

// maxIAMPolicyMembers bounds the number of member bindings carried on one
// gcp_iam_policy_observation fact so a large binding cannot emit an unbounded
// payload.
const maxIAMPolicyMembers = 500

const (
	redactionReasonIAMEtag      = "gcp_iam_etag"
	redactionReasonIAMCondition = "gcp_iam_condition"
)

// IAMPolicyObservation is one GCP IAM role binding observed on a resource. The
// collector keeps the role and condition presence as evidence and fingerprints
// every member by class; it never carries raw policy JSON or raw member
// identities.
type IAMPolicyObservation struct {
	// Boundary carries the scope and generation contract fields.
	Boundary Boundary
	// FullResourceName is the CAI full resource name the policy is attached to.
	FullResourceName string
	// AssetType is the CAI asset type of the resource.
	AssetType string
	// Role is the bounded IAM role (e.g. roles/storage.admin).
	Role string
	// Members are raw IAM members; each is fingerprinted by class.
	Members []string
	// ConditionPresent reports whether the binding carries an IAM condition.
	ConditionPresent bool
	// ConditionFingerprintInput is a raw condition body or stable condition
	// signature used only to produce a keyed fingerprint. It is never persisted.
	ConditionFingerprintInput string
	// Etag is the raw policy etag; it is fingerprinted when present.
	Etag string
	// UpdateTime is the read/update time.
	UpdateTime time.Time
	// SourceRecordID overrides the default record id.
	SourceRecordID string
	// SourceURI is the bounded source URI.
	SourceURI string
}

// NewIAMPolicyObservationEnvelope builds the durable gcp_iam_policy_observation
// fact for one role binding. Every member is recorded as its class plus a keyed
// fingerprint (never the raw member email/identity); the role and condition
// presence/fingerprint are bounded evidence; no raw policy JSON is carried. The
// stable fact key is derived from the resource identity, asset type, role, and
// condition fingerprint so same-role conditional bindings do not collide.
//
// It fails closed on a missing resource name, a missing role, no members, or a
// zero redaction key.
func NewIAMPolicyObservationEnvelope(obs IAMPolicyObservation, key redact.Key) (facts.Envelope, error) {
	if err := validateBoundary(obs.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	if key.IsZero() {
		return facts.Envelope{}, fmt.Errorf("gcp iam policy observation requires a redaction key")
	}
	fullName := strings.TrimSpace(obs.FullResourceName)
	if fullName == "" {
		return facts.Envelope{}, fmt.Errorf("gcp iam policy observation requires full_resource_name")
	}
	assetType := strings.TrimSpace(obs.AssetType)
	if assetType == "" {
		return facts.Envelope{}, fmt.Errorf("gcp iam policy observation requires asset_type")
	}
	role := strings.TrimSpace(obs.Role)
	if role == "" {
		return facts.Envelope{}, fmt.Errorf("gcp iam policy observation requires role")
	}
	members, memberTruncated := fingerprintIAMMembers(obs.Members, key)
	if len(members) == 0 {
		return facts.Envelope{}, fmt.Errorf("gcp iam policy observation requires at least one member")
	}
	conditionFingerprint := fingerprintIAMCondition(obs.ConditionFingerprintInput, key)

	stableKey := facts.StableID(facts.GCPIAMPolicyObservationFactKind, map[string]any{
		"full_resource_name":    fullName,
		"asset_type":            assetType,
		"role":                  role,
		"condition_fingerprint": conditionFingerprint,
		"content_family":        obs.Boundary.ContentFamily,
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
		"role":                     role,
		"members":                  members,
		"member_count":             len(members),
		"member_truncated":         memberTruncated,
		"condition_present":        obs.ConditionPresent,
		"condition_fingerprint":    conditionFingerprint,
		"read_time":                timeOrNil(obs.Boundary.ReadTime),
		"update_time":              timeOrNil(obs.UpdateTime.UTC()),
		"redaction_policy_version": RedactionPolicyVersion,
	}
	if etag := strings.TrimSpace(obs.Etag); etag != "" {
		payload["etag_fingerprint"] = redact.String(etag, redactionReasonIAMEtag, "gcp_iam_etag", key).Marker
	}

	return newEnvelope(
		obs.Boundary,
		facts.GCPIAMPolicyObservationFactKind,
		facts.GCPIAMPolicyObservationSchemaVersion,
		stableKey,
		sourceRecordID(obs.SourceRecordID, fullName+"|"+role),
		obs.SourceURI,
		payload,
	), nil
}

func fingerprintIAMCondition(condition string, key redact.Key) string {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return ""
	}
	return redact.String(condition, redactionReasonIAMCondition, "gcp_iam_condition", key).Marker
}

// fingerprintIAMMembers records each non-blank member as its class plus a keyed
// fingerprint, de-duplicating by fingerprint, sorting deterministically, and
// bounding the set. The raw member identity is never carried.
func fingerprintIAMMembers(members []string, key redact.Key) ([]map[string]string, bool) {
	seen := make(map[string]map[string]string)
	for _, member := range members {
		trimmed := strings.TrimSpace(member)
		if trimmed == "" {
			continue
		}
		fingerprint := FingerprintMember(trimmed, key)
		if _, ok := seen[fingerprint]; ok {
			continue
		}
		seen[fingerprint] = map[string]string{
			"member_class":       MemberClass(trimmed),
			"member_fingerprint": fingerprint,
		}
	}
	if len(seen) == 0 {
		return nil, false
	}
	fingerprints := make([]string, 0, len(seen))
	for fp := range seen {
		fingerprints = append(fingerprints, fp)
	}
	sort.Strings(fingerprints)
	truncated := false
	if len(fingerprints) > maxIAMPolicyMembers {
		fingerprints = fingerprints[:maxIAMPolicyMembers]
		truncated = true
	}
	out := make([]map[string]string, 0, len(fingerprints))
	for _, fp := range fingerprints {
		out = append(out, seen[fp])
	}
	return out, truncated
}
