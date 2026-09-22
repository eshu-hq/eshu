// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
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

// ErrCloudAdmissionUndrained is returned by LiveAdmissionCloudUIDs when some
// scope's active generation still has a nonterminal cloud_inventory_admission
// work item. reducer_cloud_resource_identity is reducer output written by
// that separate, unfenced work item, so until it drains the scope has no
// admission rows for its active generation and every uid it still holds
// would read dead. The check refuses to prove death instead (fail closed):
// the retract's own work item fails and the durable queue retries it once
// the admission has landed. A dead-lettered admission blocks the same way
// and names its scope, because its facts will never land until an operator
// redrives it.
var ErrCloudAdmissionUndrained = errors.New("cloud inventory admission not drained for an active generation")

// undrainedCloudAdmissionSQL lists active-generation cloud_inventory_admission
// work items that have not reached a terminal status. The explicit status
// list (every nonterminal status the reducer queue writes) lets the
// (stage, domain, status, ...) index serve each value as a range, so the
// probe never scans the terminal items the queue keeps forever. LIMIT bounds
// the error text; one row is enough to refuse.
const undrainedCloudAdmissionSQL = `
SELECT work.scope_id, work.generation_id, work.status
FROM fact_work_items AS work
JOIN ingestion_scopes AS scope
  ON scope.scope_id = work.scope_id
 AND scope.active_generation_id = work.generation_id
WHERE work.stage = 'reducer'
  AND work.domain = '` + string(reducercontract.DomainCloudInventoryAdmission) + `'
  AND work.status IN ('pending', 'claimed', 'running', 'retrying', 'dead_letter')
ORDER BY work.scope_id, work.generation_id
LIMIT 5`

// liveAdmissionCloudUIDsSQL reports which candidate uids are still admitted
// in some scope's current generation: a live non-tombstone admission row for
// the uid joined to its scope's active generation pointer. The
// payload->>'cloud_resource_uid' partial index (migration
// 118_cloud_resource_retract_liveness_index) serves the = ANY probe as a
// point lookup; without it the planner filters the fact-kind slice.
const liveAdmissionCloudUIDsSQL = `
SELECT DISTINCT fact.payload->>'cloud_resource_uid' AS uid
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
WHERE fact.fact_kind = '` + cloudRetractAdmissionFactKind + `'
  AND fact.is_tombstone = FALSE
  AND fact.payload->>'cloud_resource_uid' = ANY($1::text[])`

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
// reducer_cloud_resource_identity row in ANY scope. It first refuses with
// ErrCloudAdmissionUndrained while any active generation's admission work
// item is nonterminal, because such a scope's admission rows do not exist
// yet and its uids would otherwise read dead. The graphowner Gate calls it
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
	if err := requireCloudAdmissionDrained(ctx, q); err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, liveAdmissionCloudUIDsSQL, uids)
	if err != nil {
		return nil, fmt.Errorf("query live admission cloud uids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan live admission cloud uid: %w", err)
		}
		if uid != "" {
			alive[uid] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate live admission cloud uids: %w", err)
	}
	return alive, nil
}

// requireCloudAdmissionDrained returns ErrCloudAdmissionUndrained (wrapped
// with the first scopes it found) when any active generation's admission
// work item is nonterminal, and nil otherwise. It runs inside the caller's
// lock-holding transaction so the answer is consistent with the admission
// probe that follows it.
func requireCloudAdmissionDrained(ctx context.Context, q db.ExecQueryer) error {
	rows, err := q.QueryContext(ctx, undrainedCloudAdmissionSQL)
	if err != nil {
		return fmt.Errorf("query undrained cloud admission work: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var pending []string
	for rows.Next() {
		var scopeID, generationID, status string
		if err := rows.Scan(&scopeID, &generationID, &status); err != nil {
			return fmt.Errorf("scan undrained cloud admission work: %w", err)
		}
		pending = append(pending, scopeID+"/"+generationID+"="+status)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate undrained cloud admission work: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrCloudAdmissionUndrained, strings.Join(pending, ", "))
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
