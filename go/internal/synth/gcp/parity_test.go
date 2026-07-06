// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestParitySyntheticVsRecordedGCPShape is the maintainer-run parity check
// comparing this generator's synthetic output against the recorded GCP
// cassette's shape (issue #4581 acceptance criterion: "a maintainer-run
// parity check comparing synthetic vs recorded shape for GCP"), following
// the operator-gated live-smoke precedent (e.g.
// go/internal/collector/pagerduty/live_test.go's ESHU_PAGERDUTY_LIVE gate):
// it is SKIPPED by default and in CI, and only runs when a maintainer sets
// ESHU_SYNTH_GCP_PARITY=1 locally.
//
// "Parity" here means fact-kind and asset-type shape agreement, not byte
// equality: the recorded cassette (testdata/cassettes/gcpcloud/
// supply-chain-demo.json) is real (if synthetic-safe) evidence of what a live
// GCP collection produces, while this generator produces a schema-valid but
// independently constructed corpus. Two checks:
//
//  1. Every fact kind the recorded cassette carries must be one of the five
//     kinds this generator's contract covers (recordedKinds subset of
//     factKindSchemaVersions). The recorded demo fixture is not required to
//     exercise every kind the generator can produce — gcp_dns_record,
//     gcp_collection_warning, and gcp_iam_policy_observation are real GCP
//     fact kinds this generator emits that the small recorded demo happens
//     not to carry — but a kind flowing the other direction (recorded but
//     unknown to the generator) would mean a real GCP fact kind exists that
//     this generator's contract has not accounted for.
//  2. Every asset type the recorded cassette observes AND that has a
//     registered typed-depth extractor in go/internal/collector/gcpcloud
//     must appear in this package's static assetTypeInventory
//     (asset_types.go). The recorded cassette also carries plain
//     CAI-observed asset types with no registered extractor (referenced only
//     as relationship targets, e.g. compute.googleapis.com/Image) — those are
//     correctly absent from the extractor-scoped inventory (issue #4581:
//     "the typed-depth extractor registry gives the cleanest asset-type
//     inventory"), so they are excluded from this comparison by an explicit
//     allow-list rather than silently ignored.
func TestParitySyntheticVsRecordedGCPShape(t *testing.T) {
	if os.Getenv("ESHU_SYNTH_GCP_PARITY") != "1" {
		t.Skip("set ESHU_SYNTH_GCP_PARITY=1 to run the synthetic-vs-recorded GCP parity check")
	}

	recorded, err := loadRecordedGCPCassette(t)
	if err != nil {
		t.Fatalf("load recorded GCP cassette: %v", err)
	}

	recordedKinds := factKindSet(recorded)
	for kind := range recordedKinds {
		if _, ok := factKindSchemaVersions[kind]; !ok {
			t.Errorf("recorded cassette carries fact kind %q, which this generator's contract (factKindSchemaVersions) does not cover", kind)
		}
	}

	recordedAssetTypes := assetTypeSet(recorded)
	inventorySet := make(map[string]bool, len(assetTypeInventory))
	for _, at := range assetTypeInventory {
		inventorySet[at] = true
	}
	nonExtractorAssetTypes := make(map[string]bool, len(recordedNonExtractorAssetTypes))
	for _, at := range recordedNonExtractorAssetTypes {
		nonExtractorAssetTypes[at] = true
	}

	var staleInventory []string
	for at := range recordedAssetTypes {
		if inventorySet[at] || nonExtractorAssetTypes[at] {
			continue
		}
		staleInventory = append(staleInventory, at)
	}
	if len(staleInventory) > 0 {
		sort.Strings(staleInventory)
		t.Errorf("recorded cassette observes extractor-eligible asset type(s) missing from assetTypeInventory (asset_types.go): %v; refresh the inventory from the current gcpcloud extractor registry, or add to recordedNonExtractorAssetTypes if genuinely extractor-less", staleInventory)
	}

	t.Logf("parity check: recorded cassette carries %d fact kinds / %d asset types (%d without a registered extractor); synthetic inventory covers %d asset types",
		len(recordedKinds), len(recordedAssetTypes), len(nonExtractorAssetTypes), len(assetTypeInventory))
}

