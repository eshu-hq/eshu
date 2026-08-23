// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	pr := fs.Int("pr", 0, "pull request number")
	headSHA := fs.String("head-sha", "", "exact pull request head SHA for this workflow run")
	pollInterval := fs.Duration("poll-interval", 30*time.Second, "interval between GitHub check-rollup reads")
	timeout := fs.Duration("timeout", 55*time.Minute, "maximum time to wait for selected blocking checks")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *registry == "" || *repo == "" || *pr <= 0 || strings.TrimSpace(*headSHA) == "" {
		return fmt.Errorf("--registry, --repo, --pr, and --head-sha are required")
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
	if err := verifyPRHead(ctx, runner, *repo, *pr, *headSHA); err != nil {
		return err
	}
	paths, err := changedPathsForPR(ctx, runner, *repo, *pr)
	if err != nil {
		return err
	}
	required, err := reg.RequiredGates(paths)
	if err != nil {
		return err
	}
	resolved, err := resolveRequiredGateWorkflows(root, required)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "required-gates: selected %d blocking workflow job(s) for %d changed path(s)\n", len(resolved), len(paths))
	if err := awaitPRRequiredChecks(ctx, runner, *repo, *pr, *headSHA, resolved, *pollInterval, os.Stdout); err != nil {
		return err
	}
	return verifyPRHead(ctx, runner, *repo, *pr, *headSHA)
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
}

type checkRollup struct {
	Name     string `json:"name"`
	State    string `json:"state"`
	Bucket   string `json:"bucket"`
	Workflow string `json:"workflow"`
	Event    string `json:"event"`
}

type requiredCheckFinding struct {
	WorkflowName string
	Job          string
	GateIDs      []string
	State        string
}

type requiredCheckEvaluation struct {
	Pending []requiredCheckFinding
	Failed  []requiredCheckFinding
	// Cancelled holds gates whose check never produced a verdict (#6189):
	// CANCELLED, STALE, or SKIPPED because the workflow run that owned the job
	// was cancelled. All three are infrastructure state rather than a gate
	// result, and all three want the same operator repair, so they share one
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
			evaluation.Pending = append(evaluation.Pending, findingFor(gate, "MISSING"))
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
			default:
				gateFailure = check
			}
		}
		// Precedence within one gate is failure > pending > cancelled
		// (#6189).
		//
		// Scope, stated honestly: with the reader this command actually has,
		// `matches` holds at most one row, so no ordering here is observable
		// today. `gh pr checks` de-duplicates check contexts on
		// name/workflow/event (read in cli/cli v2.97.0
		// pkg/cmd/pr/checks/aggregate.go, eliminateDuplicates -- the version
		// installed here), which is the same triple matchingChecks keys on.
		// The precedence that decides real verdicts is the one in
		// awaitPRRequiredChecks, where a non-empty Failed wins over
		// everything else.
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
			evaluation.Failed = append(evaluation.Failed, findingFor(gate, gateFailure.State))
		case gatePending:
			evaluation.Pending = append(evaluation.Pending, findingFor(gate, "PENDING"))
		case gateCancellation != nil:
			evaluation.Cancelled = append(evaluation.Cancelled, findingFor(gate, gateCancellation.State))
		}
	}
	return evaluation
}

func matchingChecks(gate resolvedRequiredGate, checks []checkRollup) []checkRollup {
	var exact []checkRollup
	for _, check := range checks {
		if check.Workflow != gate.WorkflowName || check.Event != "pull_request" {
			continue
		}
		if check.Name == gate.Job {
			exact = append(exact, check)
		}
	}
	return exact
}

func findingFor(gate resolvedRequiredGate, state string) requiredCheckFinding {
	return requiredCheckFinding{
		WorkflowName: gate.WorkflowName,
		Job:          gate.Job,
		GateIDs:      append([]string(nil), gate.GateIDs...),
		State:        state,
	}
}

