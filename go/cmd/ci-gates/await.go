// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cigates"
	"gopkg.in/yaml.v3"
)

func runAwait(args []string) error {
	fs := flag.NewFlagSet("await", flag.ContinueOnError)
	registry := fs.String("registry", "", "path to ci-gates.v1.yaml registry")
	repoRoot := fs.String("repo-root", "", "repository root containing workflow files")
	repo := fs.String("repo", "", "GitHub repository in owner/name form")
	pr := fs.Int("pr", 0, "pull request number (pull_request event only)")
	headSHA := fs.String("head-sha", "", "exact pull request head or merge-group commit SHA for this workflow run")
	event := fs.String("event", eventPullRequest, "triggering event of the checks to aggregate: pull_request or merge_group")
	baseRef := fs.String("base-ref", "main", "merge_group only: base branch the merge queue targets; it must match the queue branch")
	mergeGroupBranch := fs.String("merge-group-branch", "", "merge_group only: merge queue branch of the source run (gh-readonly-queue/<base>/pr-<n>-<base sha>); its base SHA is the fixed ref the changed paths are diffed against (three-dot)")
	targetRunID := fs.Int64("target-run-id", 0, "exact triggering Build Test run ID for pull_request transfer proof")
	targetAttempt := fs.Int("target-run-attempt", 0, "exact triggering Build Test run attempt for pull_request transfer proof")
	proofFile := fs.String("proof-file", "", "write a validated v3 terminal status description when transfer or full coverage is proved")
	pollInterval := fs.Duration("poll-interval", 30*time.Second, "interval between GitHub check-rollup reads")
	timeout := fs.Duration("timeout", 55*time.Minute, "maximum time to wait for selected blocking checks")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *registry == "" || *repo == "" || strings.TrimSpace(*headSHA) == "" {
		return fmt.Errorf("--registry, --repo, and --head-sha are required")
	}
	if err := validateAwaitTarget(*event, *pr, *baseRef, *mergeGroupBranch); err != nil {
		return err
	}
	root, err := resolveRepoRoot(*repoRoot)
	if err != nil {
		return fmt.Errorf("resolve repo root: %w", err)
	}
	reg, err := cigates.Load(*registry)
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	if validationErrs := reg.ValidateRequiredStatusChecks(); len(validationErrs) > 0 {
		return fmt.Errorf("invalid required status contract: %v", validationErrs)
	}

	runner := execGHRunner{}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if *event == eventMergeGroup {
		return runAwaitMergeGroup(ctx, runner, reg, root, *repo, *baseRef, *mergeGroupBranch, *headSHA, *pollInterval)
	}
	if err := verifyPRHead(ctx, runner, *repo, *pr, *headSHA); err != nil {
		return err
	}
	ctx = bindAwaitTarget(ctx, runner, *repo, *pr, *headSHA, *targetRunID, *targetAttempt)
	paths, truncated, err := changedPathsForPR(ctx, runner, *repo, *pr)
	if err != nil {
		return err
	}
	// Reuse requires a bound caller input and a current full-backed verdict.
	// Probe failure falls through to the ordinary await.
	if transferPRPathsSafe(paths, truncated) {
		if terminal, err := tryProvenReuse(ctx, runner, reg, root, *repo, *pr, *headSHA, *pollInterval, *proofFile); terminal {
			return err
		}
	}
	resolved, err := selectRequiredGates(reg, root, paths, truncated)
	if err != nil {
		return err
	}
	// Clean-rebase smoke tier (#8617 step 5): when the replaced head was
	// fully green and everything that changed since is disjoint from
	// this PR's files, only the fast subset re-runs; heavy verdicts
	// transfer. A truncated path list already over-selected above, so
	// smoke never applies to it. Like reuse, a probe failure means the
	// full await, never a verdict.
	smoke := false
	var smokeProof smokeEvidence
	if transferPRPathsSafe(paths, truncated) && len(resolved) > 0 {
		evidence, smokeErr := probeSmokeEvidence(ctx, runner, *repo, *pr, *headSHA, paths, truncated, transferProofContexts(reg))
		if smokeErr != nil {
			_, _ = fmt.Fprintf(os.Stdout, "required-gates: smoke probe failed (%v); awaiting full selection\n", smokeErr)
		} else if evidence.Applied {
			mode, modeErr := smokeModeProven(ctx, runner, *repo)
			if modeErr != nil || !mode {
				_, _ = fmt.Fprintf(os.Stdout, "required-gates: caller planner did not record smoke mode (%v); awaiting full selection\n", modeErr)
			} else if subset := smokeSubset(reg, resolved); !smokeHasFastWork(subset) {
				_, _ = fmt.Fprintln(os.Stdout, "required-gates: smoke subset empty (all-heavy selection); awaiting full selection")
			} else {
				smoke = true
				smokeProof = evidence
				resolved = subset
				_, _ = fmt.Fprintf(os.Stdout, "required-gates: smoke tier applies (clean rebase, disjoint base delta); awaiting %d fast job(s), heavy verdicts transfer\n", len(resolved))
			}
		}
	}
	if err := awaitPRRequiredChecks(ctx, runner, *repo, *pr, *headSHA, resolved, *pollInterval, os.Stdout); err != nil {
		return err
	}
	if err := verifyPRHead(ctx, runner, *repo, *pr, *headSHA); err != nil {
		return err
	}
	if smoke {
		if err := writeSmokeProof(ctx, *proofFile, smokeProof); err != nil {
			return err
		}
		return errSmokeProven
	}
	return writeFullProofIfCovered(ctx, runner, *repo, root, paths, truncated, resolved, *proofFile)
}

