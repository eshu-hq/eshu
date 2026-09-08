// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2instance

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/cloudjoin"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// ExtractEC2InstanceIdentityNodeRows projects aws_ec2_instance aws_resource
// fact envelopes (#5448) into deterministic CloudResource property-augment
// rows keyed by the canonical cloud_resource_uid. It never fabricates a row
// for an instance whose identity is incomplete (same empty-identity skip
// cloudResourceNodeRow applies), and it is scoped to ONLY the aws_ec2_instance
// resource_type — every other aws_resource fact is ignored here, exactly the
// inverse of cloudResourceNodeRow's exclusion.
//
// Rows carry only the disjoint ec2_identity_* namespace plus the top-level
// ami_id property; they NEVER carry arn/resource_id/name/state or any other
// property the EC2 instance posture node materialization already owns. This
// is what makes the write safe to MERGE onto the same uid the posture path
// created: the two writers' SET clauses touch no property in common.
func ExtractEC2InstanceIdentityNodeRows(envelopes []facts.Envelope) ([]map[string]any, []factdecode.QuarantinedFact, error) {
	if len(envelopes) == 0 {
		return nil, nil, nil
	}

	var quarantined []factdecode.QuarantinedFact
	byUID := make(map[string]map[string]any, len(envelopes))
	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind || env.IsTombstone {
			continue
		}
		row, uid, ok, err := ec2InstanceIdentityNodeRow(env)
		if err != nil {
			q, isQuarantine, fatal := factdecode.PartitionDecodeFailures(env, err)
			if fatal != nil {
				return nil, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}
		if !ok {
			continue
		}
		// #5007 Stage 1 convention: the max-source_order_key contributor wins
		// within a scope generation, matching every other CloudResource
		// extractor's duplicate-uid resolution rule.
		if payloadcore.PreferMaxSourceOrderKey(byUID[uid], row) {
			byUID[uid] = row
		}
	}

	if len(byUID) == 0 {
		return nil, quarantined, nil
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
	return rows, quarantined, nil
}

// ec2InstanceIdentityNodeRow builds one augment row from a decoded
// aws_ec2_instance aws_resource envelope. It returns ok=false for any
// resource_type other than aws_ec2_instance (this extractor's scope) or for an
// instance whose identity cannot form a stable uid.
func ec2InstanceIdentityNodeRow(env facts.Envelope) (map[string]any, string, bool, error) {
	resource, err := schemadecode.DecodeAWSResource(env)
	if err != nil {
		return nil, "", false, err
	}
	if resource.ResourceType != awsv1.ResourceTypeEC2Instance {
		return nil, "", false, nil
	}
	uid, ok := ec2InstanceIdentityUIDForResource(resource)
	if !ok {
		return nil, "", false, nil
	}

	attrs, err := awsv1.DecodeResourceEC2InstanceAttributes(resource)
	if err != nil {
		return nil, "", false, factdecode.AttributeShapeAsFactDecodeError(env.FactKind, err)
	}
	// Skip an instance whose identity is incomplete: with no observed ami_id
	// there is nothing to augment, and stamping ami_id="" onto the
	// posture-owned CloudResource node would fabricate an empty-but-present
	// property (a `WHERE r.ami_id IS NOT NULL` reader would falsely match it).
	// A scope-wide retract-first still removes any stale ami_id, so skipping
	// the write row is the correct absence, mirroring the uid-invalid skip.
	amiID := strings.TrimSpace(attrs.AMIID)
	if amiID == "" {
		return nil, "", false, nil
	}

	// The row key is source_fact_id (the shared cross-domain provenance-key
	// convention every sibling writer's Cypher reads as row.source_fact_id);
	// the persisted graph property is the disjoint r.ec2_identity_source_fact_id.
	row := map[string]any{
		"uid":                           uid,
		"ami_id":                        amiID,
		"source_fact_id":                env.FactID,
		payloadcore.SourceOrderKeyField: payloadcore.SourceOrderKey(env),
	}
	return row, uid, true, nil
}

// ec2InstanceIdentityUIDForResource derives the stable CloudResource node uid
// from an already-decoded aws_ec2_instance aws_resource struct. It mirrors the
// reducer root's cloudResourceUIDForResource identity rules (resource_id with
// arn fallback, empty-identity rejection) inline because this family must never
// import its parent package, and the helper has only this one family consumer
// besides the root's workload slice — a hoist to cloudjoin would drag the
// staying root caller along for no family-substrate reason (issue #6061 hoist
// rule: hoist only substrate with 2+ family consumers).
func ec2InstanceIdentityUIDForResource(resource awsv1.Resource) (string, bool) {
	resourceID := resource.ResourceID
	if resourceID == "" {
		resourceID = payloadcore.DerefString(resource.ARN)
	}
	if resource.ResourceType == "" || resourceID == "" {
		return "", false
	}
	return cloudjoin.CloudResourceUID(resource.AccountID, resource.Region, resource.ResourceType, resourceID), true
}
