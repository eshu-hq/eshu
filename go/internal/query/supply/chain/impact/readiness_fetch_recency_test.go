// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// A vulnerability collector that fetches an unchanged answer stores no new
// generation (#9694), so the active facts keep their old observed_at. These
// tests pin that readiness takes freshness from the source state's last
// successful fetch for such sources, and still reports missing evidence when
// the source really has not been fetched.

func recencyState(source, scope, freshness, terminal string, lastSuccess time.Time) map[string]any {
	return map[string]any{
		"collector_instance_id": "collector-vulnerability-intelligence",
		"scope_id":              scope,
		"source":                source,
		"freshness_state":       freshness,
		"terminal_status":       terminal,
		"last_success_at":       lastSuccess.UTC().Format(time.RFC3339),
		"updated_at":            lastSuccess.UTC().Format(time.RFC3339),
		"result_count":          4,
	}
}

func recencySnapshot(source, freshness string, lastUpdated time.Time) map[string]any {
	return map[string]any{
		"source": source, "freshness": freshness, "complete": true,
		"last_updated_at": lastUpdated.UTC().Format(time.RFC3339),
	}
}

func readRecencySnapshot(t *testing.T, advisoryObserved time.Time, states, snapshots []map[string]any) ReadinessSnapshot {
	t.Helper()
	return readRecencyRows(t, advisoryObserved, nil, states, snapshots)
}

// readRecencyRows is readRecencySnapshot with an optional exploitability family
// observed at exploitObserved.
func readRecencyRows(t *testing.T, advisoryObserved time.Time, exploitObserved *time.Time, states, snapshots []map[string]any) ReadinessSnapshot {
	t.Helper()
	statesJSON, err := json.Marshal(states)
	if err != nil {
		t.Fatalf("marshal states: %v", err)
	}
	rows := [][]any{
		cov8ReadinessRow(EvidenceFamilyVulnerabilityAdvisory, 4, advisoryObserved, nil, nil, nil, nil, nil),
		cov8ReadinessRow(sourceStateFamilyMarker, 0, nil, false, nil, nil, string(statesJSON), nil),
	}
	if exploitObserved != nil {
		rows = append(rows, cov8ReadinessRow(EvidenceFamilyVulnerabilityExploitability, 2, *exploitObserved, nil, nil, nil, nil, nil))
	}
	if len(snapshots) > 0 {
		snapshotsJSON, err := json.Marshal(snapshots)
		if err != nil {
			t.Fatalf("marshal snapshots: %v", err)
		}
		rows = append(rows, cov8ReadinessRow(sourceSnapshotFamilyMarker, 0, nil, false, nil, string(snapshotsJSON), nil, nil))
	}
	tx := &cov8ImpactTx{steps: []cov8ImpactStep{{rows: rows}}}
	store := NewPostgresReadinessStoreWithReadStore(&cov8ImpactReadStore{tx: tx})
	snapshot, err := store.ReadSupplyChainImpactReadiness(context.Background(), ReadinessQuery{RepositoryID: "repo-1"})
	if err != nil {
		t.Fatalf("ReadSupplyChainImpactReadiness() error = %v", err)
	}
	return snapshot
}

func advisoryFamily(t *testing.T, snapshot ReadinessSnapshot) EvidenceFamily {
	t.Helper()
	for _, family := range snapshot.EvidenceSources {
		if family.Family == EvidenceFamilyVulnerabilityAdvisory {
			return family
		}
	}
	t.Fatalf("no advisory family in %+v", snapshot.EvidenceSources)
	return EvidenceFamily{}
}

func TestReadinessUnchangedAnswerStaysFreshFromTheLastSuccessfulFetch(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	old := now.Add(-30 * 24 * time.Hour) // facts last written 30 days ago: the answer has not changed since
	snapshot := readRecencySnapshot(t, old,
		[]map[string]any{recencyState("osv", "vuln-intel://osv/npm/example", "fresh", "succeeded", now.Add(-time.Hour))}, nil)

	family := advisoryFamily(t, snapshot)
	if family.Freshness != FreshnessLabelFresh {
		t.Fatalf("advisory freshness = %q with a successful fetch an hour ago, want %q", family.Freshness, FreshnessLabelFresh)
	}
	if got, want := family.LatestObservedAt, old.Format(time.RFC3339); got != want {
		t.Fatalf("latest_observed_at = %q, want the unchanged fact time %q: facts are not re-observed", got, want)
	}
	envelope := BuildReadiness(TargetScope{CVEID: "CVE-2026-0001"}, nil, false, snapshot)
	if ReadinessMissingContains(envelope.MissingEvidence, MissingEvidenceAdvisorySources) {
		t.Fatalf("missing_evidence = %v, want no %s for a source fetched an hour ago", envelope.MissingEvidence, MissingEvidenceAdvisorySources)
	}
}