// selectRequiredGates resolves the blocking gates for a changed-path set to
// their workflow jobs. A truncated path list selects every blocking gate:
// over-selection fails closed in the safe direction (extra gates must pass)
// while a partial path list would silently under-select.
func selectRequiredGates(reg *cigates.Registry, root string, paths []string, truncated bool) ([]resolvedRequiredGate, error) {
	var (
		required []cigates.RequiredGate
		err      error
	)
	if truncated {
		required, err = reg.AllBlockingGates()
	} else {
		required, err = reg.RequiredGates(paths)
	}
	if err != nil {
		return nil, err
	}
	resolved, err := resolveRequiredGateWorkflows(root, required)
	if err != nil {
		return nil, err
	}
	planner, active, err := requiredPlannerGate(root)
	if err != nil {
		return nil, err
	}
	if active {
		resolved = append(resolved, planner)
	}
	_, _ = fmt.Fprintf(os.Stdout, "required-gates: selected %d blocking workflow job(s) for %d changed path(s) (truncated=%v)\n", len(resolved), len(paths), truncated)
	return resolved, nil
}

type ghRunner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

type execGHRunner struct{}

func (execGHRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	// #nosec G204 -- the executable is fixed and arguments are passed directly without a shell.
	return exec.CommandContext(ctx, "gh", args...).Output()
}

type resolvedRequiredGate struct {
	WorkflowFile string
	WorkflowName string
	Job          string
	GateIDs      []string
	// Event is the triggering event whose check rows may satisfy this gate.
	// Empty means pull_request, the original and default aggregation mode.
	Event string
}

type checkRollup struct {
	Name     string `json:"name"`
	State    string `json:"state"`
	Bucket   string `json:"bucket"`
	Workflow string `json:"workflow"`
	Event    string `json:"event"`
	// RunID is the workflow run that owns this row. Rows read through
	// readEventChecks carry it from the suite join; rows read through
	// `gh pr checks` predate run attribution and carry zero, which the log
	// formatter omits. Zero is never a real run id.
	RunID int64 `json:"run_id,omitempty"`
	// HeadBranch and HeadSHA name the tree the owning run was created for,
	// read from the same runs listing as the suite join. Rows read through
	// `gh pr checks` carry neither. They travel with the row so a judged
	// finding can name its evaluated tree beside its run (#8336).
	HeadBranch string `json:"head_branch,omitempty"`
	HeadSHA    string `json:"head_sha,omitempty"`
}

