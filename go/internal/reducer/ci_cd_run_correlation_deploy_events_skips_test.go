// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

// Tests for the repository-mismatch skip counter: that the loss is reported at
// all, and that it reports deployment events actually LOST rather than rejected
// (run, event) pairs, which would multiply by the sha fan-out the attach
// depends on.

// The repository guard's skip must be reported, not silent. It is the only
// signal for that condition: the drop is total for the affected run, the
// collector's deployment_unanchored warning keys on sha rather than repository
// so it cannot fire, and validateTarget can only reject a path disagreement at
// startup because the run's repository html_url is unknown until collection.
func TestAttachDeploymentEventsToRunsReportsCrossRepositorySkips(t *testing.T) {
	t.Parallel()

	const sharedSHA = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c"

	run := cicdRunEvidenceWithCommit(sharedSHA)
	run.runDecoded.RepositoryID = strPtr("repository:r_ours")

	ours := deploymentEventEvidence("ours", sharedSHA, "production")
	ours.evidence.RepositoryID = strPtr("repository:r_ours")
	theirs := deploymentEventEvidence("theirs", sharedSHA, "staging")
	theirs.evidence.RepositoryID = strPtr("repository:r_theirs")
	alsoTheirs := deploymentEventEvidence("also-theirs", sharedSHA, "staging")
	alsoTheirs.evidence.RepositoryID = strPtr("repository:r_theirs")

	skipped := attachDeploymentEventsToRuns(
		map[string]*cicdRunEvidence{"run": run},
		[]*decodedCICDDeploymentEvent{ours, theirs, alsoTheirs},
	)

	if skipped != 2 {
		t.Fatalf("skipped = %d, want 2: every cross-repository drop must be counted so the "+
			"loss is visible to an operator rather than silent", skipped)
	}
}

// The counter must report events actually LOST, not rejected (run, event) pairs.
// An event fans out to every run sharing its sha by design -- a re-run, or two
// providers observing one push -- so counting pairs multiplies by the fan-out
// and overstates the loss. Two runs and two foreign events is still two lost
// events, not four.
func TestAttachDeploymentEventsToRunsCountsLostEventsNotRejectedPairs(t *testing.T) {
	t.Parallel()

	const sharedSHA = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c"

	runs := map[string]*cicdRunEvidence{}
	for _, key := range []string{"run-a", "run-b", "run-c"} {
		run := cicdRunEvidenceWithCommit(sharedSHA)
		run.runDecoded.RepositoryID = strPtr("repository:r_ours")
		runs[key] = run
	}

	foreign := make([]*decodedCICDDeploymentEvent, 0, 2)
	for _, id := range []string{"theirs-1", "theirs-2"} {
		event := deploymentEventEvidence(id, sharedSHA, "staging")
		event.evidence.RepositoryID = strPtr("repository:r_theirs")
		foreign = append(foreign, event)
	}

	if skipped := attachDeploymentEventsToRuns(runs, foreign); skipped != 2 {
		t.Fatalf("skipped = %d across 3 runs and 2 foreign events, want 2: the counter must "+
			"report events lost, not rejected (run, event) pairs, which would report 6", skipped)
	}
}

// An event rejected by one run but attached to another was never lost, so it
// must not be counted at all.
func TestAttachDeploymentEventsToRunsDoesNotCountAnEventThatFoundAHome(t *testing.T) {
	t.Parallel()

	const sharedSHA = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c"

	ours := cicdRunEvidenceWithCommit(sharedSHA)
	ours.runDecoded.RepositoryID = strPtr("repository:r_ours")
	theirs := cicdRunEvidenceWithCommit(sharedSHA)
	theirs.runDecoded.RepositoryID = strPtr("repository:r_theirs")

	event := deploymentEventEvidence("theirs-1", sharedSHA, "staging")
	event.evidence.RepositoryID = strPtr("repository:r_theirs")

	skipped := attachDeploymentEventsToRuns(
		map[string]*cicdRunEvidence{"ours": ours, "theirs": theirs},
		[]*decodedCICDDeploymentEvent{event},
	)
	if skipped != 0 {
		t.Fatalf("skipped = %d, want 0: the event was rejected by one run but attached to "+
			"another, so nothing was lost", skipped)
	}
	if len(theirs.deploymentEvents) != 1 {
		t.Fatalf("the matching run received %d events, want 1", len(theirs.deploymentEvents))
	}
}

// A run with no cross-repository events reports zero, so the counter cannot
// inflate on the ordinary path.
func TestAttachDeploymentEventsToRunsReportsNoSkipsOnTheOrdinaryPath(t *testing.T) {
	t.Parallel()

	const sharedSHA = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c"

	run := cicdRunEvidenceWithCommit(sharedSHA)
	run.runDecoded.RepositoryID = strPtr("repository:r_ours")
	ours := deploymentEventEvidence("ours", sharedSHA, "production")
	ours.evidence.RepositoryID = strPtr("repository:r_ours")

	if skipped := attachDeploymentEventsToRuns(
		map[string]*cicdRunEvidence{"run": run},
		[]*decodedCICDDeploymentEvent{ours},
	); skipped != 0 {
		t.Fatalf("skipped = %d, want 0 on a same-repository attach", skipped)
	}
}
