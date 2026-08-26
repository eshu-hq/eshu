// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shape

import (
	"path/filepath"
	"testing"
)

// nonBucketProjectorLabels names every label entityTypeLabelMap registers that
// no contentEntityBuckets row can produce, together with the fact source that
// does feed it.
//
// This is the reverse of TestContentEntityLabelsHaveProjectorLabels in
// bucket_sync_gate_test.go. That test walks bucket -> projector and catches a
// bucket whose label the projector cannot name. Nothing walked projector ->
// bucket, so a label could be added to entityTypeLabelMap that no bucket, no
// collector twin entry, and no parser ever produces, and every gate in the tree
// stayed green — including the #6206 ledger in
// go/internal/projector/canonical_unwritten_entity_labels_test.go, which sweeps
// the registry through phase E and correctly reports such a label as "written",
// because phase E would indeed write it if a fact ever carried the type. The
// registry entry is inert, and inert-but-registered is the exact confusion
// #6206 exists to end.
//
// The entries below are legitimate: these families do not come from filesystem
// parsing into content buckets. OCI and package-registry labels are written by
// their own canonical writers (go/internal/storage/cypher's
// oci_registry_canonical_writer.go and package_registry_canonical_writer.go),
// Parameter comes from canonical phase G reading param_name payloads, and
// ShellCommand comes from the shell-exec evidence path.
//
// What this gate machine-checks is TOTALITY: every registered label is
// classified, as bucket-backed or as listed here. It does NOT verify the source
// strings — those name a writer for a human reader and can go stale, the same
// residual bucket_sync_gate_test.go's materializedLabelsFor documents for
// metadata-keyed rewrites. Verifying all eighteen writers would mean driving
// four separate collector pipelines from this package, which it cannot import.
// The gate that a source string is real is that adding an entry requires
// naming one, in a diff a reviewer reads.
//
// Adding an entry means a registered label has no bucket behind it: correct for
// a genuinely non-bucket source, a defect otherwise. Removing one means the
// label became bucket-backed, which is a projected-truth change.
var nonBucketProjectorLabels = map[string]string{
	"ContainerImage":                   "OCI registry collector; oci_registry_canonical_writer.go",
	"ContainerImageDescriptor":         "OCI registry collector; oci_registry_canonical_writer.go",
	"ContainerImageIndex":              "OCI registry collector; oci_registry_canonical_writer.go",
	"ContainerImageTagObservation":     "OCI registry collector; oci_registry_canonical_writer.go",
	"OciImageDescriptor":               "OCI registry collector; oci_registry_canonical_writer.go",
	"OciImageIndex":                    "OCI registry collector; oci_registry_canonical_writer.go",
	"OciImageManifest":                 "OCI registry collector; oci_registry_canonical_writer.go",
	"OciImageReferrer":                 "OCI registry collector; oci_registry_canonical_writer.go",
	"OciImageTagObservation":           "OCI registry collector; oci_registry_canonical_writer.go",
	"OciRegistryRepository":            "OCI registry collector; oci_registry_canonical_writer.go",
	"Package":                          "package registry collector; package_registry_canonical_writer.go",
	"PackageDependency":                "package registry collector; package_registry_canonical_writer.go",
	"PackageRegistryPackage":           "package registry collector; package_registry_canonical_writer.go",
	"PackageRegistryPackageDependency": "package registry collector; package_registry_canonical_writer.go",
	"PackageRegistryPackageVersion":    "package registry collector; package_registry_canonical_writer.go",
	"PackageVersion":                   "package registry collector; package_registry_canonical_writer.go",
	"Parameter":                        "canonical phase G, extractRelationships over param_name facts",
	"ShellCommand":                     "shell-exec evidence path; edge_writer_shell_exec.go",
}

// TestEveryProjectorLabelHasASource is the totality gate: no label may sit in
// entityTypeLabelMap unclassified.
//
// Both directions bite. A new registry entry with no bucket behind it fails on
// the commit that adds it, so the author has to say which source feeds it or
// discover there is none. A stale ledger entry fails once its label becomes
// bucket-backed or leaves the registry, so this list cannot quietly outlive
// what it describes — the failure mode that put knownBucketSyncDrift's honesty
// test in the sibling file.
func TestEveryProjectorLabelHasASource(t *testing.T) {
	root := bucketSyncRepoRoot(t)

	buckets := parseBucketLabelSlice(t,
		filepath.Join(root, "go/internal/content/shape/materialize_tables.go"), "contentEntityBuckets")
	projector := parseStringMapValues(t,
		filepath.Join(root, "go/internal/projector/canonical.go"), "entityTypeLabelMap")

	if len(buckets) == 0 || len(projector) == 0 {
		t.Fatalf("extracted %d bucket rows and %d projector labels; an empty side means the parse lost a "+
			"declaration, and every assertion below would pass vacuously", len(buckets), len(projector))
	}

	// Every label any bucket can materialize, including the rewrites
	// materializedLabelsFor drives (Module -> ProtocolImplementation).
	bucketBacked := make(map[string]struct{}, len(buckets))
	for _, bucket := range sortedKeysOf(buckets) {
		for _, label := range materializedLabelsFor(buckets[bucket]) {
			bucketBacked[label] = struct{}{}
		}
	}

	for _, label := range sortedKeysOf(projector) {
		if _, ok := bucketBacked[label]; ok {
			continue
		}
		if source, declared := nonBucketProjectorLabels[label]; declared {
			if source == "" {
				t.Errorf("nonBucketProjectorLabels[%q] has an empty source: an entry has to name the fact "+
					"source that feeds the label, or it records nothing", label)
			}
			continue
		}
		t.Errorf("entityTypeLabelMap registers label %q and no contentEntityBuckets row produces it. The "+
			"entry is inert: no bucket, no collector twin row, no parser output, so no content_entity fact "+
			"ever carries the type and the label names a node the pipeline cannot create. Add the bucket, "+
			"or add %q to nonBucketProjectorLabels with the fact source that writes it.", label, label)
	}

	for _, label := range sortedKeysOf(nonBucketProjectorLabels) {
		if _, ok := projector[label]; !ok {
			t.Errorf("nonBucketProjectorLabels[%q] is stale: entityTypeLabelMap no longer registers that "+
				"label, so the entry describes nothing and would silently absorb a future label of the "+
				"same name", label)
			continue
		}
		if _, ok := bucketBacked[label]; ok {
			t.Errorf("nonBucketProjectorLabels[%q] is stale: a contentEntityBuckets row produces that label "+
				"now, so it is bucket-backed and the exemption is licensing a check that should apply to "+
				"it", label)
		}
	}
}
