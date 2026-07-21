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
// flux_git_repositories. It follows the exact pattern
// decode_parsed_file_data.go established for the #4750 S1 batch -- see that
// file's package-level doc comment for the tolerant-decode contract (absent
// key -> empty/false with no error, present-but-wrong-shape -> a wrapped
// error, malformed element -> a wrapped error).

// DecodeParsedFileDataHelmCharts decodes the "helm_charts" inner slice of a
// parsed_file_data map into a typed []codegraphv1.HelmChart. An absent key
// decodes to a nil slice with no error, matching
// discoverStructuredHelmEvidence's prior raw-map tolerant read
// (go/internal/relationships/structured_family_evidence.go).
func DecodeParsedFileDataHelmCharts(parsedFileData map[string]any) ([]codegraphv1.HelmChart, error) {
	raw, present := parsedFileData["helm_charts"]
	if !present || raw == nil {
		return nil, nil
	}
	elems, ok := asObjectSlice(raw)
	if !ok {
		return nil, fmt.Errorf("factschema: helm_charts: want slice of JSON objects, got %T", raw)
	}
	charts := make([]codegraphv1.HelmChart, 0, len(elems))
	for i, elem := range elems {
		var chart codegraphv1.HelmChart
		if err := decodeMapInto(elem, &chart); err != nil {
			return nil, fmt.Errorf("factschema: helm_charts[%d]: %w", i, err)
		}
		charts = append(charts, chart)
	}
	return charts, nil
}

// DecodeParsedFileDataHelmValues decodes the "helm_values" inner slice of a
// parsed_file_data map into a typed []codegraphv1.HelmValues. An absent key
// decodes to a nil slice with no error, matching
// discoverStructuredHelmEvidence's prior raw-map tolerant read
// (go/internal/relationships/structured_family_evidence.go).
func DecodeParsedFileDataHelmValues(parsedFileData map[string]any) ([]codegraphv1.HelmValues, error) {
	raw, present := parsedFileData["helm_values"]
	if !present || raw == nil {
		return nil, nil
	}
	elems, ok := asObjectSlice(raw)
	if !ok {
		return nil, fmt.Errorf("factschema: helm_values: want slice of JSON objects, got %T", raw)
	}
	values := make([]codegraphv1.HelmValues, 0, len(elems))
	for i, elem := range elems {
		var value codegraphv1.HelmValues
		if err := decodeMapInto(elem, &value); err != nil {
			return nil, fmt.Errorf("factschema: helm_values[%d]: %w", i, err)
		}
		values = append(values, value)
	}
	return values, nil
}

// DecodeParsedFileDataArgoCDApplications decodes the "argocd_applications"
// inner slice of a parsed_file_data map into a typed
// []codegraphv1.ArgoCDApplication. An absent key decodes to a nil slice with
// no error, matching discoverStructuredArgoCDEvidence's prior raw-map
// tolerant read (go/internal/relationships/structured_family_evidence.go).
func DecodeParsedFileDataArgoCDApplications(parsedFileData map[string]any) ([]codegraphv1.ArgoCDApplication, error) {
	raw, present := parsedFileData["argocd_applications"]
	if !present || raw == nil {
		return nil, nil
	}
	elems, ok := asObjectSlice(raw)
	if !ok {
		return nil, fmt.Errorf("factschema: argocd_applications: want slice of JSON objects, got %T", raw)
	}
	applications := make([]codegraphv1.ArgoCDApplication, 0, len(elems))
	for i, elem := range elems {
		var application codegraphv1.ArgoCDApplication
		if err := decodeMapInto(elem, &application); err != nil {
			return nil, fmt.Errorf("factschema: argocd_applications[%d]: %w", i, err)
		}
		applications = append(applications, application)
	}
	return applications, nil
}

// DecodeParsedFileDataArgoCDApplicationSets decodes the
// "argocd_applicationsets" inner slice of a parsed_file_data map into a
// typed []codegraphv1.ArgoCDApplicationSet. An absent key decodes to a nil
// slice with no error, matching two independent prior raw-map tolerant
// readers: discoverStructuredArgoCDEvidence
// (go/internal/relationships/structured_family_evidence.go) and
// structuredApplicationSetGeneratorRepos
// (go/internal/relationships/argocd_generator_config.go).
func DecodeParsedFileDataArgoCDApplicationSets(parsedFileData map[string]any) ([]codegraphv1.ArgoCDApplicationSet, error) {
	raw, present := parsedFileData["argocd_applicationsets"]
	if !present || raw == nil {
		return nil, nil
	}
	elems, ok := asObjectSlice(raw)
	if !ok {
		return nil, fmt.Errorf("factschema: argocd_applicationsets: want slice of JSON objects, got %T", raw)
	}
	appSets := make([]codegraphv1.ArgoCDApplicationSet, 0, len(elems))
	for i, elem := range elems {
		var appSet codegraphv1.ArgoCDApplicationSet
		if err := decodeMapInto(elem, &appSet); err != nil {
			return nil, fmt.Errorf("factschema: argocd_applicationsets[%d]: %w", i, err)
		}
		appSets = append(appSets, appSet)
	}
	return appSets, nil
}

// DecodeParsedFileDataFluxGitRepositories decodes the "flux_git_repositories"
// inner slice of a parsed_file_data map into a typed
// []codegraphv1.FluxGitRepository. An absent key decodes to a nil slice with
// no error, matching discoverStructuredFluxEvidence's prior raw-map tolerant
// read (go/internal/relationships/flux_evidence.go).
func DecodeParsedFileDataFluxGitRepositories(parsedFileData map[string]any) ([]codegraphv1.FluxGitRepository, error) {
	raw, present := parsedFileData["flux_git_repositories"]
	if !present || raw == nil {
		return nil, nil
	}
	elems, ok := asObjectSlice(raw)
	if !ok {
		return nil, fmt.Errorf("factschema: flux_git_repositories: want slice of JSON objects, got %T", raw)
	}
	gitRepositories := make([]codegraphv1.FluxGitRepository, 0, len(elems))
	for i, elem := range elems {
		var gitRepository codegraphv1.FluxGitRepository
		if err := decodeMapInto(elem, &gitRepository); err != nil {
			return nil, fmt.Errorf("factschema: flux_git_repositories[%d]: %w", i, err)
		}
		gitRepositories = append(gitRepositories, gitRepository)
	}
	return gitRepositories, nil
}
