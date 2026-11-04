// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// This file holds the pure decisions of the suppression fan-out designed in
// docs/internal/design/10019-suppression-fanout.md. The writer does not call
// it yet: the behavior it serves needs the OS finding shape of #9914.

// suppressionDiffRow is one row of the generation-diff statement: a suppression
// whose payload differs between the current generation and the baseline. An id
// is empty when that side has no such suppression or its scope names no CVE.
type suppressionDiffRow struct {
	SuppressionID string
	CurrentCVEID  string
	PriorCVEID    string
}

// suppressionGeneration is one retained generation of a suppression scope, as
// the baseline read returns it. A zero ActivatedAt means it was never activated.
// Admitted reports that some pass committed a write admission for it, which
// makes it the point the fan-out has already covered up to.
type suppressionGeneration struct {
	GenerationID string
	Status       string
	ActivatedAt  time.Time
	IngestedAt   time.Time
	Admitted     bool
}

// suppressionDiffBaseline names the generation a pass diffs against and how it
// was chosen. Kind is one of the suppressionBaseline constants and is a bounded
// telemetry label. GenerationID is empty for suppressionBaselineNone.
type suppressionDiffBaseline struct {
	GenerationID string
	Kind         string
}

const (
	// suppressionBaselineAdmitted is the newest older generation a pass admitted.
	suppressionBaselineAdmitted = "admitted"
	// suppressionBaselineOldestRetained is the oldest retained activated
	// generation, used when retention left no admitted one.
	suppressionBaselineOldestRetained = "oldest_retained"
	// suppressionBaselineNone means no older generation exists, so every
	// suppression in the current generation counts as new.
	suppressionBaselineNone = "none"
)

// suppressionDiffAction is what a pass does about the suppression diff. Its
// values are bounded telemetry labels.
type suppressionDiffAction string

const (
	// suppressionDiffSkipKind is a pass whose own facts hold no suppression, such
	// as a KEV or EPSS scope. Those ids can never join the fan-out set this way.
	suppressionDiffSkipKind suppressionDiffAction = "skipped_kind"
	// suppressionDiffSkipDuplicate is a repeat delivery of an already admitted
	// generation with no fan-out owed. It reopens nobody.
	suppressionDiffSkipDuplicate suppressionDiffAction = "skipped_duplicate"
	// suppressionDiffSkipNeverActivated is a generation that failed or was
	// superseded while pending. No reader ever saw it, so it changed nothing.
	suppressionDiffSkipNeverActivated suppressionDiffAction = "skipped_never_activated"
	// suppressionDiffDeferUnactivated is a pass that won the claim before its
	// generation activated. Fanning out now would have holders read the old set.
	suppressionDiffDeferUnactivated suppressionDiffAction = "deferred_unactivated"
	// suppressionDiffRun runs the diff.
	suppressionDiffRun suppressionDiffAction = "ran"
)

// suppressionDiffPass is the state decideSuppressionDiff judges, read under the
// writer's advisory lock before the pass's own admission row is written.
type suppressionDiffPass struct {
	ScopeFactKinds      []string
	GenerationStatus    string
	GenerationActivated bool
	AlreadyAdmitted     bool
	Recovery            bool
}

// changedSuppressionCVEIDs returns the sorted CVE ids a set of changed
// suppressions names, on both sides of each change: a re-scope has to reach the
// holders of the old id as well as the new one. Ids are trimmed as the fact
// decoder trims them. Matching a suppression to a finding folds case, but the
// holder read compares exactly, so a lower-case id also yields its upper form.
func changedSuppressionCVEIDs(rows []suppressionDiffRow) []string {
	set := make(map[string]struct{}, len(rows))
	add := func(raw string) {
		id := strings.TrimSpace(raw)
		if id == "" {
			return
		}
		set[id] = struct{}{}
		set[strings.ToUpper(id)] = struct{}{}
	}
	for _, row := range rows {
		add(row.CurrentCVEID)
		add(row.PriorCVEID)
	}
	return sortedSetKeys(set)
}

