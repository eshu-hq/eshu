// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// cloudRetractAdmissionFactKind is the reducer-owned canonical CloudResource
// identity fact kind the #6887 live-check probes. It stays in lockstep with
// cloudinventory.cloudInventoryAdmissionFactKind (the writer) and
// inventory.graphOnlyAdmissionFactKind (the unscoped read path): all three
// spell the same shared keyspace.
const cloudRetractAdmissionFactKind = "reducer_cloud_resource_identity"

// cloudRetractEC2PostureFactKind is the EC2 instance posture fact kind the
// #6887 live-check probes for the EC2-identity family.
const cloudRetractEC2PostureFactKind = "ec2_instance_posture"

// cloudRetractDefaultEC2ResourceType is the resource_type the unscoped read
// path substitutes for a blank EC2 posture resource_type
// (inventory.graphOnlyDefaultEC2ResourceType). The live-check normalizes
// candidates with the same default so its tuple predicate matches the
// reader's DISTINCT ON identity exactly.
const cloudRetractDefaultEC2ResourceType = "aws_ec2_instance"

// cloudAdmissionNonterminalStatusList is every fact_work_items status that
// is not terminal, spelled as the SQL IN list the fence probes. It matches
// generation_liveness_sql.go's enumeration of the same concept (the fail path
// writes dead_letter, but 'failed' is listed there too and costs nothing
// here: the (stage, domain, status, ...) index serves each value as a range,
// so the probe never scans the terminal items the queue keeps forever). Any
// status outside this list and outside succeeded/superseded would slip past
// the fence, which is why the list is the superset the repo already uses.
const cloudAdmissionNonterminalStatusList = `('pending', 'claimed', 'running', 'retrying', 'failed', 'dead_letter')`

// undrainedCloudAdmissionWhere is the shared predicate: active-generation
// cloud_inventory_admission work items that have not reached a terminal
// status.
const undrainedCloudAdmissionWhere = `
FROM fact_work_items AS work
JOIN ingestion_scopes AS scope
  ON scope.scope_id = work.scope_id
 AND scope.active_generation_id = work.generation_id
WHERE work.stage = 'reducer'
  AND work.domain = '` + string(reducercontract.DomainCloudInventoryAdmission) + `'
  AND work.status IN ` + cloudAdmissionNonterminalStatusList

// undrainedCloudAdmissionSQL lists undrained active-generation admission
// items for the pre-lock check (RequireCloudAdmissionDrained). LIMIT bounds
// the error text; one row is enough to refuse.
const undrainedCloudAdmissionSQL = `
SELECT work.scope_id || '/' || work.generation_id || '=' || work.status AS pending` +
	undrainedCloudAdmissionWhere + `
ORDER BY work.scope_id, work.generation_id
LIMIT 5`

// liveAdmissionCloudUIDsAliveSQL is the admission read on its own: which of
// the candidate uids have a live (active-generation, non-tombstone)
// reducer_cloud_resource_identity row in any scope. The MATERIALIZED CTE
// pins the plan (#6946): the candidate slice is fetched first through the
// migration-118 partial expression index, then joined to ingestion_scopes.
// Without it the planner is free to start from ingestion_scopes and walk
// fact_records_scope_generation_idx (14k buffers per 500-candidate chunk at
// 2,000 scopes) or to gamble on an early-terminating merge join; migration
// 119's extended statistics make the CTE's estimate accurate so the index
// wins inside it at every chunk size. Exported to the plan live test so it
// explains exactly this statement.
const liveAdmissionCloudUIDsAliveSQL = `
WITH cand AS MATERIALIZED (
  SELECT fact.scope_id, fact.generation_id, fact.payload->>'cloud_resource_uid' AS uid
  FROM fact_records AS fact
  WHERE fact.fact_kind = '` + cloudRetractAdmissionFactKind + `'
    AND fact.is_tombstone = FALSE
    AND fact.payload->>'cloud_resource_uid' = ANY($1::text[]))
SELECT DISTINCT cand.uid AS uid
FROM cand
JOIN ingestion_scopes AS scope
  ON scope.scope_id = cand.scope_id
 AND scope.active_generation_id = cand.generation_id`