type requiredCheckFinding struct {
	WorkflowName string
	Job          string
	GateIDs      []string
	State        string
	// Called marks a current-attempt Build Test job whose verdict was already
	// attributed by the jobs API and must not be overridden by a direct run.
	Called bool
	// RunID names the workflow run whose check run was judged (#8053), so the
	// await log says which event's verdict a failure came from. Zero means
	// the source row carried no attribution and is omitted from the log.
	RunID int64
	// Event is the triggering event whose run was judged. Empty normalises
	// to pull_request, the original aggregation mode.
	Event string
	// HeadBranch and HeadSHA name the owning run's tree, carried from the
	// judged check row. Empty when the source row carried no attribution;
	// the await header names the tree in that case (#8336).
	HeadBranch string
	HeadSHA    string
}

type requiredCheckEvaluation struct {
	Pending []requiredCheckFinding
	Failed  []requiredCheckFinding
	// Cancelled holds gates whose check never produced a verdict (#6189):
	// CANCELLED, STALE, SKIPPED because the workflow run that owned the job was
	// cancelled, or MISSING because that run was cancelled before the job was
	// created at all. All four are infrastructure state rather than a gate
	// result, and all four want the same operator repair, so they share one
	// bucket and one exit code. It keeps the name `Cancelled` because that is
	// what exit 13, errGateCancelled, the publisher arm, and the four
	// documents describing this contract all call it; see isNotAGateResult in
	// await_notrun.go for exactly what qualifies. Only Failed may publish
	// `failure`.
	Cancelled []requiredCheckFinding
}

func resolveRequiredGateWorkflows(repoRoot string, gates []cigates.RequiredGate) ([]resolvedRequiredGate, error) {
	workflowNames := make(map[string]string)
	nameOwners := make(map[string]string)
	resolved := make([]resolvedRequiredGate, 0, len(gates))
	for _, gate := range gates {
		if filepath.Base(gate.Workflow) != gate.Workflow {
			return nil, fmt.Errorf("workflow %q must be a filename under .github/workflows", gate.Workflow)
		}
		name, ok := workflowNames[gate.Workflow]
		if !ok {
			path := filepath.Join(repoRoot, ".github", "workflows", gate.Workflow)
			raw, err := os.ReadFile(path) // #nosec G304 -- workflow is basename-confined and comes from the committed registry
			if err != nil {
				return nil, fmt.Errorf("read workflow %q: %w", gate.Workflow, err)
			}
			var workflow struct {
				Name string `yaml:"name"`
			}
			if err := yaml.Unmarshal(raw, &workflow); err != nil {
				return nil, fmt.Errorf("parse workflow %q: %w", gate.Workflow, err)
			}
			name = strings.TrimSpace(workflow.Name)
			if name == "" {
				return nil, fmt.Errorf("workflow %q has no non-empty name", gate.Workflow)
			}
			if owner, duplicate := nameOwners[name]; duplicate && owner != gate.Workflow {
				return nil, fmt.Errorf(
					"workflow name %q is shared by %q and %q; required checks cannot be attributed unambiguously",
					name,
					owner,
					gate.Workflow,
				)
			}
			workflowNames[gate.Workflow] = name
			nameOwners[name] = gate.Workflow
		}
		checkNames := gate.CheckNames
		if len(checkNames) == 0 {
			checkNames = []string{gate.Job}
		}
		for _, checkName := range checkNames {
			resolved = append(resolved, resolvedRequiredGate{
				WorkflowFile: gate.Workflow,
				WorkflowName: name,
				Job:          checkName,
				GateIDs:      append([]string(nil), gate.GateIDs...),
			})
		}
	}
	return resolved, nil
}

