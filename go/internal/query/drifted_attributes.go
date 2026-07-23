// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// driftEvidenceKV is the minimal (key, value) shape both the AWS-specific and
// provider-neutral runtime-drift evidence row types reduce to for pairing.
// The two postgres row types (AWSCloudRuntimeDriftEvidenceRow and
// MultiCloudRuntimeDriftEvidenceRow) are structurally identical but distinct
// Go types; this shared shape lets driftedAttributesFromKV serve both
// surfaces from one implementation instead of two copies that could drift
// out of sync.
type driftEvidenceKV struct {
	Key   string
	Value string
}

// driftedAttributesFromEvidence pairs the reducer's declared_<attr>/
// observed_<attr> evidence atoms (emitted by
// cloudruntime.appendValueDriftEvidence / multicloud.appendValueDriftEvidence
// for an image_version_drift finding) into the bounded DriftedAttributeView
// projection, for the provider-neutral (multi-cloud) evidence row shape.
// Every other evidence atom (arn, resource_address, finding_kind, tags, ...)
// is intentionally ignored here -- this function is one of the two narrow
// exceptions to "the query layer never surfaces raw evidence atoms" (see
// cloud_runtime_drift.go), and it must never grow into a general evidence
// passthrough.
func driftedAttributesFromEvidence(evidence []postgres.MultiCloudRuntimeDriftEvidenceRow) []DriftedAttributeView {
	kvs := make([]driftEvidenceKV, 0, len(evidence))
	for _, atom := range evidence {
		kvs = append(kvs, driftEvidenceKV{Key: atom.Key, Value: atom.Value})
	}
	return driftedAttributesFromKV(kvs)
}

// driftedAttributesFromAWSEvidence is driftedAttributesFromEvidence's sibling
// for the AWS-specific evidence row shape (reducer_aws_cloud_runtime_drift_finding),
// feeding IaCManagementFindingRow.DriftedAttributes / AWSRuntimeDriftFindingRow
// (#5453 P2-3) the same declared_/observed_ projection
// list_cloud_runtime_drift_findings already carries.
func driftedAttributesFromAWSEvidence(evidence []postgres.AWSCloudRuntimeDriftEvidenceRow) []DriftedAttributeView {
	kvs := make([]driftEvidenceKV, 0, len(evidence))
	for _, atom := range evidence {
		kvs = append(kvs, driftEvidenceKV{Key: atom.Key, Value: atom.Value})
	}
	return driftedAttributesFromKV(kvs)
}

// driftedAttributesFromKV is the shared pairing logic both evidence-row
// adapters above delegate to: it groups every "declared_<attr>"/
// "observed_<attr>" key pair by <attr>, in deterministic attribute-name
// order, and drops any key that carries neither prefix.
func driftedAttributesFromKV(kvs []driftEvidenceKV) []DriftedAttributeView {
	declared := map[string]string{}
	observed := map[string]string{}
	seen := map[string]struct{}{}
	var attrs []string
	for _, kv := range kvs {
		switch {
		case strings.HasPrefix(kv.Key, "declared_"):
			attr := strings.TrimPrefix(kv.Key, "declared_")
			declared[attr] = kv.Value
			if _, ok := seen[attr]; !ok {
				seen[attr] = struct{}{}
				attrs = append(attrs, attr)
			}
		case strings.HasPrefix(kv.Key, "observed_"):
			attr := strings.TrimPrefix(kv.Key, "observed_")
			observed[attr] = kv.Value
			if _, ok := seen[attr]; !ok {
				seen[attr] = struct{}{}
				attrs = append(attrs, attr)
			}
		}
	}
	if len(attrs) == 0 {
		return nil
	}
	sort.Strings(attrs)
	out := make([]DriftedAttributeView, 0, len(attrs))
	for _, attr := range attrs {
		out = append(out, DriftedAttributeView{Attribute: attr, Declared: declared[attr], Observed: observed[attr]})
	}
	return out
}
