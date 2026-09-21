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
// reducer_cloud_resource_identity row in ANY scope. The graphowner Gate calls
// it inside the retract chunk's lock-holding transaction; an empty input
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
