// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

type deadCodeSaturationProbeStore struct {
	fakeDeadCodeContentStore
	candidatePages    int
	candidateRows     int
	hydrationCalls    int
	hydrationIDs      int
	reachabilityCalls int
	reachabilityIDs   int
	labels            map[string]struct{}
}

func newDeadCodeSaturationProbeStore() *deadCodeSaturationProbeStore {
	return &deadCodeSaturationProbeStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{},
		labels:                   make(map[string]struct{}),
	}
}

func (s *deadCodeSaturationProbeStore) DeadCodeCandidateRows(
	_ context.Context,
	_ string,
	label string,
	_ string,
	limit int,
	offset int,
) ([]map[string]any, error) {
	s.candidatePages++
	s.candidateRows += limit
	s.labels[label] = struct{}{}
	rows := make([]map[string]any, limit)
	for i := range rows {
		id := fmt.Sprintf("%s-%06d", label, offset+i)
		rows[i] = map[string]any{
			"entity_id":  id,
			"name":       "_candidate_" + id,
			"labels":     []any{label},
			"file_path":  "src/" + id + ".py",
			"repo_id":    "repo-1",
			"repo_name":  "saturation-proof",
			"language":   "python",
			"start_line": int64(1),
			"end_line":   int64(2),
		}
	}
	return rows, nil
}

func (s *deadCodeSaturationProbeStore) GetEntityContents(
	_ context.Context,
	entityIDs []string,
) (map[string]*EntityContent, error) {
	s.hydrationCalls++
	s.hydrationIDs += len(entityIDs)
	entities := make(map[string]*EntityContent, len(entityIDs))
	for _, id := range entityIDs {
		entities[id] = &EntityContent{
			EntityID:     id,
			RepoID:       "repo-1",
			RelativePath: "src/" + id + ".py",
			EntityType:   deadCodeProbeLabel(id),
			EntityName:   "_candidate_" + id,
			Language:     "python",
			SourceCache:  "def _candidate():\n    pass\n",
		}
	}
	return entities, nil
}

func (s *deadCodeSaturationProbeStore) CodeReachabilityIncomingEntityIDs(
	_ context.Context,
	_ string,
	entityIDs []string,
) (map[string]deadCodeIncomingEdge, error) {
	return s.recordIncoming(entityIDs), nil
}

func (*deadCodeSaturationProbeStore) CodeReachabilityCoverage(
	context.Context,
	string,
) (codeReachabilityCoverage, error) {
	return codeReachabilityCoverage{Available: true}, nil
}

func (s *deadCodeSaturationProbeStore) DeadCodeIncomingEntityIDs(
	_ context.Context,
	_ string,
	entityIDs []string,
) (map[string]deadCodeIncomingEdge, error) {
	return s.recordIncoming(entityIDs), nil
}

func (s *deadCodeSaturationProbeStore) recordIncoming(entityIDs []string) map[string]deadCodeIncomingEdge {
	s.reachabilityCalls++
	s.reachabilityIDs += len(entityIDs)
	incoming := make(map[string]deadCodeIncomingEdge, len(entityIDs))
	for _, id := range entityIDs {
		incoming[id] = deadCodeIncomingEdge{
			MaxConfidence: codeprovenance.LegacyConfidence,
			Method:        codeprovenance.MethodUnspecified,
		}
	}
	return incoming
}

func deadCodeProbeLabel(id string) string {
	for _, label := range deadCodeCandidateLabels {
		if len(id) > len(label) && id[:len(label)] == label {
			return label
		}
	}
	return "Function"
}

type deadCodeSaturationOutcome struct {
	duration          time.Duration
	candidatePages    int
	candidateRows     int
	hydrationCalls    int
	hydrationIDs      int
	reachabilityCalls int
	reachabilityIDs   int
	labels            int
}

