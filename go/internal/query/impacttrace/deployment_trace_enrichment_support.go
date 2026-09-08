// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"context"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repositoryreadmodel"
)

// This file hosts the accumulator and chain-evidence support behind the
// deployment-trace enrichment entry points (Issue #6060, lane B B4). They
// moved here from the query root (deployment_trace_support_helpers.go)
// with the entry points because the service family consumes them through
// those entries and a handler-family subpackage cannot import the query
// root without an import cycle. Bodies are unchanged modulo package
// qualifiers.

type traceEvidenceAccumulator struct {
	samplePaths   map[string]struct{}
	evidenceKinds map[string]struct{}
	modules       map[string]struct{}
	configPaths   map[string]struct{}
	matchedValues map[string]struct{}
}

func collectProvisioningChainEvidence(entities []querycontract.EntityContent) traceEvidenceAccumulator {
	evidence := newTraceEvidenceAccumulator()
	for _, entity := range entities {
		switch entity.EntityType {
		case "TerraformModule", "TerragruntConfig", "TerragruntDependency":
		default:
			continue
		}
		evidence.samplePaths[entity.RelativePath] = struct{}{}
		relationships, handled, err := BuildOutgoingTerraformRelationships(entity)
		if err != nil || !handled {
			continue
		}
		for _, relationship := range relationships {
			if reason := strings.TrimSpace(querycontract.StringVal(relationship, "reason")); reason != "" {
				evidence.evidenceKinds[reason] = struct{}{}
			}
			targetName := strings.TrimSpace(querycontract.StringVal(relationship, "target_name"))
			switch querycontract.StringVal(relationship, "type") {
			case "USES_MODULE":
				if targetName != "" {
					evidence.modules[targetName] = struct{}{}
				}
			case "DISCOVERS_CONFIG_IN":
				if targetName != "" {
					evidence.configPaths[targetName] = struct{}{}
				}
			case "READS_CONFIG_FROM":
				if targetName != "" {
					evidence.configPaths[targetName] = struct{}{}
				}
			}
		}
	}
	return evidence
}

func normalizedIndirectEvidenceHostnames(hostnames []string) []string {
	if len(hostnames) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(hostnames))
	for _, hostname := range hostnames {
		hostname = strings.TrimSpace(hostname)
		if hostname == "" {
			continue
		}
		if _, ok := seen[hostname]; ok {
			continue
		}
		seen[hostname] = struct{}{}
		normalized = append(normalized, hostname)
	}
	sort.Strings(normalized)
	return normalized
}

func backfillConsumerRepositoryDisplayNames(
	ctx context.Context,
	graph querycontract.GraphQuery,
	consumersByRepo map[string]map[string]any,
) error {
	if graph == nil || len(consumersByRepo) == 0 {
		return nil
	}

	repoIDs := make([]string, 0, len(consumersByRepo))
	for repoID, entry := range consumersByRepo {
		if repoID == "" {
			continue
		}
		repository := querycontract.StringVal(entry, "repository")
		repoName := querycontract.StringVal(entry, "repo_name")
		if repoName == "" || repository == "" || repository == repoID {
			repoIDs = append(repoIDs, repoID)
		}
	}

	namesByID, err := repositoryreadmodel.QueryRepositoryNamesByID(ctx, graph, repoIDs)
	if err != nil {
		return err
	}
	for repoID, repoName := range namesByID {
		entry := consumersByRepo[repoID]
		if entry == nil || repoName == "" {
			continue
		}
		entry["repo_name"] = repoName
		if repository := querycontract.StringVal(entry, "repository"); repository == "" || repository == repoID {
			entry["repository"] = repoName
		}
	}
	return nil
}
