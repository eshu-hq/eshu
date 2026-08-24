// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The published-status contract, asserted by observation (#6218 review round
// 4). requiredworkflow_publishrun.go runs the publisher; this file says what
// each run must have posted.
//
// Everything here used to be read out of the script's text: one validator per
// case arm, one for the `-f state=` argument, one for the lines between `esac`
// and the publish. Each of them was a claim about a spelling, each was
// defeated by a spelling nobody had listed, and the last two were defeated by
// an ordinary prose comment. These assertions are claims about the VALUE the
// publisher posted for a given exit code, which is the property the status
// actually has to hold.

// publisherUnclassifiedProbeCode is an exit code no await outcome produces.
// The publisher's `*)` arm is the one an operator meets when something the
// classifier never anticipated reaches it, so it is exercised with a code
// nothing anticipated. It must stay outside the awaitExit* set;
// TestPublisherProbeCodeIsNotARealAwaitExitCode holds that.
const publisherUnclassifiedProbeCode = 97

// publisherOutcome is one row of the contract: for this await exit code, the
// publisher must post exactly this.
type publisherOutcome struct {
	// code is the AGGREGATE_CODE the publisher is run with.
	code int
	// publishes is whether a status must be posted at all.
	publishes bool
	// state is the required `-f state=` value when publishes is true.
	state string
	// meaning is what the exit code means, for error messages an operator
	// reads at 3 AM without this file open.
	meaning string
}

// publisherOutcomeContract is the whole per-arm contract, in one place.
//
// The `failure` row is the load-bearing one: `failure` is reserved for a gate
// that genuinely concluded failure (#6075), and `required-gates-complete` is
// one of the three checks this repository's ruleset requires on `main`. A
// publisher that posts `success` for the failed-gate code is a merge on a red
// gate; one that posts `failure` for a cancelled gate is the #6189 overclaim
// that teaches people to read a red required status as noise.
func publisherOutcomeContract() []publisherOutcome {
	return []publisherOutcome{
		{
			code: awaitExitPassedCode, publishes: true, state: "success",
			meaning: "every selected blocking gate passed",
		},
		{
			code: awaitExitGateFailedCode, publishes: true, state: "failure",
			meaning: "a selected blocking gate concluded failure",
		},
		{
			code: awaitExitStillRunningCode, publishes: false,
			meaning: "selected gates have not finished",
		},
		{
			code: awaitExitBrokenCode, publishes: true, state: "error",
			meaning: "aggregation could not reach a verdict",
		},
		{
			code: awaitExitGateCancelledCode, publishes: true, state: "error",
			meaning: "a selected gate was cancelled or never ran",
		},
		{
			code: publisherUnclassifiedProbeCode, publishes: true, state: "error",
			meaning: "an exit code no arm names",
		},
	}
}

// validatePublishedOutcomes runs the terminal publisher once per contract row
// and reports what it actually posted against what it owed.
//
// This is the whole consuming end of the AGGREGATE_CODE contract. It replaced
// four textual validators -- the still-running arm, the cancelled arm, the
// `-f state=`/`-f description=` argument bindings, and the lines between the
// case block and the publish -- because all four were the same bet: that a
// spelling this package modelled was the only spelling that could change the
// published value. Running the publisher does not take that bet.
func validatePublishedOutcomes(step requiredWorkflowStep, check RequiredStatusCheck) []error {
	harness, err := newPublisherHarness(step.Run)
	if err != nil {
		return []error{fmt.Errorf(
			"required status context %q: could not observe what the terminal publisher posts: %w",
			check.Context, err)}
	}
	defer harness.close()

	var errs []error
	descriptions := make(map[int]string, len(publisherOutcomeContract()))
	for _, want := range publisherOutcomeContract() {
		observed, err := harness.evaluate("success", strconv.Itoa(want.code))
		if err != nil {
			// A harness that cannot run proves nothing, and a validator that
			// shrugs at that is how a gate goes dark. Report and stop: every
			// remaining row would fail the same way.
			return append(errs, fmt.Errorf(
				"required status context %q: could not observe what the terminal publisher posts: %w",
				check.Context, err))
		}
		errs = append(errs, publishedOutcomeErrors(check, want, observed)...)
		if want.publishes && len(observed.Publishes) == 1 {
			descriptions[want.code] = observed.Publishes[0].Description
		}
	}
	return append(errs, publishedDescriptionErrors(check, descriptions)...)
}