func TestReadinessStillReportsAdvisorySourcesWhenTheSourceWasNotFetched(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	old := now.Add(-30 * 24 * time.Hour)
	cases := []struct {
		name   string
		states []map[string]any
	}{
		{"last success outside the window", []map[string]any{recencyState("osv", "s1", "fresh", "succeeded", now.Add(-20*24*time.Hour))}},
		{"state is partial", []map[string]any{recencyState("osv", "s1", "partial", "partial", now.Add(-time.Hour))}},
		{"state failed", []map[string]any{recencyState("osv", "s1", "failed", "failed", now.Add(-time.Hour))}},
		{"one scope fresh and one stale", []map[string]any{
			recencyState("osv", "s1", "fresh", "succeeded", now.Add(-time.Hour)),
			recencyState("osv", "s2", "fresh", "succeeded", now.Add(-20*24*time.Hour)),
		}},
		{"no source state at all", nil},
		{"a fresh state for an unrelated source", []map[string]any{recencyState("cisa_kev", "s1", "fresh", "succeeded", now.Add(-time.Hour))}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			snapshot := readRecencySnapshot(t, old, tc.states, nil)
			if got := advisoryFamily(t, snapshot).Freshness; got != FreshnessLabelStale {
				t.Fatalf("advisory freshness = %q, want %q: the source was not fetched", got, FreshnessLabelStale)
			}
			envelope := BuildReadiness(TargetScope{CVEID: "CVE-2026-0001"}, nil, false, snapshot)
			if !ReadinessMissingContains(envelope.MissingEvidence, MissingEvidenceAdvisorySources) {
				t.Fatalf("missing_evidence = %v, want %s", envelope.MissingEvidence, MissingEvidenceAdvisorySources)
			}
		})
	}
}

func TestReadinessNVDSnapshotStaysFreshFromTheLastSuccessfulFetch(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	old := now.Add(-30 * 24 * time.Hour)
	fresh := recencyState("nvd", "vuln-intel://nvd/cve", "fresh", "succeeded", now.Add(-time.Hour))
	snapshot := readRecencySnapshot(t, now.Add(-time.Hour),
		[]map[string]any{fresh}, []map[string]any{recencySnapshot("nvd", "fresh", old)})
	if len(snapshot.SourceSnapshots) != 1 || snapshot.SourceSnapshots[0].Freshness != FreshnessLabelFresh {
		t.Fatalf("snapshots = %+v, want the NVD snapshot fresh from the last successful fetch", snapshot.SourceSnapshots)
	}
	envelope := BuildReadiness(TargetScope{CVEID: "CVE-2026-0001"}, nil, false, snapshot)
	if ReadinessMissingContains(envelope.MissingEvidence, MissingEvidenceTargetCollection) {
		t.Fatalf("missing_evidence = %v, want no %s", envelope.MissingEvidence, MissingEvidenceTargetCollection)
	}

	stale := recencyState("nvd", "vuln-intel://nvd/cve", "fresh", "succeeded", now.Add(-20*24*time.Hour))
	snapshot = readRecencySnapshot(t, now.Add(-time.Hour),
		[]map[string]any{stale}, []map[string]any{recencySnapshot("nvd", "fresh", old)})
	if len(snapshot.SourceSnapshots) != 1 || snapshot.SourceSnapshots[0].Freshness != FreshnessLabelStale {
		t.Fatalf("snapshots = %+v, want the NVD snapshot stale when the source was not fetched", snapshot.SourceSnapshots)
	}
}

func familyFreshness(snapshot ReadinessSnapshot, name string) string {
	for _, family := range snapshot.EvidenceSources {
		if family.Family == name {
			return family.Freshness
		}
	}
	return "absent"
}

