// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"fmt"
	"strings"
)

// fluxKustomizeToolkitGroup is the Flux CD apiVersion group for the
// kustomize.toolkit.fluxcd.io Kustomization CRD. It is deliberately distinct
// from the generic "kustomize.config.k8s.io" group isKustomization matches:
// a Flux Kustomization is a live cluster-reconciliation custom resource that
// nests its declarative fields under spec (sourceRef, path,
// targetNamespace), not a kustomization.yaml build manifest, so it needs its
// own parser instead of reusing parseKustomization's top-level-key walk
// (issue #5342).
const fluxKustomizeToolkitGroup = "kustomize.toolkit.fluxcd.io/"

// isFluxKustomization reports whether a document is a Flux CD Kustomization
// custom resource: apiVersion group "kustomize.toolkit.fluxcd.io" (any
// version) with kind "Kustomization". It intentionally does not match on
// filename -- a Flux Kustomization CR can be saved under any file name, and
// isKustomization's own filename-only branch is vetoed for any non-generic
// apiVersion, so a Flux Kustomization saved as kustomization.yaml still
// reaches this branch instead of the generic overlay parser.
func isFluxKustomization(apiVersion string, kind string) bool {
	return strings.HasPrefix(apiVersion, fluxKustomizeToolkitGroup) && kind == "Kustomization"
}

// parseFluxKustomization captures a Flux Kustomization CR's reconciliation
// source as typed deployment evidence: spec.sourceRef (kind/name/namespace),
// spec.path, and spec.targetNamespace.
//
// Fields are parsed defensively: an absent or empty field is simply omitted
// from the row, never fabricated (a missing spec.path is recorded as absent,
// not defaulted to "./").
//
// This bucket is evidence only. It is deliberately NOT registered in
// go/internal/content/shape/materialize_tables.go's contentEntityBuckets and
// is NOT wired into go/internal/relationships/structured_family_evidence.go,
// so it does not become a graph node or a queryable relationship-evidence
// surface -- there is no read surface backing it. Modeling Flux as a
// queryable deployment platform is tracked separately (#5360).
func parseFluxKustomization(document map[string]any, metadata map[string]any, path string, lineNumber int) map[string]any {
	spec, _ := document["spec"].(map[string]any)
	sourceRef, _ := spec["sourceRef"].(map[string]any)

	row := map[string]any{
		"name":        strings.TrimSpace(fmt.Sprint(metadata["name"])),
		"line_number": lineNumber,
		"namespace":   strings.TrimSpace(fmt.Sprint(metadata["namespace"])),
		"path":        path,
		"lang":        "yaml",
	}
	if kind := cleanYAMLString(sourceRef["kind"]); kind != "" {
		row["source_ref_kind"] = kind
	}
	if name := cleanYAMLString(sourceRef["name"]); name != "" {
		row["source_ref_name"] = name
	}
	if namespace := cleanYAMLString(sourceRef["namespace"]); namespace != "" {
		row["source_ref_namespace"] = namespace
	}
	if specPath := cleanYAMLString(spec["path"]); specPath != "" {
		row["spec_path"] = specPath
	}
	if targetNamespace := cleanYAMLString(spec["targetNamespace"]); targetNamespace != "" {
		row["target_namespace"] = targetNamespace
	}
	if labels := collectMetadataLabels(metadata); labels != "" {
		row["labels"] = labels
	}
	return row
}
