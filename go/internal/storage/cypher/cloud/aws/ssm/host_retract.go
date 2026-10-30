// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssm

import (
	"context"
	"fmt"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// Committed-capture reads and supersession retracts for SSM host inventory
// observation nodes. The write path lives in host_writer.go; everything here
// runs host- and evidence-scoped, so a stale or conflicted capture can never
// delete another host's preserved truth. Retract statements run through
// dispatchRetract (sequential Execute, never ExecuteGroup) per the
// #4367/#5128/#5152 NornicDB under-apply precedent.

// ssmHostCommittedCapturesCypher reads back the newest committed
// application capture time for one scope's hosts in a single
// UNWIND-anchored, label-anchored statement. The projection consumes only
// the application capture (platform evidence needs no committed comparison),
// so no platform leg is fetched. The UNWIND binding (candidate_host) and the
// RETURN aliases use distinct names per the NornicDB variable-shadowing
// pitfall. Times return as unix ints so no RFC3339 offset form can corrupt
// the comparison.
const ssmHostCommittedCapturesCypher = `UNWIND $hosts AS candidate_host
MATCH (n:SSMHostApplication)
WHERE n.scope_id = $scope_id
  AND n.partition = candidate_host.partition
  AND n.account_id = candidate_host.account_id
  AND n.region = candidate_host.region
  AND n.instance_id = candidate_host.instance_id
RETURN candidate_host.partition AS host_partition,
       candidate_host.account_id AS host_account_id,
       candidate_host.region AS host_region,
       candidate_host.instance_id AS host_instance_id,
       n.capture_time_unix AS capture_time_unix`

const ssmHostStaleApplicationCountCypher = `MATCH (n:SSMHostApplication)
WHERE n.scope_id = $scope_id
  AND n.evidence_source = $evidence_source
  AND n.partition = $partition
  AND n.account_id = $account_id
  AND n.region = $region
  AND n.instance_id = $instance_id
  AND n.capture_time_unix < $cutoff_unix
RETURN count(n) AS stale_count`

const retractSSMHostStaleApplicationCypher = `MATCH (n:SSMHostApplication)
WHERE n.scope_id = $scope_id
  AND n.evidence_source = $evidence_source
  AND n.partition = $partition
  AND n.account_id = $account_id
  AND n.region = $region
  AND n.instance_id = $instance_id
  AND n.capture_time_unix < $cutoff_unix
DETACH DELETE n`

const ssmHostStalePlatformCountCypher = `MATCH (n:SSMHostPlatform)
WHERE n.scope_id = $scope_id
  AND n.evidence_source = $evidence_source
  AND n.uid IN $uids
RETURN count(n) AS stale_count`

const retractSSMHostPlatformCypher = `MATCH (n:SSMHostPlatform)
WHERE n.scope_id = $scope_id
  AND n.evidence_source = $evidence_source
  AND n.uid IN $uids
DETACH DELETE n`

func (w *HostNodeWriter) ReadCommittedHosts(
	ctx context.Context,
	scopeID string,
	hosts []HostTuple,
) (map[string]CommittedHost, error) {
	committed := make(map[string]CommittedHost, len(hosts))
	if len(hosts) == 0 || w.reader == nil {
		return committed, nil
	}
	params := make([]map[string]any, 0, len(hosts))
	for _, host := range hosts {
		params = append(params, map[string]any{
			"partition":   host.Partition,
			"account_id":  host.AccountID,
			"region":      host.Region,
			"instance_id": host.InstanceID,
		})
	}
	rows, err := w.reader.Run(ctx, ssmHostCommittedCapturesCypher, map[string]any{
		"scope_id": scopeID,
		"hosts":    params,
	})
	if err != nil {
		return nil, fmt.Errorf("ssm host node writer read committed captures: %w", err)
	}
	for _, row := range rows {
		host := HostTuple{
			Partition:  stringVal(row["host_partition"]),
			AccountID:  stringVal(row["host_account_id"]),
			Region:     stringVal(row["host_region"]),
			InstanceID: stringVal(row["host_instance_id"]),
		}
		key := HostKey(host)
		entry := committed[key]
		if unix, ok := intVal(row["capture_time_unix"]); ok {
			capture := time.Unix(unix, 0).UTC()
			if !entry.HasAppCapture || capture.After(entry.AppCapture) {
				entry.AppCapture = capture
				entry.HasAppCapture = true
			}
		}
		committed[key] = entry
	}
	return committed, nil
}

// RetractStaleApplicationNodes deletes one host's application observation
// nodes from captures strictly older than the given cutoff. Same-capture
// rows merge and newer captures are never touched, so a truncated re-read of
// one capture can never delete its siblings.
func (w *HostNodeWriter) RetractStaleApplicationNodes(
	ctx context.Context,
	scopeID string,
	evidenceSource string,
	host HostTuple,
	olderThan time.Time,
) (int, error) {
	if w.executor == nil {
		return 0, fmt.Errorf("ssm host node writer executor is required")
	}
	params := map[string]any{
		"scope_id":        scopeID,
		"evidence_source": evidenceSource,
		"partition":       host.Partition,
		"account_id":      host.AccountID,
		"region":          host.Region,
		"instance_id":     host.InstanceID,
		"cutoff_unix":     olderThan.UTC().Unix(),
	}
	stale, err := w.countStale(ctx, ssmHostStaleApplicationCountCypher, params)
	if err != nil {
		return 0, err
	}
	if stale == 0 {
		return 0, nil
	}
	stmt := sourcecypher.Statement{
		Operation: sourcecypher.OperationCanonicalRetract,
		Cypher:    retractSSMHostStaleApplicationCypher,
		Parameters: withStatementMetadata(params,
			fmt.Sprintf("node_properties=%s retract host capture older than %s", ssmHostApplicationLabel, olderThan.UTC().Format(time.RFC3339))),
	}
	if err := w.dispatchRetract(ctx, stmt); err != nil {
		return 0, err
	}
	return stale, nil
}

// RetractPlatformNodes deletes the listed platform observation uids with
// their pairing edges for one scope and evidence source: the conflicted
// provenance path, where the prior agreed node must go with no replacement.
func (w *HostNodeWriter) RetractPlatformNodes(
	ctx context.Context,
	scopeID string,
	evidenceSource string,
	uids []string,
) (int, error) {
	if len(uids) == 0 {
		return 0, nil
	}
	if w.executor == nil {
		return 0, fmt.Errorf("ssm host node writer executor is required")
	}
	params := map[string]any{
		"scope_id":        scopeID,
		"evidence_source": evidenceSource,
		"uids":            uids,
	}
	stale, err := w.countStale(ctx, ssmHostStalePlatformCountCypher, params)
	if err != nil {
		return 0, err
	}
	if stale == 0 {
		return 0, nil
	}
	stmt := sourcecypher.Statement{
		Operation: sourcecypher.OperationCanonicalRetract,
		Cypher:    retractSSMHostPlatformCypher,
		Parameters: withStatementMetadata(params,
			fmt.Sprintf("node_properties=%s retract uids=%d", ssmHostPlatformLabel, len(uids))),
	}
	if err := w.dispatchRetract(ctx, stmt); err != nil {
		return 0, err
	}
	return stale, nil
}

func (w *HostNodeWriter) countStale(ctx context.Context, cypher string, params map[string]any) (int, error) {
	if w.reader == nil {
		return 0, fmt.Errorf("ssm host node writer reader is required to count stale nodes")
	}
	rows, err := w.reader.Run(ctx, cypher, params)
	if err != nil {
		return 0, fmt.Errorf("ssm host node writer count stale nodes: %w", err)
	}
	for _, row := range rows {
		if count, ok := intVal(row["stale_count"]); ok {
			return int(count), nil
		}
	}
	return 0, fmt.Errorf("ssm host node writer stale count query returned no stale_count row")
}

// dispatchRetract runs one retract statement through Execute in its own
// auto-commit transaction — never ExecuteGroup. On NornicDB v1.1.11 a retract
// inside a managed transaction can under-apply even as a single statement
// (#4367/#5128/#5152), so retracts never batch with a sibling write.
func (w *HostNodeWriter) dispatchRetract(ctx context.Context, stmt sourcecypher.Statement) error {
	if err := w.executor.Execute(ctx, stmt); err != nil {
		return sourcecypher.WrapRetryableNeo4jError(err)
	}
	return nil
}
