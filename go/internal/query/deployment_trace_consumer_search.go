package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

func searchConsumerEvidenceAnyRepo(
	ctx context.Context,
	content ContentStore,
	serviceRepoID string,
	serviceName string,
	hostnames []string,
	limit int,
) (map[string]traceEvidenceAccumulator, error) {
	evidenceByRepo := map[string]traceEvidenceAccumulator{}
	if content == nil {
		return evidenceByRepo, nil
	}
	if limit <= 0 {
		limit = defaultIndirectEvidenceSearchLimit
	}

	searches := []consumerEvidenceSearch{}
	if serviceName = strings.TrimSpace(serviceName); serviceName != "" {
		searches = append(searches, consumerEvidenceSearch{
			matchedValue: serviceName,
			evidenceKind: "repository_reference",
			exactCase:    exactCaseServiceNameSearch(serviceName),
		})
	}
	for _, hostname := range hostnames {
		searches = append(searches, consumerEvidenceSearch{
			matchedValue: hostname,
			evidenceKind: "hostname_reference",
			exactCase:    true,
		})
	}
	if len(searches) == 0 {
		return evidenceByRepo, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan consumerEvidenceSearchResult, len(searches))
	var wg sync.WaitGroup
	for _, search := range searches {
		search := search
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := runConsumerEvidenceSearch(ctx, content, search, limit)
			if err != nil {
				cancel()
				results <- consumerEvidenceSearchResult{err: err}
				return
			}
			evidence := map[string]traceEvidenceAccumulator{}
			collectSearchRowsByRepo(evidence, rows, serviceRepoID, search.evidenceKind, search.matchedValue)
			results <- consumerEvidenceSearchResult{evidence: evidence}
		}()
	}
	wg.Wait()
	close(results)

	for result := range results {
		if result.err != nil {
			return evidenceByRepo, result.err
		}
		mergeTraceEvidenceByRepo(evidenceByRepo, result.evidence)
	}
	return evidenceByRepo, nil
}

func exactCaseServiceNameSearch(serviceName string) bool {
	return serviceName != "" && serviceName == strings.ToLower(serviceName)
}

type consumerEvidenceSearch struct {
	matchedValue string
	evidenceKind string
	exactCase    bool
}

type consumerEvidenceSearchResult struct {
	evidence map[string]traceEvidenceAccumulator
	err      error
}

type contentReferenceSearchStore interface {
	SearchFileReferenceAnyRepo(ctx context.Context, kind string, value string, limit int) ([]FileContent, bool, error)
}

func runConsumerEvidenceSearch(
	ctx context.Context,
	content ContentStore,
	search consumerEvidenceSearch,
	limit int,
) ([]FileContent, error) {
	if search.evidenceKind == "repository_reference" && search.exactCase {
		if indexed, ok := content.(contentReferenceSearchStore); ok {
			rows, available, err := indexed.SearchFileReferenceAnyRepo(ctx, "service_name", search.matchedValue, limit)
			if err != nil {
				return nil, fmt.Errorf("search indexed consumer evidence for service name %q: %w", search.matchedValue, err)
			}
			if available {
				return rows, nil
			}
		}
	}
	if search.evidenceKind == "hostname_reference" {
		if indexed, ok := content.(contentReferenceSearchStore); ok {
			rows, available, err := indexed.SearchFileReferenceAnyRepo(ctx, "hostname", search.matchedValue, limit)
			if err != nil {
				return nil, fmt.Errorf("search indexed consumer evidence for hostname %q: %w", search.matchedValue, err)
			}
			if available {
				return rows, nil
			}
		}
	}
	if search.exactCase {
		rows, err := content.SearchFileContentAnyRepoExactCase(ctx, search.matchedValue, limit)
		if err != nil {
			return nil, fmt.Errorf("search consumer evidence for hostname %q: %w", search.matchedValue, err)
		}
		return rows, nil
	}
	rows, err := content.SearchFileContentAnyRepo(ctx, search.matchedValue, limit)
	if err != nil {
		return nil, fmt.Errorf("search consumer evidence for service name: %w", err)
	}
	return rows, nil
}

