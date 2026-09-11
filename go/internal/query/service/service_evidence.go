// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/service/evidence"
)

func LoadServiceQueryEvidence(
	ctx context.Context,
	reader ServiceEvidenceReader,
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

	collected := ServiceQueryEvidence{filesTruncated: filesTruncated}
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
			collected.EntrypointCandidates = append(collected.EntrypointCandidates, ServiceEntrypointCandidateEvidence{
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
			collected.Hostnames = append(collected.Hostnames, ServiceHostnameEvidence{
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
			collected.Environments = append(collected.Environments, ServiceEnvironmentEvidence{
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
			collected.DocsRoutes = append(collected.DocsRoutes, ServiceDocsRouteEvidence{
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
			collected.APISpecs = append(collected.APISpecs, spec)
		}
	}

	sort.Slice(collected.Hostnames, func(i, j int) bool {
		if collected.Hostnames[i].Hostname != collected.Hostnames[j].Hostname {
			return collected.Hostnames[i].Hostname < collected.Hostnames[j].Hostname
		}
		return collected.Hostnames[i].RelativePath < collected.Hostnames[j].RelativePath
	})
	sort.Slice(collected.Environments, func(i, j int) bool {
		if collected.Environments[i].Environment != collected.Environments[j].Environment {
			return collected.Environments[i].Environment < collected.Environments[j].Environment
		}
		return collected.Environments[i].RelativePath < collected.Environments[j].RelativePath
	})
	sort.Slice(collected.DocsRoutes, func(i, j int) bool {
		if collected.DocsRoutes[i].Route != collected.DocsRoutes[j].Route {
			return collected.DocsRoutes[i].Route < collected.DocsRoutes[j].Route
		}
		return collected.DocsRoutes[i].RelativePath < collected.DocsRoutes[j].RelativePath
	})
	sort.Slice(collected.EntrypointCandidates, func(i, j int) bool {
		if collected.EntrypointCandidates[i].Candidate != collected.EntrypointCandidates[j].Candidate {
			return collected.EntrypointCandidates[i].Candidate < collected.EntrypointCandidates[j].Candidate
		}
		if collected.EntrypointCandidates[i].Classification != collected.EntrypointCandidates[j].Classification {
			return collected.EntrypointCandidates[i].Classification < collected.EntrypointCandidates[j].Classification
		}
		return collected.EntrypointCandidates[i].RelativePath < collected.EntrypointCandidates[j].RelativePath
	})
	sort.Slice(collected.APISpecs, func(i, j int) bool {
		return collected.APISpecs[i].RelativePath < collected.APISpecs[j].RelativePath
	})

	return collected, nil
}

func isServiceEvidenceCandidate(file querycontract.FileContent, normalizedServiceName string) bool {
	return querycontract.IsServiceEvidenceCandidate(file, normalizedServiceName)
}

// extractAPISpecEvidence summarizes one candidate API spec file, resolving
// external `$ref` path entries through resolver first. The implementation
// moved to the service/evidence leaf for #6060; this wrapper keeps root
// callers unchanged.
func extractAPISpecEvidence(file querycontract.FileContent, resolver specFileResolver) (ServiceAPISpecEvidence, bool, error) {
	return evidence.ExtractAPISpecEvidence(file, resolver)
}