// liveAdmissionCloudUIDsFencedSQL is the in-transaction probe: the fence
// rows and the admission rows come back from ONE statement, so under READ
// COMMITTED both see the same snapshot and a scope's active pointer cannot
// flip onto an undrained generation between "no admission is pending" and
// "this uid has no admission row". Kind 'undrained' rows (at most 5) make
// the caller refuse; kind 'alive' rows are the admitted candidate uids.
const liveAdmissionCloudUIDsFencedSQL = `
(SELECT 'undrained' AS kind, work.scope_id || '/' || work.generation_id || '=' || work.status AS value` +
	undrainedCloudAdmissionWhere + `
 ORDER BY work.scope_id, work.generation_id
 LIMIT 5)
UNION ALL
(SELECT 'alive' AS kind, alive.uid AS value FROM (` + liveAdmissionCloudUIDsAliveSQL + `) AS alive)`

// liveEC2PostureUIDsSQL reports which candidate EC2 tuples still have a live
// posture fact in some scope's current generation. The tuple predicate
// reproduces the unscoped reader's EC2 identity exactly: blank-tolerant
// account/region equality, blank resource_type falling back to the EC2
// default, and instance_id falling back to arn. Candidates arrive
// pre-normalized by normalizeEC2PostureCandidate, so the SQL compares plain
// equalities.
const liveEC2PostureUIDsSQL = `
SELECT DISTINCT t.uid AS uid
FROM UNNEST($1::text[], $2::text[], $3::text[], $4::text[], $5::text[])
  AS t(uid, account_id, region, resource_type, instance_ref)
JOIN fact_records AS fact
  ON COALESCE(fact.payload->>'account_id', '') = t.account_id
 AND COALESCE(fact.payload->>'region', '') = t.region
 AND COALESCE(NULLIF(fact.payload->>'resource_type', ''), '` + cloudRetractDefaultEC2ResourceType + `') = t.resource_type
 AND COALESCE(NULLIF(fact.payload->>'instance_id', ''), fact.payload->>'arn') = t.instance_ref
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
WHERE fact.fact_kind = '` + cloudRetractEC2PostureFactKind + `'
  AND fact.is_tombstone = FALSE`