func mergeTraceEvidenceByRepo(
	dst map[string]traceEvidenceAccumulator,
	src map[string]traceEvidenceAccumulator,
) {
	for repoID, evidence := range src {
		existing, ok := dst[repoID]
		if !ok {
			dst[repoID] = evidence
			continue
		}
		mergeStringSet(existing.samplePaths, evidence.samplePaths)
		mergeStringSet(existing.evidenceKinds, evidence.evidenceKinds)
		mergeStringSet(existing.modules, evidence.modules)
		mergeStringSet(existing.configPaths, evidence.configPaths)
		mergeStringSet(existing.matchedValues, evidence.matchedValues)
		dst[repoID] = existing
	}
}

func mergeStringSet(dst, src map[string]struct{}) {
	for value := range src {
		dst[value] = struct{}{}
	}
}

func collectSearchRowsByRepo(
	evidenceByRepo map[string]traceEvidenceAccumulator,
	rows []FileContent,
	serviceRepoID string,
	evidenceKind string,
	matchedValue string,
) {
	if len(rows) == 0 {
		return
	}
	for _, row := range rows {
		repoID := strings.TrimSpace(row.RepoID)
		if repoID == "" || repoID == strings.TrimSpace(serviceRepoID) {
			continue
		}
		evidence, ok := evidenceByRepo[repoID]
		if !ok {
			evidence = newTraceEvidenceAccumulator()
		}
		evidence.evidenceKinds[evidenceKind] = struct{}{}
		if matchedValue != "" {
			evidence.matchedValues[matchedValue] = struct{}{}
		}
		if relativePath := strings.TrimSpace(row.RelativePath); relativePath != "" {
			evidence.samplePaths[relativePath] = struct{}{}
		}
		evidenceByRepo[repoID] = evidence
	}
}

func appendConsumerEvidence(entry map[string]any, evidence traceEvidenceAccumulator) {
	evidenceKinds := sortedAccumulatorValues(evidence.evidenceKinds)
	if len(evidenceKinds) > 0 {
		entry["evidence_kinds"] = evidenceKinds
	}
	matchedValues := sortedAccumulatorValues(evidence.matchedValues)
	if len(matchedValues) > 0 {
		entry["matched_values"] = matchedValues
	}
	samplePaths := sortedAccumulatorValues(evidence.samplePaths)
	if len(samplePaths) > 0 {
		entry["sample_paths"] = samplePaths
	}

	consumerKinds := StringSliceVal(entry, "consumer_kinds")
	if containsString(evidenceKinds, "repository_reference") {
		appendUniqueString(&consumerKinds, "service_reference_consumer")
	}
	if containsString(evidenceKinds, "hostname_reference") {
		appendUniqueString(&consumerKinds, "hostname_reference_consumer")
	}
	sort.Strings(consumerKinds)
	entry["consumer_kinds"] = consumerKinds
}

func consumerRepositorySortScore(entry map[string]any) int {
	score := 0
	for _, kind := range StringSliceVal(entry, "consumer_kinds") {
		switch kind {
		case "graph_provisioning_consumer":
			score += 100
		case "service_reference_consumer", "hostname_reference_consumer":
			score += 15
		default:
			score += 5
		}
	}
	score += len(StringSliceVal(entry, "graph_relationship_types")) * 10
	score += len(StringSliceVal(entry, "evidence_kinds")) * 5
	score += len(StringSliceVal(entry, "matched_values")) * 3
	score += len(StringSliceVal(entry, "sample_paths"))
	return score
}

func newTraceEvidenceAccumulator() traceEvidenceAccumulator {
	return traceEvidenceAccumulator{
		samplePaths:   map[string]struct{}{},
		evidenceKinds: map[string]struct{}{},
		modules:       map[string]struct{}{},
		configPaths:   map[string]struct{}{},
		matchedValues: map[string]struct{}{},
	}
}

func sortedAccumulatorValues(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	items := make([]string, 0, len(values))
	for value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	sort.Strings(items)
	return items
}

func appendUniqueString(values *[]string, candidate string) {
	if candidate = strings.TrimSpace(candidate); candidate == "" {
		return
	}
	for _, existing := range *values {
		if existing == candidate {
			return
		}
	}
	*values = append(*values, candidate)
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
