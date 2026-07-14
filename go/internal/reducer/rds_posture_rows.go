// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	rdsPostureResourceTypeInstance = "aws_rds_db_instance"
	rdsPostureResourceTypeCluster  = "aws_rds_db_cluster"

	rdsPosturePublicCandidate = "candidate_public_endpoint"
	rdsPostureNotPublic       = "not_public_endpoint"

	rdsPostureSkipSourceUnresolved = "source_unresolved"
)

type rdsPostureTally struct {
	updated int
	skipped map[string]int
}

func newRDSPostureTally() rdsPostureTally {
	return rdsPostureTally{skipped: make(map[string]int)}
}

func (t rdsPostureTally) totalSkipped() int {
	total := 0
	for _, count := range t.skipped {
		total += count
	}
	return total
}

// ExtractRDSPostureRows projects rds_instance_posture facts into deterministic
// CloudResource node-property rows. A posture fact only produces a row when the
// same scope generation also emitted an aws_resource fact for the RDS DB
// instance or Aurora cluster; otherwise the fact is counted as source
// unresolved and no node uid is fabricated.
func ExtractRDSPostureRows(
	resourceEnvelopes []facts.Envelope,
	postureEnvelopes []facts.Envelope,
) ([]map[string]any, rdsPostureTally, []quarantinedFact, error) {
	tally := newRDSPostureTally()
	if len(postureEnvelopes) == 0 {
		return nil, tally, nil, nil
	}

	index, quarantined, err := buildRDSPostureResourceIndex(resourceEnvelopes)
	if err != nil {
		return nil, tally, nil, err
	}
	byUID := make(map[string]map[string]any, len(postureEnvelopes))
	for _, env := range postureEnvelopes {
		if env.FactKind != facts.RDSInstancePostureFactKind {
			continue
		}
		row, uid, ok, err := rdsPostureRow(env)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return nil, tally, quarantined, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}
		if !ok {
			tally.skipped[rdsPostureSkipSourceUnresolved]++
			continue
		}
		if _, exists := index[uid]; !exists {
			tally.skipped[rdsPostureSkipSourceUnresolved]++
			continue
		}
		// Last fact for the uid wins for mutable posture properties; the uid
		// identity remains stable and the writer's MATCH+SET is idempotent.
		byUID[uid] = row
	}

	if len(byUID) == 0 {
		return nil, tally, quarantined, nil
	}

	uids := make([]string, 0, len(byUID))
	for uid := range byUID {
		uids = append(uids, uid)
	}
	sort.Strings(uids)

	rows := make([]map[string]any, 0, len(uids))
	for _, uid := range uids {
		rows = append(rows, byUID[uid])
	}
	tally.updated = len(rows)
	return rows, tally, quarantined, nil
}

func buildRDSPostureResourceIndex(envelopes []facts.Envelope) (map[string]struct{}, []quarantinedFact, error) {
	index := make(map[string]struct{}, len(envelopes))
	var quarantined []quarantinedFact
	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind {
			continue
		}
		resource, err := decodeAWSResource(env)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return nil, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}
		if !isRDSPostureResourceType(resource.ResourceType) {
			continue
		}
		resourceID := resource.ResourceID
		if resourceID == "" {
			resourceID = derefString(resource.ARN)
		}
		if resourceID == "" {
			continue
		}
		uid := cloudResourceUID(resource.AccountID, resource.Region, resource.ResourceType, resourceID)
		index[uid] = struct{}{}
	}
	return index, quarantined, nil
}

// rdsPostureRow decodes one rds_instance_posture envelope through the
// contracts seam and builds its CloudResource node-property row. It returns
// ok=false (with a nil error) when the fact decodes cleanly but carries
// neither a graph-projectable resource type nor a resource id/arn to derive a
// stable uid from — a valid-but-incomplete fact, not a malformed one, so it is
// a conservative skip (rdsPostureSkipSourceUnresolved), never a quarantine. A
// non-nil error is always a decode failure (a missing/null required field such
// as account_id or region); the caller routes it through
// partitionDecodeFailures so it dead-letters as input_invalid instead of
// silently zeroing the identity, per Contract System v1.
func rdsPostureRow(env facts.Envelope) (map[string]any, string, bool, error) {
	posture, err := decodeRDSInstancePosture(env)
	if err != nil {
		return nil, "", false, err
	}

	resourceType := derefString(posture.ResourceType)
	resourceID := derefString(posture.ResourceID)
	if resourceID == "" {
		resourceID = derefString(posture.ARN)
	}
	if !isRDSPostureResourceType(resourceType) || resourceID == "" {
		return nil, "", false, nil
	}

	publicState := rdsPostureNotPublic
	if posture.PubliclyAccessible {
		publicState = rdsPosturePublicCandidate
	}

	var securityParameters map[string]string
	if posture.SecurityParameters != nil {
		securityParameters = *posture.SecurityParameters
	}

	uid := cloudResourceUID(posture.AccountID, posture.Region, resourceType, resourceID)
	row := map[string]any{
		"uid":                       uid,
		"rds_identifier":            derefString(posture.Identifier),
		"rds_resource_type":         resourceType,
		"rds_engine":                derefString(posture.Engine),
		"rds_publicly_accessible":   posture.PubliclyAccessible,
		"rds_public_exposure_state": publicState,
		"rds_storage_encrypted":     posture.StorageEncrypted,
		"rds_kms_key_id":            derefString(posture.KMSKeyID),
		"rds_iam_database_authentication_enabled": posture.IAMDatabaseAuthenticationEnabled,
		"rds_multi_az":                            posture.MultiAZ,
		"rds_deletion_protection":                 posture.DeletionProtection,
		"rds_backup_retention_period":             int64(posture.BackupRetentionPeriod),
		"rds_performance_insights_enabled":        posture.PerformanceInsightsEnabled,
		"rds_performance_insights_retention_days": int64(posture.PerformanceInsightsRetentionDays),
		"rds_performance_insights_kms_key_id":     derefString(posture.PerformanceInsightsKMSKeyID),
		"rds_ca_certificate_identifier":           derefString(posture.CACertificateIdentifier),
		"rds_parameter_groups":                    uniqueSortedStrings(posture.ParameterGroups),
		"rds_option_groups":                       uniqueSortedStrings(posture.OptionGroups),
		"rds_security_parameters":                 rdsPostureSecurityParameters(securityParameters),
		"source_fact_id":                          env.FactID,
	}
	return row, uid, true, nil
}

func isRDSPostureResourceType(resourceType string) bool {
	switch strings.TrimSpace(resourceType) {
	case rdsPostureResourceTypeInstance, rdsPostureResourceTypeCluster:
		return true
	default:
		return false
	}
}

// rdsPostureSecurityParameters flattens the typed, optional
// security_parameters map into the row's deterministic "key=value" string
// list, matching the pre-typing raw-payload derivation byte-for-byte: a nil
// map (the field was absent from the payload) returns nil, and every entry is
// trimmed, empty keys/values dropped, then deduplicated and sorted. The caller
// dereferences the struct's optional *map[string]string field (nil pointer
// distinguishes an absent key from an observed-empty map) into the plain map
// this function takes; maps are already reference types, so accepting a
// pointer-to-map parameter here would be redundant indirection.
func rdsPostureSecurityParameters(params map[string]string) []string {
	if params == nil {
		return nil
	}
	values := make([]string, 0, len(params))
	for key, value := range params {
		values = appendRDSPostureKeyValue(values, key, value)
	}
	return uniqueSortedStrings(values)
}

func appendRDSPostureKeyValue(values []string, key string, value string) []string {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return values
	}
	return append(values, key+"="+value)
}
