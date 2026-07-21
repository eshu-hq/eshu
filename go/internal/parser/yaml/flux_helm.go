// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"strings"
)

// fluxHelmToolkitGroup is the Flux CD apiVersion group for the
// helm.toolkit.fluxcd.io HelmRelease CRD. Kept in its own file (rather than
// flux.go, which owns the kustomize.toolkit.fluxcd.io Kustomization CRD) so
// neither file approaches the 500-line package limit as Flux coverage grows
// (issue #5483 C1).
const fluxHelmToolkitGroup = "helm.toolkit.fluxcd.io/"

// isFluxHelmRelease reports whether a document is a Flux CD HelmRelease
// custom resource: apiVersion group "helm.toolkit.fluxcd.io" (any version)
// with kind "HelmRelease".
func isFluxHelmRelease(apiVersion string, kind string) bool {
	return strings.HasPrefix(apiVersion, fluxHelmToolkitGroup) && kind == "HelmRelease"
}

// parseFluxHelmRelease captures a Flux HelmRelease CR's chart resolution
// evidence: spec.chart.spec (chart/version/sourceRef) OR spec.chartRef
// (kind/name/namespace), plus spec.targetNamespace.
//
// Per the Flux HelmRelease API (helm.toolkit.fluxcd.io), exactly one of
// spec.chart or spec.chartRef must be set on a valid CR. This parser makes no
// such judgment: it captures whichever fields are present verbatim, including
// both (an invalid CR) or neither. The exactly-one-of validation and the
// resulting honest non-link decision belong entirely to the edge resolver
// (go/internal/storage/cypher/canonical_flux_helm_edges.go), never here.
//
// spec.chart.spec.sourceRef uses the SAME three row keys
// (source_ref_kind/name/namespace) that parseFluxKustomization emits for its
// spec.sourceRef, since both name a Flux source CR the same way. spec.chartRef
// is captured under DISTINCT chart_ref_kind/name/namespace keys -- it must
// never be folded into source_ref_*, because a HelmRelease can carry both
// shapes (invalid, but the parser is honest capture only) and collapsing them
// would silently discard which one was actually set.
//
// The presence of a spec.chart mapping is recorded as chart_present="true"
// even when the block is EMPTY (no chart name, no sourceRef). This is the
// only signal the edge resolver has for "a spec.chart block exists" when the
// block yields no populated field, and it is load-bearing for the
// exactly-one-of chart/chartRef non-link guard: a HelmRelease with an empty
// spec.chart block AND a spec.chartRef is invalid (Flux rejects it at
// admission), and without this signal the resolver would fabricate a chartRef
// edge for a CR that can never reconcile (codex-P1). It is omitted only when
// spec.chart is entirely absent, so a legitimate chartRef-only HelmRelease
// keeps chart_present unset and resolves normally.
//
// Fields are parsed defensively: an absent or empty field is omitted from the
// row, never fabricated.
func parseFluxHelmRelease(document map[string]any, metadata map[string]any, path string, lineNumber int) map[string]any {
	spec, _ := document["spec"].(map[string]any)
	chart, chartPresent := spec["chart"].(map[string]any)
	chartSpec, _ := chart["spec"].(map[string]any)
	sourceRef, _ := chartSpec["sourceRef"].(map[string]any)
	chartRef, _ := spec["chartRef"].(map[string]any)

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
	// it, never fabricate "<nil>" (fmt.Sprint(nil)) or an empty string.
	if namespace := cleanYAMLString(metadata["namespace"]); namespace != "" {
		row["namespace"] = namespace
	}
	// Record the spec.chart block's presence even when empty (see the doc
	// comment): the resolver's exactly-one-of chart/chartRef non-link guard
	// keys on this when the block yields no chart name or sourceRef.
	if chartPresent {
		row["chart_present"] = "true"
	}
	if chartName := cleanYAMLString(chartSpec["chart"]); chartName != "" {
		row["chart"] = chartName
	}
	if version := cleanYAMLString(chartSpec["version"]); version != "" {
		row["chart_version"] = version
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
	if kind := cleanYAMLString(chartRef["kind"]); kind != "" {
		row["chart_ref_kind"] = kind
	}
	if name := cleanYAMLString(chartRef["name"]); name != "" {
		row["chart_ref_name"] = name
	}
	if namespace := cleanYAMLString(chartRef["namespace"]); namespace != "" {
		row["chart_ref_namespace"] = namespace
	}
	if targetNamespace := cleanYAMLString(spec["targetNamespace"]); targetNamespace != "" {
		row["target_namespace"] = targetNamespace
	}
	if labels := collectMetadataLabels(metadata); labels != "" {
		row["labels"] = labels
	}
	return row
}
