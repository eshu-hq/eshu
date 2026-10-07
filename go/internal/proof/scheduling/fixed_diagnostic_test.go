// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
)

func diagnosticProbeRow(kind, term, id string) codetopicparallel.ProbeRow {
	return codetopicparallel.ProbeRow{SourceKind: kind, MatchedTerm: term, EntityID: &id}
}

func mustDiagnosticPage(t *testing.T, rows []assembledDiagnosticRow) diagnosticPage {
	t.Helper()
	page, err := summarizeDiagnosticPage(rows)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestDiagnosticPoolHashesDistinguishMembershipFromArrival(t *testing.T) {
	first := []codetopicparallel.ProbeRow{
		diagnosticProbeRow("entity", "config", "private-a"),
		diagnosticProbeRow("entity", "config", "private-b"),
	}
	reversed := []codetopicparallel.ProbeRow{first[1], first[0]}
	changed := []codetopicparallel.ProbeRow{first[0], diagnosticProbeRow("entity", "config", "private-c")}
	a, err := summarizeDiagnosticPools(first, []string{"config"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	b, err := summarizeDiagnosticPools(reversed, []string{"config"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	c, err := summarizeDiagnosticPools(changed, []string{"config"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	key := poolKey{kind: "entity", term: "config"}
	if a[key].multisetHash != b[key].multisetHash || a[key].arrivalHash == b[key].arrivalHash {
		t.Fatalf("arrival order confused with membership: a=%+v b=%+v", a[key], b[key])
	}
	if a[key].multisetHash == c[key].multisetHash || !a[key].capped {
		t.Fatalf("capped membership change hidden: a=%+v c=%+v", a[key], c[key])
	}
	uncapped, err := summarizeDiagnosticPools(first[:1], []string{"config"}, 2)
	if err != nil || uncapped[key].capped {
		t.Fatalf("uncapped pool misclassified: summary=%+v err=%v", uncapped[key], err)
	}
	if _, err := summarizeDiagnosticPools(first, []string{"config"}, 0); err == nil {
		t.Fatal("nonpositive cap accepted")
	}
	if _, err := summarizeDiagnosticPools(first, []string{"other"}, 2); err == nil {
		t.Fatal("unexpected term accepted")
	}
}

func TestDiagnosticReplayUsesUnchangedPayloadTwice(t *testing.T) {
	payload := []byte(`[{"entity_id":"private-a"}]`)
	var seen [][]byte
	assembled := func(_ context.Context, input []byte) (diagnosticPage, error) {
		seen = append(seen, bytes.Clone(input))
		return summarizeDiagnosticPage([]assembledDiagnosticRow{{Score: 1}})
	}
	pages, err := replayDiagnosticAssembly(context.Background(), payload, assembled)
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || !bytes.Equal(seen[0], payload) || !bytes.Equal(seen[1], payload) {
		t.Fatalf("assembly replay changed payload: calls=%d", len(seen))
	}
	if pages[0].fullHash != pages[1].fullHash {
		t.Fatal("same-payload replay changed page hash")
	}
	assembledError := errors.New("assembly failed")
	if _, err := replayDiagnosticAssembly(context.Background(), payload, func(context.Context, []byte) (diagnosticPage, error) {
		return diagnosticPage{}, assembledError
	}); !errors.Is(err, assembledError) {
		t.Fatalf("assembly error lost: %v", err)
	}
}

func TestDiagnosticPageSeparatesVisibleFromLookahead(t *testing.T) {
	rows := make([]assembledDiagnosticRow, 26)
	for i := range rows {
		rows[i] = assembledDiagnosticRow{Score: int32(26 - i)}
	}
	first := mustDiagnosticPage(t, rows)
	rows[25].Score = 99
	second := mustDiagnosticPage(t, rows)
	if first.visibleHash != second.visibleHash || first.lookaheadHash == second.lookaheadHash || first.fullHash == second.fullHash {
		t.Fatalf("lookahead change altered visible page or was hidden: first=%+v second=%+v", first, second)
	}
	if !first.lookaheadPresent || !second.lookaheadPresent {
		t.Fatal("26th row not marked as lookahead")
	}
	short := mustDiagnosticPage(t, rows[:25])
	if short.lookaheadPresent || short.lookaheadHash != "" {
		t.Fatalf("missing lookahead hidden: %+v", short)
	}
}

func TestDiagnosticFirstDifferenceRedactsRankTextAndMarksNull(t *testing.T) {
	repoID := "private-repository-id"
	path := "private/path.go"
	left := mustDiagnosticPage(t, []assembledDiagnosticRow{{Score: 3, RepoID: &repoID, RelativePath: &path, SourceKind: "entity"}})
	right := mustDiagnosticPage(t, []assembledDiagnosticRow{{Score: 3, RepoID: nil, RelativePath: &path, SourceKind: "entity"}})
	line := formatDiagnosticFirstDifference("baseline1", "baseline2", left, right, "C.UTF-8", "c", "")
	for _, want := range []string{"rank_index=0", "region=visible", "repo_id_null=false", "repo_id_null=true", "collation=C.UTF-8", "provider=c", "repo_id_sha256="} {
		if !strings.Contains(line, want) {
			t.Errorf("first difference missing %q: %s", want, line)
		}
	}
	for _, secret := range []string{repoID, path} {
		if strings.Contains(line, secret) {
			t.Errorf("first difference leaked %q: %s", secret, line)
		}
	}
	rows := make([]assembledDiagnosticRow, 26)
	for i := range rows {
		rows[i].Score = int32(26 - i)
	}
	lookaheadLeft := mustDiagnosticPage(t, rows)
	rows[25].Score = 0
	lookaheadRight := mustDiagnosticPage(t, rows)
	if line := formatDiagnosticFirstDifference("baseline1", "candidate1", lookaheadLeft, lookaheadRight, "C", "c", ""); !strings.Contains(line, "rank_index=25 region=lookahead") {
		t.Fatalf("lookahead difference not labeled: %s", line)
	}
}

func TestDiagnosticReportDoesNotPrintProbeOrPageIdentifiers(t *testing.T) {
	privateID := "private-entity-identifier"
	rows := []codetopicparallel.ProbeRow{diagnosticProbeRow("entity", "config", privateID)}
	pools, err := summarizeDiagnosticPools(rows, []string{"config"}, candidateCap)
	if err != nil {
		t.Fatal(err)
	}
	page := mustDiagnosticPage(t, []assembledDiagnosticRow{{SourceKind: "entity", EntityID: &privateID, Score: 1}})
	capture := diagnosticCapture{
		name: "baseline1", rowCount: 1, pools: pools,
		pages: [2]diagnosticPage{page, page},
	}
	var report bytes.Buffer
	if err := writeDiagnosticCapture(&report, capture, []string{"config"}); err != nil {
		t.Fatal(err)
	}
	if err := writeDiagnosticPair(&report, capture, capture, "C", "c", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(report.String(), privateID) {
		t.Fatalf("diagnostic report leaked an identifier: %s", report.String())
	}
	for _, want := range []string{"capped=false", "pool_multiset_equal=true", "visible_equal=true", "lookahead_equal=true"} {
		if !strings.Contains(report.String(), want) {
			t.Errorf("report missing %q", want)
		}
	}
}

func TestDiagnosticDatabaseFailureSuppressesPrivateValues(t *testing.T) {
	err := diagnosticDatabaseFailure(errors.New("invalid identifier private-entity-identifier"))
	if strings.Contains(err.Error(), "private-entity-identifier") {
		t.Fatalf("diagnostic failure leaked a private value: %v", err)
	}
}
