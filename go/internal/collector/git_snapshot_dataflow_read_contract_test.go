// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// reachingDefPayloadKeys are the code_dataflow_function payload keys the
// reaching-def read consumes (query.codeFlowFunctionFromPayload, and
// query.codeFlowReachingPayloads for def_use). They are named here because a
// collector test cannot import the query package — query already depends on
// collector — so this list plus its mirror,
// query.TestReachingDefAnswersFromCollectorPayloadShape, is what keeps the two
// sides of the seam honest.
//
// def_use is the load-bearing one: a reaching-def answer with an empty def_use
// is an answer with no reaching definitions in it, which reads as "this
// function has none" rather than "the producer and the reader disagree".
var reachingDefPayloadKeys = []string{
	"repo_id",
	"relative_path",
	"function_name",
	"language",
	"line_number",
	"def_use",
}

// TestRealParserDataflowFactCarriesTheReachingDefReadContract closes the last
// unproven link in issue #5692's chain.
//
// TestDataflowFunctionFactEmittedAndCounted proves a DataflowFunctionSnapshot
// becomes a fact, but it hand-builds that snapshot. So the shape the REAL
// parser produces was never checked against the shape the read consumes, and a
// parser whose rows did not carry def_use would have produced a fact, passed
// that test, and still answered reaching_def with nothing in it.
//
// This drives the real parser through the real snapshotter with the gate on,
// streams the real facts, and asserts the emitted payload carries everything
// the reaching-def read reads — with a non-empty def_use.
func TestRealParserDataflowFactCarriesTheReachingDefReadContract(t *testing.T) {
	t.Parallel()

	snapshot := dataflowGateChainSnapshot(t, true)
	if len(snapshot.DataflowFunctions) == 0 {
		t.Fatal("real parser produced no dataflow functions with the gate on")
	}

	// The fact builder must see the same repository root the snapshotter
	// parsed. A fresh temp dir here would leave the emitted envelopes' source
	// URIs and generation metadata pointing at a directory the snapshot never
	// came from, and would hide any repo-root-dependent behaviour in
	// buildStreamingGeneration.
	repoPath := snapshot.RepoPath
	observedAt := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	envelopes := drainFactChannel(
		buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false, "").Facts,
	)

	var dataflowFacts []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.CodeDataflowFunctionFactKind {
			dataflowFacts = append(dataflowFacts, envelope)
		}
	}
	if len(dataflowFacts) == 0 {
		t.Fatalf("no %s fact emitted from a real parser snapshot", facts.CodeDataflowFunctionFactKind)
	}

	var withDefUse int
	for _, envelope := range dataflowFacts {
		for _, key := range reachingDefPayloadKeys {
			if _, present := envelope.Payload[key]; !present {
				t.Errorf("payload is missing %q, which the reaching-def read consumes: %+v", key, envelope.Payload)
			}
		}
		if rows, ok := envelope.Payload["def_use"].([]map[string]any); ok && len(rows) > 0 {
			withDefUse++
		}
	}
	if withDefUse == 0 {
		t.Fatalf("no emitted fact carries a non-empty def_use, so reaching_def would answer with no definitions: %+v", dataflowFacts[0].Payload)
	}
}