// TestReadinessFetchConfirmationTable pins each rule of the freshener on both
// sides, so a rule cannot be dropped without a case failing: terminal status,
// the exploitability sources, every source of the advisory family (OSV, NVD and
// GLAD), and the per-source snapshot match.
func TestReadinessFetchConfirmationTable(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	old := now.Add(-30 * 24 * time.Hour)
	recent := now.Add(-time.Hour)
	cases := []struct {
		name         string
		states       []map[string]any
		wantAdvisory string
		wantExploit  string
	}{
		{
			"exploitability fresh from a KEV success",
			[]map[string]any{recencyState("cisa_kev", "k", "fresh", "succeeded", recent)},
			FreshnessLabelStale, FreshnessLabelFresh,
		},
		{
			"exploitability fresh from an EPSS success",
			[]map[string]any{recencyState("first_epss", "e", "fresh", "succeeded", recent)},
			FreshnessLabelStale, FreshnessLabelFresh,
		},
		{
			"exploitability stays stale when EPSS failed beside a fresh KEV",
			[]map[string]any{
				recencyState("cisa_kev", "k", "fresh", "succeeded", recent),
				recencyState("first_epss", "e", "failed", "failed", recent),
			},
			FreshnessLabelStale, FreshnessLabelStale,
		},
		{
			"exploitability stays stale with no exploitability state",
			[]map[string]any{recencyState("osv", "o", "fresh", "succeeded", recent)},
			FreshnessLabelFresh, FreshnessLabelStale,
		},
		{
			"advisory stays stale when NVD failed beside a fresh OSV",
			[]map[string]any{
				recencyState("osv", "o", "fresh", "succeeded", recent),
				recencyState("nvd", "n", "failed", "failed", recent),
			},
			FreshnessLabelStale, FreshnessLabelStale,
		},
		{
			"advisory stays stale when GLAD failed beside a fresh OSV",
			[]map[string]any{
				recencyState("osv", "o", "fresh", "succeeded", recent),
				recencyState("glad", "g", "failed", "failed", recent),
			},
			FreshnessLabelStale, FreshnessLabelStale,
		},
		{
			"advisory fresh from NVD alone",
			[]map[string]any{recencyState("nvd", "n", "fresh", "succeeded", recent)},
			FreshnessLabelFresh, FreshnessLabelStale,
		},
		{
			"advisory fresh from GLAD alone",
			[]map[string]any{recencyState("glad", "g", "fresh", "succeeded", recent)},
			FreshnessLabelFresh, FreshnessLabelStale,
		},
		{
			"a fresh state that did not succeed does not confirm",
			[]map[string]any{recencyState("osv", "o", "fresh", "skipped", recent)},
			FreshnessLabelStale, FreshnessLabelStale,
		},
		{
			"a fresh state that is partial does not confirm",
			[]map[string]any{recencyState("osv", "o", "fresh", "partial", recent)},
			FreshnessLabelStale, FreshnessLabelStale,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			snapshot := readRecencyRows(t, old, &old, tc.states, nil)
			if got := familyFreshness(snapshot, EvidenceFamilyVulnerabilityAdvisory); got != tc.wantAdvisory {
				t.Fatalf("advisory freshness = %q, want %q", got, tc.wantAdvisory)
			}
			if got := familyFreshness(snapshot, EvidenceFamilyVulnerabilityExploitability); got != tc.wantExploit {
				t.Fatalf("exploitability freshness = %q, want %q", got, tc.wantExploit)
			}
		})
	}
}

// TestReadinessSnapshotsFreshenPerSource covers every source's snapshot: with a
// configured source cache an unchanged answer is skipped, so the snapshot's
// cache_updated_at stops moving (#9694 review G2). A snapshot is freshened only
// by its own source's states, and its last_updated_at moves to that source's
// newest success.
func TestReadinessSnapshotsFreshenPerSource(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-30 * 24 * time.Hour)
	newest := now.Add(-time.Hour)
	older := now.Add(-3 * time.Hour)
	for _, source := range []string{"nvd", "osv", "cisa_kev", "first_epss"} {
		t.Run(source+" confirmed by its own fresh success", func(t *testing.T) {
			t.Parallel()
			snapshot := readRecencySnapshot(t, now.Add(-time.Hour),
				[]map[string]any{
					recencyState(source, "scope-a", "fresh", "succeeded", older),
					recencyState(source, "scope-b", "fresh", "succeeded", newest),
				},
				[]map[string]any{recencySnapshot(source, "fresh", old)})
			if len(snapshot.SourceSnapshots) != 1 {
				t.Fatalf("snapshots = %+v, want one", snapshot.SourceSnapshots)
			}
			got := snapshot.SourceSnapshots[0]
			if got.Freshness != FreshnessLabelFresh {
				t.Fatalf("%s snapshot freshness = %q, want fresh", source, got.Freshness)
			}
			if got.LastUpdatedAt != newest.Format(time.RFC3339) {
				t.Fatalf("%s last_updated_at = %q, want the newest success %q", source, got.LastUpdatedAt, newest.Format(time.RFC3339))
			}
		})
		t.Run(source+" stays stale when its own state is not a fresh success", func(t *testing.T) {
			t.Parallel()
			for name, state := range map[string]map[string]any{
				"failed":  recencyState(source, "scope-a", "failed", "failed", newest),
				"partial": recencyState(source, "scope-a", "partial", "partial", newest),
				"aged":    recencyState(source, "scope-a", "fresh", "succeeded", now.Add(-20*24*time.Hour)),
			} {
				snapshot := readRecencySnapshot(t, now.Add(-time.Hour), []map[string]any{state},
					[]map[string]any{recencySnapshot(source, "fresh", old)})
				if got := snapshot.SourceSnapshots[0].Freshness; got != FreshnessLabelStale {
					t.Fatalf("%s snapshot freshness = %q with a %s state, want stale", source, got, name)
				}
			}
		})
	}
	t.Run("another source's fresh state does not freshen a snapshot", func(t *testing.T) {
		t.Parallel()
		snapshot := readRecencySnapshot(t, now.Add(-time.Hour),
			[]map[string]any{recencyState("nvd", "n", "fresh", "succeeded", newest)},
			[]map[string]any{recencySnapshot("osv", "fresh", old)})
		if got := snapshot.SourceSnapshots[0].Freshness; got != FreshnessLabelStale {
			t.Fatalf("osv snapshot freshness = %q with only an nvd state, want stale", got)
		}
	})
}
