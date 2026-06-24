// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
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
) ([]map[string]any, rdsPostureTally) {
	tally := newRDSPostureTally()
	if len(postureEnvelopes) == 0 {
		return nil, tally
	}

	index := buildRDSPostureResourceIndex(resourceEnvelopes)
	byUID := make(map[string]map[string]any, len(postureEnvelopes))
	for _, env := range postureEnvelopes {
		if env.FactKind != facts.RDSInstancePostureFactKind {
			continue
		}
		row, uid, ok := rdsPostureRow(env)
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
		return nil, tally
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
	return rows, tally
}

func buildRDSPostureResourceIndex(envelopes []facts.Envelope) map[string]struct{} {
	index := make(map[string]struct{}, len(envelopes))
	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind {
			continue
		}
		resourceType := payloadString(env.Payload, "resource_type")
		if !isRDSPostureResourceType(resourceType) {
			continue
		}
		resourceID := payloadString(env.Payload, "resource_id")
		arn := payloadString(env.Payload, "arn")
		if resourceID == "" {
			resourceID = arn
		}
		if resourceID == "" {
			continue
		}
		uid := cloudResourceUID(
			payloadString(env.Payload, "account_id"),
			payloadString(env.Payload, "region"),
			resourceType,
			resourceID,
		)
		index[uid] = struct{}{}
	}
	return index
}

func rdsPostureRow(env facts.Envelope) (map[string]any, string, bool) {
	accountID := payloadString(env.Payload, "account_id")
	region := payloadString(env.Payload, "region")
	resourceType := payloadString(env.Payload, "resource_type")
	resourceID := payloadString(env.Payload, "resource_id")
	arn := payloadString(env.Payload, "arn")
	if resourceID == "" {
		resourceID = arn
	}
	if !isRDSPostureResourceType(resourceType) || resourceID == "" {
		return nil, "", false
	}

	publiclyAccessible := rdsPosturePayloadBool(env.Payload, "publicly_accessible")
	publicState := rdsPostureNotPublic
	if publiclyAccessible {
		publicState = rdsPosturePublicCandidate
	}

	uid := cloudResourceUID(accountID, region, resourceType, resourceID)
	row := map[string]any{
		"uid":                       uid,
		"rds_identifier":            payloadString(env.Payload, "identifier"),
		"rds_resource_type":         resourceType,
		"rds_engine":                payloadString(env.Payload, "engine"),
		"rds_publicly_accessible":   publiclyAccessible,
		"rds_public_exposure_state": publicState,
		"rds_storage_encrypted":     rdsPosturePayloadBool(env.Payload, "storage_encrypted"),
		"rds_kms_key_id":            payloadString(env.Payload, "kms_key_id"),
		"rds_iam_database_authentication_enabled": rdsPosturePayloadBool(env.Payload, "iam_database_authentication_enabled"),
		"rds_multi_az":                            rdsPosturePayloadBool(env.Payload, "multi_az"),
		"rds_deletion_protection":                 rdsPosturePayloadBool(env.Payload, "deletion_protection"),
		"rds_backup_retention_period":             rdsPosturePayloadInt64(env.Payload, "backup_retention_period"),
		"rds_performance_insights_enabled":        rdsPosturePayloadBool(env.Payload, "performance_insights_enabled"),
		"rds_performance_insights_retention_days": rdsPosturePayloadInt64(env.Payload, "performance_insights_retention_days"),
		"rds_performance_insights_kms_key_id":     payloadString(env.Payload, "performance_insights_kms_key_id"),
		"rds_ca_certificate_identifier":           payloadString(env.Payload, "ca_certificate_identifier"),
		"rds_parameter_groups":                    rdsPosturePayloadStringSlice(env.Payload, "parameter_groups"),
		"rds_option_groups":                       rdsPosturePayloadStringSlice(env.Payload, "option_groups"),
		"rds_security_parameters":                 rdsPosturePayloadKeyValueStrings(env.Payload, "security_parameters"),
		"source_fact_id":                          env.FactID,
	}
	return row, uid, true
}

func isRDSPostureResourceType(resourceType string) bool {
	switch strings.TrimSpace(resourceType) {
	case rdsPostureResourceTypeInstance, rdsPostureResourceTypeCluster:
		return true
	default:
		return false
	}
}

func rdsPosturePayloadBool(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok || value == nil {
		return false
	}
	boolValue, ok := value.(bool)
	return ok && boolValue
}

func rdsPosturePayloadInt64(payload map[string]any, key string) int64 {
	value, ok := payload[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int8:
		return int64(typed)
	case int16:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case uint:
		return int64(typed)
	case uint8:
		return int64(typed)
	case uint16:
		return int64(typed)
	case uint32:
		return int64(typed)
	case uint64:
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

func rdsPosturePayloadStringSlice(payload map[string]any, key string) []string {
	raw, ok := payload[key]
	if !ok || raw == nil {
		return nil
	}
	var values []string
	switch typed := raw.(type) {
	case []string:
		values = append(values, typed...)
	case []any:
		for _, value := range typed {
			values = append(values, fmt.Sprint(value))
		}
	}
	return uniqueSortedStrings(values)
}

func rdsPosturePayloadKeyValueStrings(payload map[string]any, key string) []string {
	raw, ok := payload[key]
	if !ok || raw == nil {
		return nil
	}
	values := make([]string, 0)
	switch typed := raw.(type) {
	case map[string]string:
		for key, value := range typed {
			values = appendRDSPostureKeyValue(values, key, value)
		}
	case map[string]any:
		for key, value := range typed {
			values = appendRDSPostureKeyValue(values, key, fmt.Sprint(value))
		}
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