func changedPathsForPR(ctx context.Context, runner ghRunner, repo string, pr int) ([]string, error) {
	endpoint := "repos/" + repo + "/pulls/" + strconv.Itoa(pr) + "/files"
	output, err := runner.Run(ctx, "api", "--paginate", "--slurp", endpoint)
	if err != nil {
		return nil, fmt.Errorf("list changed files for PR #%d: %w", pr, err)
	}
	var pages [][]struct {
		Filename         string `json:"filename"`
		PreviousFilename string `json:"previous_filename"`
	}
	if err := json.Unmarshal(output, &pages); err != nil {
		return nil, fmt.Errorf("decode changed files for PR #%d: %w", pr, err)
	}
	seen := make(map[string]struct{})
	var paths []string
	fileCount := 0
	appendPath := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, duplicate := seen[path]; duplicate {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	for _, page := range pages {
		for _, file := range page {
			fileCount++
			appendPath(file.Filename)
			appendPath(file.PreviousFilename)
		}
	}
	countOutput, err := runner.Run(
		ctx,
		"pr", "view", strconv.Itoa(pr),
		"--repo", repo,
		"--json", "changedFiles",
		"--jq", ".changedFiles",
	)
	if err != nil {
		return nil, fmt.Errorf("read changed-file count for PR #%d: %w", pr, err)
	}
	expectedCount, err := strconv.Atoi(strings.TrimSpace(string(countOutput)))
	if err != nil {
		return nil, fmt.Errorf("decode changed-file count for PR #%d: %w", pr, err)
	}
	if fileCount != expectedCount {
		return nil, fmt.Errorf(
			"pull-files response for PR #%d returned %d of %d changed files; refusing partial gate selection",
			pr,
			fileCount,
			expectedCount,
		)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("PR #%d has no changed file paths", pr)
	}
	return paths, nil
}

func verifyPRHead(ctx context.Context, runner ghRunner, repo string, pr int, expected string) error {
	output, err := runner.Run(
		ctx,
		"pr", "view", strconv.Itoa(pr),
		"--repo", repo,
		"--json", "headRefOid",
		"--jq", ".headRefOid",
	)
	if err != nil {
		return fmt.Errorf("read PR #%d head: %w", pr, err)
	}
	actual := strings.TrimSpace(string(output))
	if actual != expected {
		return fmt.Errorf("PR #%d head changed while aggregating checks: got %s, want %s", pr, actual, expected)
	}
	return nil
}

func awaitPRRequiredChecks(
	ctx context.Context,
	runner ghRunner,
	repo string,
	pr int,
	headSHA string,
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
		checks, err := readPRChecks(ctx, runner, repo, pr)
		if err != nil {
			return err
		}
		// Only a SKIPPED selected check needs the run conclusions, and a
		// skipped selected check is rare, so the call is not made on the
		// common path (#6189). A lookup failure is reported and then treated
		// as "nothing known cancelled", which leaves a skipped gate publishing
		// `failure` exactly as it did before this change -- degraded to the
		// old behaviour, never to a pass.
		var runs runConclusions
		if anySelectedCheckSkipped(required, checks) {
			runs, err = workflowRunConclusions(ctx, runner, repo, headSHA)
			if err != nil {
				_, _ = fmt.Fprintf(out,
					"required-gates: could not read workflow run conclusions (%v); a skipped gate stays a failure\n", err)
				runs = nil
			}
		}
		evaluation := evaluateRequiredChecks(required, checks, runs)
		if len(evaluation.Failed) > 0 {
			// Wrapped so classifyAwaitOutcome recognises this structurally
			// (#6075): this is the one outcome allowed to publish `failure`.
			return fmt.Errorf("%w: %s", errGateFailed, formatFindings(evaluation.Failed))
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

func readPRChecks(ctx context.Context, runner ghRunner, repo string, pr int) ([]checkRollup, error) {
	output, commandErr := runner.Run(
		ctx,
		"pr", "checks", strconv.Itoa(pr),
		"--repo", repo,
		"--json", "name,state,bucket,workflow,event",
	)
	var checks []checkRollup
	if err := json.Unmarshal(output, &checks); err != nil {
		if commandErr != nil {
			return nil, fmt.Errorf("read PR #%d checks: %w", pr, commandErr)
		}
		return nil, fmt.Errorf("decode PR #%d checks: %w", pr, err)
	}
	// `gh pr checks` intentionally exits 8 while checks are pending and 1 when
	// a check is red. The JSON is still authoritative in both cases, so a valid
	// payload is evaluated instead of treating those status exits as API errors.
	return checks, nil
}

func formatFindings(findings []requiredCheckFinding) string {
	parts := make([]string, 0, len(findings))
	for _, finding := range findings {
		detail := finding.WorkflowName + " / " + finding.Job + " [" + strings.Join(finding.GateIDs, ",") + "]"
		if finding.State != "" {
			detail += "=" + finding.State
		}
		parts = append(parts, detail)
	}
	return strings.Join(parts, "; ")
}