// publishedOutcomeErrors compares one observed run against its contract row.
func publishedOutcomeErrors(check RequiredStatusCheck, want publisherOutcome, observed PublisherRun) []error {
	if !want.publishes {
		if len(observed.Publishes) == 0 {
			return nil
		}
		return []error{fmt.Errorf(
			"required status context %q: terminal publisher posts state=%q for AGGREGATE_CODE=%d (%s); "+
				"it must post nothing at all -- another aggregate run for this head is already queued "+
				"behind the workflows still going, and it is the authoritative one",
			check.Context, observed.Publishes[0].State, want.code, want.meaning)}
	}
	switch len(observed.Publishes) {
	case 1:
	case 0:
		return []error{fmt.Errorf(
			"required status context %q: terminal publisher posts no status at all for AGGREGATE_CODE=%d "+
				"(%s); the required status would stay on the `pending` the first step wrote, which blocks "+
				"the merge with nothing an operator can act on. Shell output: %s",
			check.Context, want.code, want.meaning, strings.TrimSpace(observed.Output))}
	default:
		return []error{fmt.Errorf(
			"required status context %q: terminal publisher posts %d statuses for AGGREGATE_CODE=%d (%s); "+
				"exactly one must be posted, or which verdict lands on the head SHA depends on which call "+
				"ran last",
			check.Context, len(observed.Publishes), want.code, want.meaning)}
	}
	published := observed.Publishes[0]
	var errs []error
	if published.State != want.state {
		errs = append(errs, fmt.Errorf(
			"required status context %q: terminal publisher posts state=%q for AGGREGATE_CODE=%d (%s); "+
				"want state=%q. This is what the publisher actually handed the status API with that exit "+
				"code, whatever the case arms read like",
			check.Context, published.State, want.code, want.meaning, want.state))
	}
	if published.Context != check.Context {
		errs = append(errs, fmt.Errorf(
			"required status context %q: terminal publisher posts context=%q for AGGREGATE_CODE=%d; "+
				"a verdict written to any other context leaves the required one untouched",
			check.Context, published.Context, want.code))
	}
	if !strings.Contains(published.URL, "/statuses/"+publisherProbeHeadSHA) {
		errs = append(errs, fmt.Errorf(
			"required status context %q: terminal publisher posts to %q for AGGREGATE_CODE=%d; it must "+
				"target the head SHA it was given (%s), or the verdict lands on a commit nobody is merging",
			check.Context, published.URL, want.code, publisherProbeHeadSHA))
	}
	return errs
}

// publishedDescriptionErrors holds the operator-facing half: every outcome
// must carry a description, and whatever the publisher says about one outcome
// it must not say about another.
//
// Distinctness, deliberately, rather than any required phrase. An earlier
// draft also required the cancelled outcome's description to contain
// "cancel". That is a wording pin: rewording a description is ordinary
// maintenance, and the draft turned it into a red -- caught by a positive
// control that reworded the failed-gate sentence. Distinctness catches the
// same defects structurally. Deleting the cancelled arm makes it fall through
// to `*)` and describe itself exactly as the unclassified outcome does;
// describing a cancellation as "A required gate failed" makes it read exactly
// as the failed-gate outcome does. Both are reported without this file having
// an opinion on English.
//
// Required, not optional. Before this file the description only had to be
// BOUND to a variable rather than PRESENT, on the reasoning that dropping it
// cost an operator a sentence and could not make a wrong status look right.
// Observation makes that reasoning false: a cancelled gate (#6189) and a
// broken aggregation both publish state=error, so with the descriptions gone
// nothing distinguishes them, and deleting the cancelled arm entirely becomes
// invisible -- the fall-through to `*)` publishes the same state. The
// description is load-bearing now, so it is required.
//
// A hard-coded `-f description=` is the #6189 overclaim reached from one
// argument over -- "A required gate failed" posted for a head where nothing
// failed. Observation catches it without naming a spelling, because a literal
// produces the SAME sentence for every arm. Distinctness is checked rather
// than any particular wording, so the workflow's prose can be reworded freely.
func publishedDescriptionErrors(check RequiredStatusCheck, descriptions map[int]string) []error {
	// The `*)` arm serves both the broken code and anything unclassified, so
	// those two legitimately share a sentence. Compare the arms that must
	// differ.
	distinct := []int{awaitExitPassedCode, awaitExitGateFailedCode, awaitExitGateCancelledCode, publisherUnclassifiedProbeCode}
	var errs []error
	for _, code := range distinct {
		if descriptions[code] != "" {
			continue
		}
		errs = append(errs, fmt.Errorf(
			"required status context %q: terminal publisher posts no description for await exit code %d; "+
				"a cancelled gate and a broken aggregation both publish state=error, so the description is "+
				"the only thing that tells an operator which of the two happened",
			check.Context, code))
	}
	if len(errs) > 0 {
		// Distinctness over mostly-empty strings would report the same cause
		// a second time in a less useful shape.
		return errs
	}
	seen := make(map[string][]int, len(distinct))
	for _, code := range distinct {
		seen[descriptions[code]] = append(seen[descriptions[code]], code)
	}
	for _, text := range sortedDescriptionKeys(seen) {
		codes := seen[text]
		if len(codes) < 2 {
			continue
		}
		errs = append(errs, fmt.Errorf(
			"required status context %q: terminal publisher describes await exit codes %v identically as "+
				"%q; outcomes that mean different things must not read the same, and a description that "+
				"never varies is a literal the AGGREGATE_CODE branch cannot reach",
			check.Context, codes, text))
	}
	return errs
}

// sortedDescriptionKeys keeps the reported order stable; Go randomises map
// iteration, so an unsorted walk would reorder a multi-error report run to
// run and make two identical failures look different.
func sortedDescriptionKeys(seen map[string][]int) []string {
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
