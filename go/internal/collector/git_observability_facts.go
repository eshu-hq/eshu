// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const observabilityRedactionVersion = "observability-redaction-v1"

var observabilitySourceBuckets = []struct {
	bucket string
	kind   string
}{
	{bucket: "observability_declared_folders", kind: facts.ObservabilityDeclaredFolderFactKind},
	{bucket: "observability_declared_dashboards", kind: facts.ObservabilityDeclaredDashboardFactKind},
	{bucket: "observability_declared_datasources", kind: facts.ObservabilityDeclaredDatasourceFactKind},
	{bucket: "observability_declared_alert_rules", kind: facts.ObservabilityDeclaredAlertRuleFactKind},
	{bucket: "observability_declared_scrape_configs", kind: facts.ObservabilityDeclaredScrapeConfigFactKind},
	{bucket: "observability_declared_metric_rules", kind: facts.ObservabilityDeclaredMetricRuleFactKind},
	{bucket: "observability_declared_metric_routes", kind: facts.ObservabilityDeclaredMetricRouteFactKind},
	{bucket: "observability_declared_log_routes", kind: facts.ObservabilityDeclaredLogRouteFactKind},
	{bucket: "observability_declared_trace_routes", kind: facts.ObservabilityDeclaredTraceRouteFactKind},
	{bucket: "observability_applied_resources", kind: facts.ObservabilityAppliedResourceFactKind},
	{bucket: "observability_applied_sync_states", kind: facts.ObservabilityAppliedSyncStateFactKind},
	{bucket: "observability_coverage_warnings", kind: facts.ObservabilityCoverageWarningFactKind},
}

var forbiddenObservabilityPayloadKeys = map[string]struct{}{
	"actions":                  {},
	"basicAuthPassword":        {},
	"config_json":              {},
	"cluster_server":           {},
	"dashboard_json":           {},
	"data":                     {},
	"endpoint":                 {},
	"expr":                     {},
	"headers":                  {},
	"include":                  {},
	"job_name":                 {},
	"json":                     {},
	"jobName":                  {},
	"labels":                   {},
	"matchLabels":              {},
	"model":                    {},
	"panels":                   {},
	"password":                 {},
	"queries":                  {},
	"query":                    {},
	"resourceVersion":          {},
	"secureJsonData":           {},
	"secure_json_data_encoded": {},
	"serverSnippet":            {},
	"scrape_configs":           {},
	"span_id":                  {},
	"spanID":                   {},
	"spans":                    {},
	"static_configs":           {},
	"staticConfigs":            {},
	"tags":                     {},
	"tenant_id":                {},
	"targets":                  {},
	"title":                    {},
	"trace_id":                 {},
	"traceID":                  {},
	"traces":                   {},
	"url":                      {},
}

func observabilityFactCount(fileData []map[string]any) int {
	count := 0
	for _, file := range fileData {
		fileCount := 0
		for _, mapping := range observabilitySourceBuckets {
			fileCount += len(observabilityRows(file, mapping.bucket))
		}
		if fileCount > 0 {
			count += fileCount + 1
		}
	}
	return count
}

func emitObservabilityFactsForFile(
	ch chan<- facts.Envelope,
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	fileData map[string]any,
	sourceRevision string,
) {
	relativePath := repositoryRelativePath(repoPath, payloadPath(fileData, "path"))
	rowCount := observabilityFactCount([]map[string]any{fileData})
	if rowCount == 0 {
		return
	}
	sourceClass := observabilitySourceClassForFile(fileData)
	ch <- observabilitySourceInstanceEnvelope(
		repoPath, repoID, scopeID, generationID, observedAt, relativePath, sourceRevision, sourceClass, rowCount-1,
	)
	for _, mapping := range observabilitySourceBuckets {
		for _, row := range observabilityRows(fileData, mapping.bucket) {
			ch <- observabilityRowEnvelope(
				mapping.kind, repoPath, repoID, scopeID, generationID, observedAt, relativePath, sourceRevision, row,
			)
		}
	}
}

func observabilitySourceInstanceEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	relativePath string,
	sourceRevision string,
	sourceClass string,
	sourceFactCount int,
) facts.Envelope {
	sourceInstanceID := repoID + ":" + relativePath
	payload := observabilityBasePayload(repoID, scopeID, generationID, observedAt, relativePath, sourceRevision)
	payload["source_class"] = firstNonEmptyString(sourceClass, "declared")
	payload["name"] = "source." + relativePath
	payload["source_kind"] = "git"
	payload["source_instance_id"] = sourceInstanceID
	payload["source_fact_count"] = sourceFactCount
	payload["outcome"] = "exact"
	factKey := observabilityFactKey(facts.ObservabilitySourceInstanceFactKind, sourceInstanceID, generationID)
	envelope := factEnvelope(
		facts.ObservabilitySourceInstanceFactKind,
		scopeID,
		generationID,
		observedAt,
		factKey,
		payload,
		filepath.Join(repoPath, filepath.FromSlash(relativePath)),
	)
	envelope.SchemaVersion = facts.ObservabilitySchemaVersionV1
	return envelope
}

