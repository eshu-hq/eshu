// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// driftEvidenceKV is this file's private copy of root package query's
// driftEvidenceKV (drifted_attributes.go). The two are structurally
// identical on purpose: DriftedAttributesFromAWSEvidence moved here (#6642
// Part A) because its family caller (management_transform.go) moved here,
// but root's driftedAttributesFromEvidence (the provider-neutral sibling,
// used only by the staying cloud_runtime_drift_store.go) could not move with
// it without also moving querycontract.DriftedAttributeView's only other
// staying consumer. A leaf package cannot import root package query to reuse
// its unexported KV-pairing helper without an import cycle (root already
// imports this leaf for the compatibility aliases), so this file keeps its
// own copy rather than exporting root's.
type driftEvidenceKV struct {
	Key   string
	Value string
}

// DriftedAttributesFromAWSEvidence pairs the reducer's declared_<attr>/
// observed_<attr> evidence atoms (emitted by
// cloudruntime.appendValueDriftEvidence for an image_version_drift finding)
// into the bounded querycontract.DriftedAttributeView projection, for the
// AWS-specific evidence row shape (reducer_aws_cloud_runtime_drift_finding).
// Every other evidence atom (arn, resource_address, finding_kind, tags, ...)
// is intentionally ignored here -- this function is one of the two narrow
// exceptions to "the query layer never surfaces raw evidence atoms" (see
// root's cloud_runtime_drift.go); it must never grow into a general evidence
// passthrough. Its home moved here from root's drifted_attributes.go (#6642
// Part A) because its only non-test caller, management_transform.go, moved
// here too; root's cloud_runtime_drift_aggregate.go (staying) reaches this
// through the iac_alias.go forwarder.
func DriftedAttributesFromAWSEvidence(evidence []postgres.AWSCloudRuntimeDriftEvidenceRow) []querycontract.DriftedAttributeView {
	kvs := make([]driftEvidenceKV, 0, len(evidence))
	for _, atom := range evidence {
		kvs = append(kvs, driftEvidenceKV{Key: atom.Key, Value: atom.Value})
	}
	return driftedAttributesFromKV(kvs)
}

// driftedAttributesFromKV groups every "declared_<attr>"/"observed_<attr>"
// key pair by <attr>, in deterministic attribute-name order, and drops any
// key that carries neither prefix. See root's drifted_attributes.go for the
// identical logic kept there for the provider-neutral evidence shape.
func driftedAttributesFromKV(kvs []driftEvidenceKV) []querycontract.DriftedAttributeView {
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
	out := make([]querycontract.DriftedAttributeView, 0, len(attrs))
	for _, attr := range attrs {
		out = append(out, querycontract.DriftedAttributeView{Attribute: attr, Declared: declared[attr], Observed: observed[attr]})
	}
	return out
}
