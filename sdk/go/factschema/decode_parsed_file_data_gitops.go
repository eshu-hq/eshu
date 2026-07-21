// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"fmt"

	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// This file holds the typed accessors for five more closed-shape
// parsed_file_data inner keys (issue #5445 slice 1): helm_charts,
// helm_values, argocd_applications, argocd_applicationsets, and
// flux_git_repositories. Each uses decodeParsedFileDataTolerantSlice
// (decode_parsed_file_data_tolerant.go), NOT the abort-on-first-malformed
// -element asObjectSlice the #4750 S1 batch uses -- see that helper's doc
// comment for why: these accessors migrate real go/internal/relationships
// call sites whose pre-typing raw-map read tolerated one malformed row in an
// otherwise-good multi-row bucket, and this batch preserves that tolerance
// exactly rather than regressing it.

// DecodeParsedFileDataHelmCharts decodes the "helm_charts" inner slice of a
// parsed_file_data map into a typed []codegraphv1.HelmChart. An absent key
// decodes to a nil slice with no error, and a malformed element is skipped
// rather than failing the whole decode, matching
// discoverStructuredHelmEvidence's prior raw-map tolerant read
// (go/internal/relationships/structured_family_evidence.go).
func DecodeParsedFileDataHelmCharts(parsedFileData map[string]any) ([]codegraphv1.HelmChart, error) {
	raw, present := parsedFileData["helm_charts"]
	if !present || raw == nil {
		return nil, nil
	}
	charts, ok := decodeParsedFileDataTolerantSlice[codegraphv1.HelmChart](raw)
	if !ok {
		return nil, fmt.Errorf("factschema: helm_charts: want slice of JSON objects, got %T", raw)
	}
	return charts, nil
}

// DecodeParsedFileDataHelmValues decodes the "helm_values" inner slice of a
// parsed_file_data map into a typed []codegraphv1.HelmValues. An absent key
// decodes to a nil slice with no error, and a malformed element is skipped
// rather than failing the whole decode, matching
// discoverStructuredHelmEvidence's prior raw-map tolerant read
// (go/internal/relationships/structured_family_evidence.go).
func DecodeParsedFileDataHelmValues(parsedFileData map[string]any) ([]codegraphv1.HelmValues, error) {
	raw, present := parsedFileData["helm_values"]
	if !present || raw == nil {
		return nil, nil
	}
	values, ok := decodeParsedFileDataTolerantSlice[codegraphv1.HelmValues](raw)
	if !ok {
		return nil, fmt.Errorf("factschema: helm_values: want slice of JSON objects, got %T", raw)
	}
	return values, nil
}

// DecodeParsedFileDataArgoCDApplications decodes the "argocd_applications"
// inner slice of a parsed_file_data map into a typed
// []codegraphv1.ArgoCDApplication. An absent key decodes to a nil slice with
// no error, and a malformed element is skipped rather than failing the whole
// decode, matching discoverStructuredArgoCDEvidence's prior raw-map
// tolerant read (go/internal/relationships/structured_family_evidence.go).
func DecodeParsedFileDataArgoCDApplications(parsedFileData map[string]any) ([]codegraphv1.ArgoCDApplication, error) {
	raw, present := parsedFileData["argocd_applications"]
	if !present || raw == nil {
		return nil, nil
	}
	applications, ok := decodeParsedFileDataTolerantSlice[codegraphv1.ArgoCDApplication](raw)
	if !ok {
		return nil, fmt.Errorf("factschema: argocd_applications: want slice of JSON objects, got %T", raw)
	}
	return applications, nil
}

// DecodeParsedFileDataArgoCDApplicationSets decodes the
// "argocd_applicationsets" inner slice of a parsed_file_data map into a
// typed []codegraphv1.ArgoCDApplicationSet. An absent key decodes to a nil
// slice with no error, and a malformed element is skipped rather than
// failing the whole decode, matching two independent prior raw-map tolerant
// readers: discoverStructuredArgoCDEvidence
// (go/internal/relationships/structured_family_evidence.go) and
// structuredApplicationSetGeneratorRepos
// (go/internal/relationships/argocd_generator_config.go).
func DecodeParsedFileDataArgoCDApplicationSets(parsedFileData map[string]any) ([]codegraphv1.ArgoCDApplicationSet, error) {
	raw, present := parsedFileData["argocd_applicationsets"]
	if !present || raw == nil {
		return nil, nil
	}
	appSets, ok := decodeParsedFileDataTolerantSlice[codegraphv1.ArgoCDApplicationSet](raw)
	if !ok {
		return nil, fmt.Errorf("factschema: argocd_applicationsets: want slice of JSON objects, got %T", raw)
	}
	return appSets, nil
}

// DecodeParsedFileDataFluxGitRepositories decodes the "flux_git_repositories"
// inner slice of a parsed_file_data map into a typed
// []codegraphv1.FluxGitRepository. An absent key decodes to a nil slice with
// no error, and a malformed element is skipped rather than failing the whole
// decode, matching discoverStructuredFluxEvidence's prior raw-map tolerant
// read (go/internal/relationships/flux_evidence.go).
func DecodeParsedFileDataFluxGitRepositories(parsedFileData map[string]any) ([]codegraphv1.FluxGitRepository, error) {
	raw, present := parsedFileData["flux_git_repositories"]
	if !present || raw == nil {
		return nil, nil
	}
	gitRepositories, ok := decodeParsedFileDataTolerantSlice[codegraphv1.FluxGitRepository](raw)
	if !ok {
		return nil, fmt.Errorf("factschema: flux_git_repositories: want slice of JSON objects, got %T", raw)
	}
	return gitRepositories, nil
}
