// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"context"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// crossRepoDeadCodeRootPathStore reads the file of consumer root entities so a
// row can say that only tests consume it (#7603). The content store implements
// it beside crossRepoDeadCodeEvidenceStore. A store that does not, or a root it
// has no row for, leaves the flag off: the flag is a fact that was proven, never
// a default. Keys of the result are root entity ids, values their relative path.
type crossRepoDeadCodeRootPathStore interface {
	CrossRepoDeadCodeConsumerRootPaths(
		ctx context.Context,
		rootEntityIDs []string,
		consumerRepoIDs []string,
	) (map[string]string, error)
}

// crossRepoDeadCodeConsumerSetBounded reports whether the answer may not see
// every consumer of a symbol, so test_only_consumers cannot be proven: the
// request named consumer_repo_ids (the read is bound to them and the hidden
// consumer probe does not run), or a consumer repository's snapshot is
// incomplete. Strong evidence still makes a row live in both cases, because
// liveness needs one consumer; "only tests call this" needs all of them. The
// handler skips the root path read and the bucketing pass leaves the flag off.
// "Named" here means len(req.ConsumerRepoIDs) > 0; it is read from the request
// because the coverage result carries the selector only when the coverage check
// ran.
func crossRepoDeadCodeConsumerSetBounded(
	req CrossRepoDeadCodeRequest,
	coverage crossRepoDeadCodeConsumerCoverageResult,
) bool {
	return len(req.ConsumerRepoIDs) > 0 || coverage.incomplete()
}

// crossRepoDeadCodeConsumerRootPaths runs the one batched root path read a
// request needs: every distinct consumer root of an evidence item that names
// one, and the consumer repositories those roots belong to. It runs once per
// request, never per candidate, and not at all when no candidate has a consumer
// root. Needs-evidence items (the truncation marker among them) carry no usable
// root and are skipped, since they keep their row out of live_by_consumer.
func (a *Analyzer) crossRepoDeadCodeConsumerRootPaths(
	ctx context.Context,
	evidence map[string][]CrossRepoDeadCodeEvidence,
) (map[string]string, error) {
	store, ok := a.deps.Content.(crossRepoDeadCodeRootPathStore)
	if !ok {
		return nil, nil
	}
	var rootIDs, repoIDs []string
	for _, items := range evidence {
		for _, item := range items {
			if item.NeedsEvidence || item.ConsumerEntityID == "" || item.ConsumerRepoID == "" {
				continue
			}
			rootIDs = append(rootIDs, item.ConsumerEntityID)
			repoIDs = append(repoIDs, item.ConsumerRepoID)
		}
	}
	if len(rootIDs) == 0 {
		return nil, nil
	}
	slices.Sort(rootIDs)
	slices.Sort(repoIDs)
	return store.CrossRepoDeadCodeConsumerRootPaths(ctx, slices.Compact(rootIDs), slices.Compact(repoIDs))
}

// crossRepoDeadCodeTestOnlyConsumers reports whether every consumer of a live
// row has a root entity in a test file, by the single test-path rule
// codemodel.DeadCodeIsTestFile. It never changes liveness: a test caller is a
// caller. A consumer whose root is unnamed or has no path row is not proven a
// test, so one such consumer keeps the flag off.
func crossRepoDeadCodeTestOnlyConsumers(visible []CrossRepoDeadCodeEvidence, rootPaths map[string]string) bool {
	if len(visible) == 0 {
		return false
	}
	for _, item := range visible {
		path, ok := rootPaths[item.ConsumerEntityID]
		if !ok || item.ConsumerEntityID == "" ||
			!codemodel.DeadCodeIsTestFile(nil, &querycontract.EntityContent{RelativePath: path}) {
			return false
		}
	}
	return true
}

func crossRepoDeadCodeHasStrongLiveEvidence(evidence []CrossRepoDeadCodeEvidence) bool {
	for _, item := range evidence {
		if item.NeedsEvidence || item.Ambiguous || !strings.EqualFold(item.GenerationStatus, "active") {
			continue
		}
		if item.Confidence > codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName) {
			return true
		}
	}
	return false
}

func crossRepoDeadCodeStrongestConfidenceLabel(evidence []CrossRepoDeadCodeEvidence) string {
	best := 0.0
	label := ""
	for _, item := range evidence {
		if item.Confidence > best {
			best = item.Confidence
			label = item.ConfidenceLabel
		}
	}
	if label == "" {
		return CrossRepoDeadCodeConfidenceLabel(best)
	}
	return label
}

// CrossRepoDeadCodeConfidenceLabel maps a numeric consumer-evidence confidence
// to its response label: high at 0.9 and above, medium above the repository
// unique-name confidence, low for any other positive value, and unknown for zero.
func CrossRepoDeadCodeConfidenceLabel(confidence float64) string {
	switch {
	case confidence >= 0.9:
		return "high"
	case confidence > codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName):
		return "medium"
	case confidence > 0:
		return "low"
	default:
		return "unknown"
	}
}

func crossRepoDeadCodeConsumerSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	return set
}

func cleanCrossRepoDeadCodeStrings(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}