// recordedNonExtractorAssetTypes lists asset types the recorded GCP demo
// cassette observes that have NO registered typed-depth extractor in
// go/internal/collector/gcpcloud as of this generator's authoring — they
// appear in the recording only as bare CAI resources or relationship targets
// (e.g. "compute.googleapis.com/Image" is a disk's source image, referenced
// by extractor_disk.go but never itself the subject of
// RegisterAssetExtractor). This package's assetTypeInventory is deliberately
// scoped to extractor-registered types (issue #4581), so these are excluded
// from the parity comparison rather than silently ignored: this list is the
// documented reason, refreshed by the same maintainer who refreshes
// asset_types.go.
var recordedNonExtractorAssetTypes = []string{
	"appengine.googleapis.com/Version",
	"bigqueryconnection.googleapis.com/Connection",
	"bigtableadmin.googleapis.com/Table",
	"cloudkms.googleapis.com/CryptoKeyVersion",
	"cloudresourcemanager.googleapis.com/Project",
	"compute.googleapis.com/BackendBucket",
	"compute.googleapis.com/ExternalVpnGateway",
	"compute.googleapis.com/Image",
	"compute.googleapis.com/InstanceGroup",
	"compute.googleapis.com/ServiceAttachment",
	"compute.googleapis.com/Snapshot",
	"compute.googleapis.com/TargetHttpProxy",
	"compute.googleapis.com/TargetInstance",
	"compute.googleapis.com/TargetPool",
	"compute.googleapis.com/TargetVpnGateway",
	"eventarc.googleapis.com/Channel",
	"pubsub.googleapis.com/Schema",
	"sourcerepo.googleapis.com/Repository",
	"vpcaccess.googleapis.com/Connector",
}

// parityCassette is the minimal shape this parity check reads from either the
// recorded or the synthetic cassette JSON.
type parityCassette struct {
	Scopes []struct {
		Facts []struct {
			FactKind string         `json:"fact_kind"`
			Payload  map[string]any `json:"payload"`
		} `json:"facts"`
	} `json:"scopes"`
}

// loadRecordedGCPCassette reads the real checked-in GCP cassette
// (testdata/cassettes/gcpcloud/supply-chain-demo.json) the B-7 golden-corpus
// gate replays, the same maintainer-curated evidence of live GCP collection
// shape this parity check compares against.
func loadRecordedGCPCassette(t *testing.T) (parityCassette, error) {
	t.Helper()
	wd, err := filepath.Abs(".")
	if err != nil {
		return parityCassette{}, err
	}
	// This file lives at <repoRoot>/go/internal/synth/gcp/; the recorded
	// cassette lives at <repoRoot>/testdata/cassettes/gcpcloud/supply-chain-demo.json.
	path := filepath.Join(wd, "..", "..", "..", "..", "testdata", "cassettes", "gcpcloud", "supply-chain-demo.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return parityCassette{}, err
	}
	var out parityCassette
	if err := json.Unmarshal(raw, &out); err != nil {
		return parityCassette{}, err
	}
	return out, nil
}

// factKindSet collects the distinct fact kinds observed in c.
func factKindSet(c parityCassette) map[string]bool {
	set := map[string]bool{}
	for _, scope := range c.Scopes {
		for _, fact := range scope.Facts {
			set[fact.FactKind] = true
		}
	}
	return set
}

// assetTypeSet collects the distinct gcp_cloud_resource asset_type values
// observed in c.
func assetTypeSet(c parityCassette) map[string]bool {
	set := map[string]bool{}
	for _, scope := range c.Scopes {
		for _, fact := range scope.Facts {
			if fact.FactKind != "gcp_cloud_resource" {
				continue
			}
			if assetType, ok := fact.Payload["asset_type"].(string); ok && assetType != "" {
				set[assetType] = true
			}
		}
	}
	return set
}