// countSuppressionsWithoutCVEScope counts changed suppressions that name no CVE
// on either side. The fan-out cannot reach their holders by CVE id, so this count
// is the operator's signal for what waits on each holder's own next pass.
func countSuppressionsWithoutCVEScope(rows []suppressionDiffRow) int {
	count := 0
	for _, row := range rows {
		if strings.TrimSpace(row.CurrentCVEID) == "" && strings.TrimSpace(row.PriorCVEID) == "" {
			count++
		}
	}
	return count
}

// unionSuppressionFanOutCVEIDs is the pass's fan-out set: the CVEs whose finding
// rows it changed plus the CVEs its changed suppressions name.
func unionSuppressionFanOutCVEIDs(changed []string, rows []suppressionDiffRow) []string {
	return unionSortedStrings(changed, changedSuppressionCVEIDs(rows))
}

// selectSuppressionDiffBaseline picks the generation the current one diffs
// against. The plain previous generation is wrong here: a generation activated
// and then superseded before its reducer item ran never fans out, so its change
// would fall between two diffs. The newest older generation some pass admitted
// marks what has been covered, so the diff spans every generation since. Failed
// and never-activated generations never count, since no reader saw them. With no
// admitted generation retained, the oldest retained one covers all that can
// still be compared.
func selectSuppressionDiffBaseline(currentID string, candidates []suppressionGeneration) suppressionDiffBaseline {
	current := slices.IndexFunc(candidates, func(g suppressionGeneration) bool {
		return g.GenerationID == currentID
	})
	if current < 0 || candidates[current].ActivatedAt.IsZero() {
		return suppressionDiffBaseline{Kind: suppressionBaselineNone}
	}
	currentActivatedAt := candidates[current].ActivatedAt

	var admitted, oldest *suppressionGeneration
	for i := range candidates {
		candidate := &candidates[i]
		if candidate.ActivatedAt.IsZero() || !candidate.ActivatedAt.Before(currentActivatedAt) {
			continue
		}
		if candidate.Status != "active" && candidate.Status != "superseded" {
			continue
		}
		if oldest == nil || suppressionGenerationNewer(*oldest, *candidate) {
			oldest = candidate
		}
		if candidate.Admitted && (admitted == nil || suppressionGenerationNewer(*candidate, *admitted)) {
			admitted = candidate
		}
	}
	switch {
	case admitted != nil:
		return suppressionDiffBaseline{GenerationID: admitted.GenerationID, Kind: suppressionBaselineAdmitted}
	case oldest != nil:
		return suppressionDiffBaseline{GenerationID: oldest.GenerationID, Kind: suppressionBaselineOldestRetained}
	default:
		return suppressionDiffBaseline{Kind: suppressionBaselineNone}
	}
}

// suppressionGenerationNewer orders generations by activation, then ingestion,
// then id, so the choice is deterministic across retries.
func suppressionGenerationNewer(a, b suppressionGeneration) bool {
	if !a.ActivatedAt.Equal(b.ActivatedAt) {
		return a.ActivatedAt.After(b.ActivatedAt)
	}
	if !a.IngestedAt.Equal(b.IngestedAt) {
		return a.IngestedAt.After(b.IngestedAt)
	}
	return a.GenerationID > b.GenerationID
}

// decideSuppressionDiff decides whether a pass runs the suppression diff. A first
// pass runs it even for a generation already superseded, because skipping would
// admit that generation and move the baseline past a change nobody fanned out. A
// retry owing fan-out also runs it: finding rows cannot rebuild the set when the
// suppression changed no row, and the diff over immutable generations repeats.
func decideSuppressionDiff(pass suppressionDiffPass) suppressionDiffAction {
	if !slices.Contains(pass.ScopeFactKinds, chain.VulnerabilitySuppressionFactKind) {
		return suppressionDiffSkipKind
	}
	if !pass.GenerationActivated {
		if pass.GenerationStatus == "pending" {
			return suppressionDiffDeferUnactivated
		}
		return suppressionDiffSkipNeverActivated
	}
	if pass.AlreadyAdmitted && !pass.Recovery {
		return suppressionDiffSkipDuplicate
	}
	return suppressionDiffRun
}
