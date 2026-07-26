// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/environment"
	"github.com/eshu-hq/eshu/go/internal/facts"
	cicdrunv1 "github.com/eshu-hq/eshu/sdk/go/factschema/cicdrun/v1"
)

// decodedCICDDeploymentEvent pairs a ci.deployment_event envelope with its
// once-decoded typed value. A deployment event carries no run_id -- GitHub's
// Deployments API has no run identity at all -- so, unlike the run-scoped
// kinds in ci_cd_run_correlation_decode.go, this evidence cannot be bucketed
// under a run during the decode pass. It is decoded once during the build
// phase and fanned out to every run sharing its head sha by
// attachDeploymentEventsToRuns, the same repo/commit-scoped attach pattern
// attachWorkflowImagesToRuns already established for
// ci.workflow_image_evidence (#5424).
type decodedCICDDeploymentEvent struct {
	envelope facts.Envelope
	evidence cicdrunv1.DeploymentEvent
}

// attachDeploymentEventsToRuns joins each already-decoded ci.deployment_event
// (decodedCICDDeploymentEvent, decoded once during the build phase and never
// re-decoded here) to every run whose CommitSHA equals the event's SHA, both
// trimmed to preserve the same byte-parity convention every other identity
// comparison in this package follows (trimmedCICDField/trimmedCICDPtr).
//
// A deployment event has no run_id to join on -- the only link back to a
// workflow run is sha -> the run's head_sha -- so an event fans out to EVERY
// run sharing that head sha rather than picking one: multiple runs can share
// a head commit (for example a re-run, or two providers observing the same
// push), and each one independently earns the deployment evidence. A run
// whose own decoded CommitSHA is empty (CommitSHA is optional on
// cicdrunv1.Run) matches nothing, so an event that also carries an empty sha
// can never join under a shared empty-string key.
func attachDeploymentEventsToRuns(runs map[string]*cicdRunEvidence, events []*decodedCICDDeploymentEvent) int {
	if len(events) == 0 {
		return 0
	}
	skipped := 0
	for _, ev := range runs {
		runCommit := trimmedCICDPtr(ev.runDecoded.CommitSHA)
		if runCommit == "" {
			continue
		}
		runRepository := trimmedCICDPtr(ev.runDecoded.RepositoryID)
		var matched []*decodedCICDDeploymentEvent
		for _, event := range events {
			if trimmedCICDField(event.evidence.SHA) != runCommit {
				continue
			}
			// A commit sha is only unique within a repository. One ci_cd_run
			// scope is one repository today (source.go partitions by
			// target.Repository), so sha alone cannot currently cross-join --
			// but the fact already carries RepositoryID, and leaving it unread
			// means a scope that ever spans repositories would silently attach
			// one repository's deployment to another's run on a shared commit.
			// The sibling attachWorkflowImagesToRuns gates on repository first
			// for the same reason. Only skip when BOTH sides name a repository
			// and they disagree, so a producer that omits the optional field
			// keeps the sha-only behaviour rather than losing its events.
			eventRepository := trimmedCICDPtr(event.evidence.RepositoryID)
			if runRepository != "" && eventRepository != "" && runRepository != eventRepository {
				skipped++
				continue
			}
			matched = append(matched, event)
		}
		ev.deploymentEvents = matched
	}
	return skipped
}

// cicdDeploymentEventStateRank orders a deployment event's provider-reported
// State for deterministic selection: a "success" state is the strongest
// evidence of the environment actually deployed to, "in_progress" is the next
// best (a deployment underway), and every other state (including absent,
// pending, failure, error, inactive, queued) ranks lowest. The rank is
// case-insensitive because free-string provider fields are not guaranteed to
// arrive in one case.
func cicdDeploymentEventStateRank(event *decodedCICDDeploymentEvent) int {
	switch strings.ToLower(trimmedCICDPtr(event.evidence.State)) {
	case "success":
		return 2
	case "in_progress":
		return 1
	default:
		return 0
	}
}

// deploymentEventOutranks reports whether candidate must be preferred over
// current under the deterministic selection order selectDeploymentEvent
// applies: state rank first (see cicdDeploymentEventStateRank), then the
// highest deployment_id compared as strings, then the highest status_id
// compared as strings. Comparing the id fields as strings (not parsed as
// integers) keeps the tie-break total and side-effect-free for a provider id
// that is not purely numeric, at the cost of "10" sorting before "9" -- an
// accepted trade because the id fields exist here only to make selection
// deterministic, not to assert a numeric ordering of provider events.
func deploymentEventOutranks(candidate, current *decodedCICDDeploymentEvent) bool {
	if candidateRank, currentRank := cicdDeploymentEventStateRank(candidate), cicdDeploymentEventStateRank(current); candidateRank != currentRank {
		return candidateRank > currentRank
	}
	if candidateID, currentID := trimmedCICDField(candidate.evidence.DeploymentID), trimmedCICDField(current.evidence.DeploymentID); candidateID != currentID {
		return candidateID > currentID
	}
	return trimmedCICDPtr(candidate.evidence.StatusID) > trimmedCICDPtr(current.evidence.StatusID)
}

// selectDeploymentEvent deterministically picks one winning event from a
// run's attached deployment events, per deploymentEventOutranks. It never
// depends on slice order: the same set of events, fed in any order, always
// selects the same winner. Returns nil for an empty slice.
func selectDeploymentEvent(events []*decodedCICDDeploymentEvent) *decodedCICDDeploymentEvent {
	if len(events) == 0 {
		return nil
	}
	winner := events[0]
	for _, candidate := range events[1:] {
		if deploymentEventOutranks(candidate, winner) {
			winner = candidate
		}
	}
	return winner
}

// classifyCICDDeploymentEventEnvironment resolves the environment evidence a
// run's attached deployment events supply, if any: the winning event's
// Environment, canonicalized through the same environment.Canonical the
// declared (ci.environment_observation) path already uses, plus the winning
// event's own fact ID for EvidenceFactIDs attribution. ok is false when the
// run has no attached deployment events, signaling classifyCICDRunEvidence to
// fall back to the declared path.
func classifyCICDDeploymentEventEnvironment(events []*decodedCICDDeploymentEvent) (env string, factID string, ok bool) {
	winner := selectDeploymentEvent(events)
	if winner == nil {
		return "", "", false
	}
	canonical := environment.Canonical(trimmedCICDField(winner.evidence.Environment))
	if canonical == "" {
		// An event whose environment canonicalizes to nothing carries no
		// deployment truth. Reporting ok here would suppress the declared
		// fallback and publish an empty environment stamped
		// environment_evidence=deploy_event, which #5426 reads as deployment
		// truth. The typed decode only enforces presence and non-null, not
		// non-empty, and the contract module serves external collectors and
		// cassettes as well as this repo's collector, so the guard belongs
		// here rather than only at the emitter.
		return "", "", false
	}
	return canonical, winner.envelope.FactID, true
}