// LiveAdmissionCloudUIDs returns the subset of candidate uids that are still
// admitted by a live (current-generation, non-tombstone)
// reducer_cloud_resource_identity row in ANY scope. It refuses with
// reducercontract.ErrCloudAdmissionUndrained while any active generation's
// admission work item is nonterminal, because such a scope's admission rows
// do not exist yet and its uids would otherwise read dead; the fence and the
// admission rows are read by one statement, so they share a snapshot. The graphowner Gate calls it
// inside the retract chunk's lock-holding transaction; an empty input
// returns an empty set without touching the database.
func LiveAdmissionCloudUIDs(
	ctx context.Context,
	q db.ExecQueryer,
	uids []string,
) (map[string]struct{}, error) {
	if q == nil {
		return nil, fmt.Errorf("cloud resource liveness queryer is required")
	}
	alive := make(map[string]struct{})
	if len(uids) == 0 {
		return alive, nil
	}
	rows, err := q.QueryContext(ctx, liveAdmissionCloudUIDsFencedSQL, uids)
	if err != nil {
		return nil, fmt.Errorf("query live admission cloud uids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var pending []string
	for rows.Next() {
		var kind, value string
		if err := rows.Scan(&kind, &value); err != nil {
			return nil, fmt.Errorf("scan live admission cloud uid: %w", err)
		}
		switch kind {
		case "undrained":
			pending = append(pending, value)
		case "alive":
			if value != "" {
				alive[value] = struct{}{}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate live admission cloud uids: %w", err)
	}
	if len(pending) > 0 {
		return nil, fmt.Errorf("%w: %s", reducercontract.ErrCloudAdmissionUndrained, strings.Join(pending, ", "))
	}
	return alive, nil
}

// RequireCloudAdmissionDrained returns reducercontract.ErrCloudAdmissionUndrained
// (wrapped with the first scopes it found) when any active generation's
// cloud_inventory_admission work item is nonterminal, and nil otherwise. The
// graphowner retracter runs it BEFORE taking any per-uid lock so a refusal
// costs one index probe and no lock churn; the in-transaction probe
// (LiveAdmissionCloudUIDs) repeats the same predicate in the same statement
// as the admission rows, which is what makes the answer consistent.
func RequireCloudAdmissionDrained(ctx context.Context, q db.ExecQueryer) error {
	if q == nil {
		return fmt.Errorf("cloud resource liveness queryer is required")
	}
	rows, err := q.QueryContext(ctx, undrainedCloudAdmissionSQL)
	if err != nil {
		return fmt.Errorf("query undrained cloud admission work: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var pending []string
	for rows.Next() {
		var item string
		if err := rows.Scan(&item); err != nil {
			return fmt.Errorf("scan undrained cloud admission work: %w", err)
		}
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate undrained cloud admission work: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", reducercontract.ErrCloudAdmissionUndrained, strings.Join(pending, ", "))
}

// normalizeEC2PostureCandidate applies the unscoped reader's EC2 identity
// semantics: trim account/region, default a blank resource_type, and resolve
// the instance reference to instance_id-or-arn. It reports false when the
// candidate carries no usable identity (blank reference), which can never
// match a live posture row.
func normalizeEC2PostureCandidate(c reducercontract.EC2PostureCandidate) (accountID, region, resourceType, instanceRef string, ok bool) {
	accountID = strings.TrimSpace(c.AccountID)
	region = strings.TrimSpace(c.Region)
	resourceType = strings.TrimSpace(c.ResourceType)
	if resourceType == "" {
		resourceType = cloudRetractDefaultEC2ResourceType
	}
	instanceRef = strings.TrimSpace(c.InstanceID)
	if instanceRef == "" {
		instanceRef = strings.TrimSpace(c.ARN)
	}
	if instanceRef == "" {
		return "", "", "", "", false
	}
	return accountID, region, resourceType, instanceRef, true
}

// LiveEC2PostureUIDs returns the subset of candidate UIDs whose EC2 identity
// tuple is still carried by a live (current-generation, non-tombstone)
// ec2_instance_posture fact in ANY scope. A candidate with no usable identity
// is never reported alive. Like LiveAdmissionCloudUIDs it runs against the
// caller's (lock-holding) transaction and short-circuits on empty input.
func LiveEC2PostureUIDs(
	ctx context.Context,
	q db.ExecQueryer,
	candidates []reducercontract.EC2PostureCandidate,
) (map[string]struct{}, error) {
	if q == nil {
		return nil, fmt.Errorf("cloud resource liveness queryer is required")
	}
	alive := make(map[string]struct{})
	if len(candidates) == 0 {
		return alive, nil
	}
	var uids, accounts, regions, types, refs []string
	for _, c := range candidates {
		uid := strings.TrimSpace(c.UID)
		if uid == "" {
			continue
		}
		accountID, region, resourceType, instanceRef, ok := normalizeEC2PostureCandidate(c)
		if !ok {
			continue
		}
		uids = append(uids, uid)
		accounts = append(accounts, accountID)
		regions = append(regions, region)
		types = append(types, resourceType)
		refs = append(refs, instanceRef)
	}
	if len(uids) == 0 {
		return alive, nil
	}
	rows, err := q.QueryContext(ctx, liveEC2PostureUIDsSQL, uids, accounts, regions, types, refs)
	if err != nil {
		return nil, fmt.Errorf("query live ec2 posture uids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan live ec2 posture uid: %w", err)
		}
		if uid != "" {
			alive[uid] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate live ec2 posture uids: %w", err)
	}
	return alive, nil
}