func evaluateRequiredChecks(
	required []resolvedRequiredGate,
	checks []checkRollup,
	runs runConclusions,
) requiredCheckEvaluation {
	var evaluation requiredCheckEvaluation
	for _, gate := range required {
		matches := matchingChecks(gate, checks)
		if len(matches) == 0 {
			// No check run for this job exists at all; only the owning run's
			// conclusion says whether one ever will. See
			// missingCheckIsCancellationArtifact for both directions (#6189).
			if missingCheckIsCancellationArtifact(gate, runs) {
				evaluation.Cancelled = append(evaluation.Cancelled, findingFor(gate, "MISSING (run cancelled)", 0))
				continue
			}
			evaluation.Pending = append(evaluation.Pending, findingFor(gate, "MISSING", 0))
			continue
		}

		gatePending := false
		var gateFailure, gateCancellation *checkRollup
		for i := range matches {
			check := &matches[i]
			switch {
			case strings.EqualFold(check.Bucket, "pass"):
				continue
			// Before the pending bucket, deliberately: gh reports STALE
			// with bucket "pending", so testing pending first would file a
			// permanently-stale check as still-running and wait it out.
			case isNotAGateResult(*check, runs):
				gateCancellation = check
			// Before the pending-bucket test for the same reason STALE is:
			// gh files SKIPPED in its "skipping" bucket, so a skip whose
			// owning run is still executing would otherwise fall to
			// `default:` and be called a gate failure (#6189, third round).
			case isAwaitingRunConclusion(*check, runs):
				gatePending = true
			case strings.EqualFold(check.Bucket, "pending"):
				gatePending = true
			// After the pending test, deliberately: only a terminal failure
			// verdict can land here, and a FAILURE recorded inside a
			// cancelled run is cancelled infrastructure, not a gate result
			// (#8053). NEUTRAL and SKIPPED keep their own arms -- the
			// predicate matches failure verdicts only, so the genuine-skip
			// and neutral fail-closed rules below cannot drift through it.
			case isFailureInCancelledRun(*check, runs):
				gateCancellation = check
			default:
				gateFailure = check
			}
		}
		// Precedence within one gate is failure > pending > cancelled
		// (#6189).
		//
		// `gh pr checks` de-duplicates rows with the same name, workflow,
		// and event. A gate can still match both its direct check and the
		// Build Test caller alias. A failure in either must beat a pending or
		// cancelled sibling; a pending sibling must beat cancellation. This
		// prevents a direct green row from hiding a called failure, and the
		// outer await gives any Failed finding priority over other gates.
		//
		// It is kept, and ordered deliberately, because this repository does
		// not pin the runner's gh -- the same reason isCancelledCheck in
		// await_notrun.go matches two signals. Failure first: a gate that
		// genuinely concluded failure must keep blocking even when another leg
		// of the same gate was cancelled. Pending before cancelled: a leg
		// still running may yet go red, so reporting "cancelled" while one is
		// in flight would give the head a softer verdict than it may turn out
		// to deserve.
		// TestEvaluateRequiredChecksPrefersPendingOverCancelledWithinOneGate
		// pins that second ordering so it cannot invert unnoticed.
		switch {
		case gateFailure != nil:
			evaluation.Failed = append(evaluation.Failed, findingForCheck(gate, *gateFailure, gateFailure.State))
		case gatePending:
			evaluation.Pending = append(evaluation.Pending, findingFor(gate, "PENDING", 0))
		case gateCancellation != nil:
			evaluation.Cancelled = append(evaluation.Cancelled, findingForCheck(gate, *gateCancellation, gateCancellation.State))
		}
	}
	return evaluation
}

func matchingChecks(gate resolvedRequiredGate, checks []checkRollup) []checkRollup {
	wantEvent := gate.Event
	if wantEvent == "" {
		wantEvent = eventPullRequest
	}
	var exact []checkRollup
	calledName := calledCheckName(gate.WorkflowFile, gate.Job)
	for _, check := range checks {
		if check.Event != wantEvent {
			continue
		}
		if check.Workflow == gate.WorkflowName && check.Name == gate.Job ||
			calledName != "" && check.Workflow == "Build Test" && check.Name == calledName {
			exact = append(exact, check)
		}
	}
	return exact
}

