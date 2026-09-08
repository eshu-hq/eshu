// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/serviceevidence"
)

func loadServiceQueryEvidence(
	ctx context.Context,
	reader serviceEvidenceReader,
	repoID string,
	serviceName string,
) (ServiceQueryEvidence, error) {
	if reader == nil || repoID == "" {
		return ServiceQueryEvidence{}, nil
	}

	files, filesTruncated, err := listServiceEvidenceFiles(ctx, reader, repoID)
	if err != nil {
		return ServiceQueryEvidence{}, err
	}

	evidence := ServiceQueryEvidence{filesTruncated: filesTruncated}
	seenHostnames := map[string]struct{}{}
	seenEntrypointCandidates := map[string]struct{}{}
	seenEnvironments := map[string]struct{}{}
	seenDocsRoutes := map[string]struct{}{}
	seenSpecs := map[string]struct{}{}
	normalizedServiceName := querycontract.NormalizeEvidenceToken(serviceName)

	for _, file := range files {
		if !isServiceEvidenceCandidate(file, normalizedServiceName) {
			continue
		}

		hydrated := file
		if strings.TrimSpace(hydrated.Content) == "" {
			fileContent, err := reader.GetFileContent(ctx, repoID, file.RelativePath)
			if err != nil {
				return ServiceQueryEvidence{}, fmt.Errorf("get service evidence file %q: %w", file.RelativePath, err)
			}
			if fileContent == nil {
				continue
			}
			hydrated = *fileContent
		}

		hostnameCandidates := extractObservedHostnameCandidates(hydrated.Content)
		hostnames := exactObservedHostnameCandidates(hostnameCandidates)
		environments := inferObservedEnvironments(hydrated.RelativePath, hydrated.Content, hostnames)
		for _, candidate := range hostnameCandidates {
			if candidate.Classification == "exact_hostname" {
				continue
			}
			key := candidate.Value + "\x00" + candidate.Classification + "\x00" + hydrated.RelativePath
			if _, ok := seenEntrypointCandidates[key]; ok {
				continue
			}
			seenEntrypointCandidates[key] = struct{}{}
			evidence.EntrypointCandidates = append(evidence.EntrypointCandidates, ServiceEntrypointCandidateEvidence{
				Candidate:      candidate.Value,
				Classification: candidate.Classification,
				RelativePath:   hydrated.RelativePath,
				Reason:         candidate.Reason,
			})
		}
		for _, hostname := range hostnames {
			environment := inferHostnameEnvironment(hostname)
			if environment == "" && len(environments) > 0 {
				environment = environments[0]
			}
			if _, ok := seenHostnames[hostname]; ok {
				continue
			}
			seenHostnames[hostname] = struct{}{}
			evidence.Hostnames = append(evidence.Hostnames, ServiceHostnameEvidence{
				Hostname:     hostname,
				Environment:  environment,
				RelativePath: hydrated.RelativePath,
				Reason:       exactHostnameCandidateReason(hostnameCandidates, hostname),
			})
		}

		for _, environment := range environments {
			if _, ok := seenEnvironments[environment]; ok {
				continue
			}
			seenEnvironments[environment] = struct{}{}
			evidence.Environments = append(evidence.Environments, ServiceEnvironmentEvidence{
				Environment:  environment,
				RelativePath: hydrated.RelativePath,
				Reason:       "path_or_content_environment_signal",
			})
		}

		for _, route := range extractDocsRoutes(hydrated.Content) {
			if _, ok := seenDocsRoutes[route]; ok {
				continue
			}
			seenDocsRoutes[route] = struct{}{}
			evidence.DocsRoutes = append(evidence.DocsRoutes, ServiceDocsRouteEvidence{
				Route:        route,
				RelativePath: hydrated.RelativePath,
				Reason:       "docs_route_reference",
			})
		}

		spec, ok, err := extractAPISpecEvidence(hydrated, buildSpecFileResolver(ctx, reader, repoID))
		if err != nil {
			return ServiceQueryEvidence{}, fmt.Errorf("extract api spec evidence %q: %w", hydrated.RelativePath, err)
		}
		if ok {
			key := spec.RelativePath
			if _, ok := seenSpecs[key]; ok {
				continue
			}
			seenSpecs[key] = struct{}{}
			evidence.APISpecs = append(evidence.APISpecs, spec)
		}
	}

	sort.Slice(evidence.Hostnames, func(i, j int) bool {
		if evidence.Hostnames[i].Hostname != evidence.Hostnames[j].Hostname {
			return evidence.Hostnames[i].Hostname < evidence.Hostnames[j].Hostname
		}
		return evidence.Hostnames[i].RelativePath < evidence.Hostnames[j].RelativePath
	})
	sort.Slice(evidence.Environments, func(i, j int) bool {
		if evidence.Environments[i].Environment != evidence.Environments[j].Environment {
			return evidence.Environments[i].Environment < evidence.Environments[j].Environment
		}
		return evidence.Environments[i].RelativePath < evidence.Environments[j].RelativePath
	})
	sort.Slice(evidence.DocsRoutes, func(i, j int) bool {
		if evidence.DocsRoutes[i].Route != evidence.DocsRoutes[j].Route {
			return evidence.DocsRoutes[i].Route < evidence.DocsRoutes[j].Route
		}
		return evidence.DocsRoutes[i].RelativePath < evidence.DocsRoutes[j].RelativePath
	})
	sort.Slice(evidence.EntrypointCandidates, func(i, j int) bool {
		if evidence.EntrypointCandidates[i].Candidate != evidence.EntrypointCandidates[j].Candidate {
			return evidence.EntrypointCandidates[i].Candidate < evidence.EntrypointCandidates[j].Candidate
		}
		if evidence.EntrypointCandidates[i].Classification != evidence.EntrypointCandidates[j].Classification {
			return evidence.EntrypointCandidates[i].Classification < evidence.EntrypointCandidates[j].Classification
		}
		return evidence.EntrypointCandidates[i].RelativePath < evidence.EntrypointCandidates[j].RelativePath
	})
	sort.Slice(evidence.APISpecs, func(i, j int) bool {
		return evidence.APISpecs[i].RelativePath < evidence.APISpecs[j].RelativePath
	})

	return evidence, nil
}

func isServiceEvidenceCandidate(file FileContent, normalizedServiceName string) bool {
	return querycontract.IsServiceEvidenceCandidate(file, normalizedServiceName)
}

// extractAPISpecEvidence summarizes one candidate API spec file, resolving
// external `$ref` path entries through resolver first. The implementation
// moved to the serviceevidence leaf for #6060; this wrapper keeps root
// callers unchanged.
func extractAPISpecEvidence(file FileContent, resolver specFileResolver) (ServiceAPISpecEvidence, bool, error) {
	return serviceevidence.ExtractAPISpecEvidence(file, resolver)
}