func TestDeadCodeRoundRobinSaturationProof(t *testing.T) {
	originalLabels := append([]string(nil), deadCodeCandidateLabels...)
	t.Cleanup(func() { deadCodeCandidateLabels = originalLabels })

	for _, scanner := range []string{"dead-code", "investigate", "cross-repo"} {
		t.Run(scanner, func(t *testing.T) {
			legacy := runDeadCodeSaturationShape(t, scanner, originalLabels[:1], false)
			widened := runDeadCodeSaturationShape(t, scanner, originalLabels, true)
			bounded := runDeadCodeSaturationShape(t, scanner, originalLabels, false)

			assertDeadCodeSaturationCounts(t, legacy, 10, 2500, 1)
			assertDeadCodeSaturationCounts(t, widened, 60, 15000, len(originalLabels))
			assertDeadCodeSaturationCounts(t, bounded, 10, 2500, len(originalLabels))
			t.Logf(
				"scanner=%s legacy=%s widened=%s bounded=%s rows/pages/probes=%d/%d/%d -> %d/%d/%d",
				scanner,
				legacy.duration,
				widened.duration,
				bounded.duration,
				widened.candidateRows,
				widened.candidatePages,
				widened.reachabilityCalls,
				bounded.candidateRows,
				bounded.candidatePages,
				bounded.reachabilityCalls,
			)
		})
	}
}

func runDeadCodeSaturationShape(
	t *testing.T,
	scanner string,
	labels []string,
	perLabel bool,
) deadCodeSaturationOutcome {
	t.Helper()
	store := newDeadCodeSaturationProbeStore()
	started := time.Now()
	if perLabel {
		for _, label := range labels {
			deadCodeCandidateLabels = []string{label}
			runDeadCodeSaturationScanner(t, scanner, store)
		}
	} else {
		deadCodeCandidateLabels = append([]string(nil), labels...)
		runDeadCodeSaturationScanner(t, scanner, store)
	}
	return deadCodeSaturationOutcome{
		duration:          time.Since(started),
		candidatePages:    store.candidatePages,
		candidateRows:     store.candidateRows,
		hydrationCalls:    store.hydrationCalls,
		hydrationIDs:      store.hydrationIDs,
		reachabilityCalls: store.reachabilityCalls,
		reachabilityIDs:   store.reachabilityIDs,
		labels:            len(store.labels),
	}
}

func runDeadCodeSaturationScanner(t *testing.T, scanner string, store *deadCodeSaturationProbeStore) {
	t.Helper()
	handler := &CodeHandler{Content: store, Neo4j: fakeGraphReader{}}
	ctx := context.Background()
	var err error
	switch scanner {
	case "dead-code":
		_, err = handler.scanDeadCodeCandidates(ctx, deadCodeRequest{RepoID: "repo-1", Limit: 100})
	case "investigate":
		_, err = handler.scanDeadCodeInvestigation(ctx, deadCodeInvestigationRequest{RepoID: "repo-1", Limit: 100})
	case "cross-repo":
		_, err = handler.scanCrossRepoDeadCodeCandidates(ctx, crossRepoDeadCodeRequest{RepoID: "repo-1", Limit: 100})
	default:
		t.Fatalf("unknown scanner %q", scanner)
	}
	if err != nil {
		t.Fatalf("%s saturation scan error = %v", scanner, err)
	}
}

func assertDeadCodeSaturationCounts(
	t *testing.T,
	got deadCodeSaturationOutcome,
	wantPages int,
	wantRows int,
	wantLabels int,
) {
	t.Helper()
	if got.candidatePages != wantPages || got.candidateRows != wantRows ||
		got.hydrationCalls != wantPages || got.hydrationIDs != wantRows ||
		got.reachabilityCalls != wantPages || got.reachabilityIDs != wantRows ||
		got.labels != wantLabels {
		t.Fatalf("saturation outcome = %+v; want pages/calls=%d rows/ids=%d labels=%d", got, wantPages, wantRows, wantLabels)
	}
}