func observabilityRowEnvelope(
	factKind string,
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	relativePath string,
	sourceRevision string,
	row map[string]any,
) facts.Envelope {
	payload := observabilityBasePayload(repoID, scopeID, generationID, observedAt, relativePath, sourceRevision)
	for key, value := range row {
		if key == "cluster_server" {
			if fingerprint := observabilityFingerprint(payloadString(row, key)); fingerprint != "" {
				payload["cluster_server_fingerprint"] = fingerprint
			}
			continue
		}
		if key == "source_revision" && payloadString(row, key) == "unknown" {
			continue
		}
		if observabilityPayloadKeyForbidden(key) {
			continue
		}
		payload[key] = value
	}
	payload["source_instance_id"] = repoID + ":" + relativePath
	if payload["outcome"] == nil {
		payload["outcome"] = "derived"
	}
	sourceKind := payloadString(payload, "source_kind")
	if sourceKind == "" {
		payload["source_kind"] = "git"
	}
	factKey := observabilityFactKey(factKind, observabilityRecordIdentity(payload), generationID)
	envelope := factEnvelope(
		factKind,
		scopeID,
		generationID,
		observedAt,
		factKey,
		payload,
		filepath.Join(repoPath, filepath.FromSlash(relativePath)),
	)
	envelope.SchemaVersion = facts.ObservabilitySchemaVersionV1
	return envelope
}

func observabilityBasePayload(
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	relativePath string,
	sourceRevision string,
) map[string]any {
	payload := map[string]any{
		"repo_id":           repoID,
		"relative_path":     relativePath,
		"source_class":      "declared",
		"scope_id":          scopeID,
		"generation_id":     generationID,
		"observed_at":       observedAt.UTC().Format(time.RFC3339Nano),
		"freshness_state":   "current",
		"redaction_version": observabilityRedactionVersion,
		"source_revision":   firstNonEmptyString(sourceRevision, "unknown"),
	}
	payload["provenance"] = map[string]any{
		"repo_id":         repoID,
		"relative_path":   relativePath,
		"source_revision": firstNonEmptyString(sourceRevision, "unknown"),
	}
	return payload
}

func observabilityRows(fileData map[string]any, bucket string) []map[string]any {
	switch typed := fileData[bucket].(type) {
	case []map[string]any:
		return typed
	case []any:
		rows := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			row, ok := item.(map[string]any)
			if ok {
				rows = append(rows, row)
			}
		}
		return rows
	default:
		return nil
	}
}

func observabilitySourceClassForFile(fileData map[string]any) string {
	classes := map[string]struct{}{}
	for _, mapping := range observabilitySourceBuckets {
		for _, row := range observabilityRows(fileData, mapping.bucket) {
			if sourceClass := strings.TrimSpace(payloadString(row, "source_class")); sourceClass != "" {
				classes[sourceClass] = struct{}{}
			}
		}
	}
	if len(classes) == 0 {
		return "declared"
	}
	for _, candidate := range []string{"applied", "observed", "declared"} {
		if _, ok := classes[candidate]; ok {
			return candidate
		}
	}
	for sourceClass := range classes {
		return sourceClass
	}
	return "declared"
}

func observabilityPayloadKeyForbidden(key string) bool {
	if _, forbidden := forbiddenObservabilityPayloadKeys[key]; forbidden {
		return true
	}
	lower := strings.ToLower(key)
	return strings.Contains(lower, "password") ||
		strings.Contains(lower, "private") ||
		strings.Contains(lower, "secret") ||
		strings.Contains(lower, "token")
}

func observabilityFingerprint(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.ToLower(value)))
	return hex.EncodeToString(sum[:])[:16]
}

func observabilityFactKey(factKind string, identity string, generationID string) string {
	return strings.Join([]string{factKind, identity, generationID}, ":")
}

func observabilityRecordIdentity(payload map[string]any) string {
	keys := []string{
		"source_class", "source_kind", "source_instance_id", "relative_path",
		"folder_uid", "folder_title_fingerprint", "dashboard_uid", "datasource_uid", "alert_rule_uid", "warning_kind",
		"selector_identity_fingerprint", "job_name_fingerprint", "rule_group", "rule_kind", "alert_rule_name_fingerprint", "record_rule_name_fingerprint",
		"pipeline_name", "backend_kind", "exporter_refs", "processor_refs", "receiver_refs", "route_destination_fingerprint", "tenant_id_fingerprint", "label_identity_fingerprint",
		"connector_refs", "trace_tag_identity_fingerprint", "traces_to_logs_datasource_uid", "traces_to_metrics_datasource_uid", "service_map_datasource_uid",
		"app_name", "app_namespace", "cluster_name", "resource_identity", "resource_identity_fingerprint", "observability_resource_class",
		"sync_status", "health_status", "operation_phase", "resource_uid_fingerprint",
		"name", "resource_kind", "resource_name", "resource_namespace", "config_key",
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if value := payloadString(payload, key); value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	sort.Strings(parts)
	if len(parts) == 0 {
		return fmt.Sprint(payload)
	}
	return strings.Join(parts, "|")
}

func commitSHAByRelativePath(repoPath string, snapshot *RepositorySnapshot) map[string]string {
	result := make(map[string]string, len(snapshot.ContentFileMetas)+len(snapshot.ContentFiles))
	for _, meta := range snapshot.ContentFileMetas {
		if strings.TrimSpace(meta.CommitSHA) != "" {
			result[meta.RelativePath] = meta.CommitSHA
		}
	}
	for _, file := range snapshot.ContentFiles {
		if strings.TrimSpace(file.CommitSHA) != "" {
			result[file.RelativePath] = file.CommitSHA
		}
	}
	for _, fileData := range snapshot.FileData {
		if revision := payloadString(fileData, "commit_sha", "source_revision"); revision != "" {
			relativePath := repositoryRelativePath(repoPath, payloadPath(fileData, "path"))
			result[relativePath] = revision
		}
	}
	return result
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