// awaitRequiredChecks polls readChecks until every selected gate reaches a
// verdict, the context ends, or one fails. event scopes the workflow-run
// conclusions consulted for SKIPPED and MISSING gates to the same event the
// check rows came from.
func awaitRequiredChecks(
	ctx context.Context,
	runner ghRunner,
	repo string,
	headSHA string,
	event string,
	readChecks func(context.Context) ([]checkRollup, error),
	required []resolvedRequiredGate,
	pollInterval time.Duration,
	out io.Writer,
) error {
	if len(required) == 0 {
		_, _ = fmt.Fprintln(out, "required-gates: no blocking gates selected for the changed paths")
		return nil
	}
	if pollInterval <= 0 {
		return fmt.Errorf("poll interval must be positive")
	}

	lastPending := ""
	waitInterval := pollInterval
	maxWaitInterval := 5 * time.Minute
	if pollInterval > maxWaitInterval {
		maxWaitInterval = pollInterval
	}
	for {
		checks, err := readChecks(ctx)
		if err != nil {
			return err
		}
		// A SKIPPED selected check, or a selected gate with no check at all
		// once nothing else is still running, needs the run conclusions;
		// neither is the common path, so the call is not made on it (#6189).
		// needsRunConclusions carries why each is gated the way it is.
		//
		// A lookup failure is reported and then treated as "nothing known
		// cancelled", which leaves a skipped gate publishing `failure` exactly
		// as it did before this change and a missing gate pending exactly as
		// it did -- degraded to the old behaviour, never to a pass.
		var runs runConclusions
		if needsRunConclusions(required, checks) {
			runs, err = workflowRunConclusionsForEvent(ctx, runner, repo, headSHA, event)
			if err != nil {
				_, _ = fmt.Fprintf(out,
					"required-gates: could not read workflow run conclusions (%v); "+
						"a skipped gate stays a failure and a missing gate stays pending\n", err)
				runs = nil
			}
		}
		evaluation := evaluateRequiredChecks(required, checks, runs)
		if len(evaluation.Failed) > 0 {
			// A failed rollup row is not yet a failed gate (#8053): the
			// row may belong to a superseded event. Attribute each one to
			// the newest workflow run for the commit before judging it.
			// Only failures confirmed in the newest run keep the gate-failed
			// outcome below, so this stays the one outcome allowed to
			// publish `failure` (#6075) and the classification stays
			// structural.
			attributed := attributeFailedGatesToNewestRun(ctx, runner, repo, headSHA, event, evaluation.Failed, runs, out)
			if len(attributed.Confirmed) > 0 {
				return fmt.Errorf("%w: %s", errGateFailed, formatFindings(attributed.Confirmed))
			}
			evaluation.Failed = nil
			evaluation.Pending = append(evaluation.Pending, attributed.Redirected.Pending...)
			evaluation.Cancelled = append(evaluation.Cancelled, attributed.Redirected.Cancelled...)
		}
		if len(evaluation.Pending) == 0 {
			if len(evaluation.Cancelled) > 0 {
				// Every remaining gate is terminal and at least one was
				// cancelled (#6189). Wrapped so classifyAwaitOutcome
				// recognises this structurally: it is infrastructure state,
				// not a gate result, and must not publish `failure`.
				return fmt.Errorf("%w: %s", errGateCancelled, formatFindings(evaluation.Cancelled))
			}
			_, _ = fmt.Fprintf(out, "required-gates: all %d selected blocking workflow job(s) passed\n", len(required))
			return nil
		}
		pending := formatFindings(evaluation.Pending)
		if pending != lastPending {
			_, _ = fmt.Fprintf(out, "required-gates: waiting for %d job(s): %s\n", len(evaluation.Pending), pending)
			lastPending = pending
		}

		timer := time.NewTimer(waitInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			// Gates were STILL PENDING when the wait ended, so this is not a
			// gate result and must not publish `failure` (#6075). The
			// ctx.Err() cause is kept in the message for the operator.
			return fmt.Errorf("%w: timed out waiting for (%s): %v", errStillRunning, pending, ctx.Err())
		case <-timer.C:
		}
		if waitInterval < maxWaitInterval {
			waitInterval *= 2
			if waitInterval > maxWaitInterval {
				waitInterval = maxWaitInterval
			}
		}
	}
}
