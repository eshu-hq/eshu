// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
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
// spec.path is emitted under the "source_path" row key (not "spec_path") so
// it lines up with the key deployment-trace helpers already read for other
// GitOps controllers (e.g. impact_trace_deployment_gitops_helpers.go reading
// metadataNonEmptyStringValue(entity.Metadata, "source_path") for ArgoCD).
// This bucket had zero consumers before #5360 PR A, so the rename is free;
// renaming after a consumer exists would not be.
//
// Fields are parsed defensively: an absent or empty field is simply omitted
// from the row, never fabricated (a missing spec.path is recorded as absent,
// not defaulted to "./").
//
// Identity boundary: a Kustomization that uses metadata.generateName instead
// of metadata.name has an empty name here (never a fabricated "<nil>"), so its
// row identity is (repo_id, path, label, name="", start_line). That is unique
// because two same-label entities cannot share a start line in one file --
// multi-document YAML forces a distinct `---` document start line per entity.
//
// This bucket is registered in
// go/internal/content/shape/materialize_tables.go's contentEntityBuckets as
// the typed FluxKustomization content entity (issue #5360 PR A), making it
// reachable through get_entity_context. It is still NOT wired into
// go/internal/relationships/structured_family_evidence.go: the
// RECONCILES_FROM correlation edge to its source CR (FluxGitRepository /
// FluxOCIRepository / FluxBucket) is a separate, later change.
func parseFluxKustomization(document map[string]any, metadata map[string]any, path string, lineNumber int) map[string]any {
	spec, _ := document["spec"].(map[string]any)
	sourceRef, _ := spec["sourceRef"].(map[string]any)

	row := map[string]any{
		// name kept present for row-shape stability; "" (not "<nil>") when the
		// manifest uses metadata.generateName instead of metadata.name.
		"name":        cleanYAMLString(metadata["name"]),
		"line_number": lineNumber,
		"path":        path,
		"lang":        "yaml",
	}
	if generateName := cleanYAMLString(metadata["generateName"]); generateName != "" {
		row["generate_name"] = generateName
	}
	// metadata.namespace is injected at apply-time far more often than it is
	// written in the manifest, so an absent namespace is the common case: omit
	// it, never fabricate "<nil>" (fmt.Sprint(nil)) or an empty string. This is
	// distinct from spec.sourceRef.namespace (source_ref_namespace) captured
	// below; the Flux default that fills an empty sourceRef namespace from the
	// Kustomization's own namespace is a reducer rule, not a parser fabrication.
	if namespace := cleanYAMLString(metadata["namespace"]); namespace != "" {
		row["namespace"] = namespace
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
		row["source_path"] = specPath
	}
	if targetNamespace := cleanYAMLString(spec["targetNamespace"]); targetNamespace != "" {
		row["target_namespace"] = targetNamespace
	}
	if labels := collectMetadataLabels(metadata); labels != "" {
		row["labels"] = labels
	}
	return row
}
