// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Why this file exists (#6189, #6218 review rounds 2-4).
//
// Four review rounds found the same defect four times, and three of them were
// one class: a guard located its target inside the publisher's `run:` script
// by matching a SUBSTRING, and a comment moved the match. The publisher's run
// block is 55 lines of which 34 are prose, so prose is not an exotic input
// there -- it is most of the file. The last of those, round 4's K-1, needed
// only this:
//
//	fi
//	# the gh api -X POST call below publishes the status
//	state=success
//	gh api -X POST "repos/${GITHUB_REPOSITORY}/statuses/${HEAD_SHA}" \
//
// Both guards anchored on the FIRST `gh api -X POST`, the decoy comment
// carried that text, so both of them read the region ABOVE the injected
// assignment and cleared a publisher that posts `success` for an exit code
// meaning a required gate genuinely failed.
//
// The answer is not a fourth textual patch. #6194 spent nine review rounds
// extending a textual model of bash one bypass at a time without converging,
// and every fix here has the same shape as those did. So this file stops
// reading the script and RUNS it: `gh` is replaced by a recorder on PATH, the
// publisher's own shell executes under bash for one AGGREGATE_CODE, and the
// caller asserts on the argv the publisher actually handed the recorder. A
// spelling nobody anticipated cannot hide from that, because the assertion
// never looks at the spelling.
//
// What this deliberately does NOT cover -- see EvaluatePublisher's own doc
// comment, which carries the boundary in full.

const (
	// The sentinel environment the publisher runs against. They are values a
	// real run could never produce, so an assertion that matches one is
	// matching this harness's input rather than something the workflow
	// invented, and `example.invalid` cannot resolve if anything ever did try
	// to reach it.
	publisherProbeRepository = "eshu-hq/required-gates-probe"
	publisherProbeHeadSHA    = "0000000000000000000000000000000000006189"
	publisherProbeTargetURL  = "https://example.invalid/actions/runs/6189"

	// publisherRecordSeparator and publisherFieldSeparator frame the recorded
	// argv. ASCII RS and US are used rather than a newline or a space because
	// a status description legitimately contains both, and an argument that
	// could split a record is an argument that could hide one.
	publisherRecordSeparator = "\x1e"
	publisherFieldSeparator  = "\x1f"

	// publisherEvalTimeout bounds one evaluation. The publisher is a few
	// dozen lines of branch-and-post, so anything approaching this is a shape
	// that blocks -- a `read`, a missing binary prompting, an accidental
	// loop -- and the gate must fail loud rather than hang a CI job.
	publisherEvalTimeout = 30 * time.Second
)

// PublishedStatus is one commit-status publish observed while the required
// status publisher ran. Every field comes from the argv the publisher handed
// the intercepted `gh`, after the shell finished expanding it, so it is what
// GitHub would have been asked to store -- not the text that produced it.
type PublishedStatus struct {
	// URL is the API path the publish targeted, e.g.
	// `repos/<owner>/<repo>/statuses/<sha>`.
	URL string
	// Context is the `-f context=` value: which commit status this writes.
	Context string
	// State is the `-f state=` value: success, failure, error, or pending.
	State string
	// Description is the `-f description=` value, empty when the publisher
	// passes none. A description is optional by contract; see
	// validatePublishedOutcomes.
	Description string
	// Args is the whole recorded argv, for error messages that need to show
	// what was actually run.
	Args []string
}

// PublisherRun is everything one evaluation of the publisher observed.
type PublisherRun struct {
	// Publishes lists every commit-status publish the run attempted, in
	// order. Empty means the publisher posted nothing at all, which is the
	// contract for the still-running exit code.
	Publishes []PublishedStatus
	// ExitCode is the shell's exit status. The publisher ends with
	// `[[ "${state}" == "success" ]]`, so a non-zero code here is the normal
	// result for every non-passing outcome; it is captured for diagnosis, not
	// asserted on.
	ExitCode int
	// Output is the combined stdout/stderr, carried into error messages so a
	// failure names what the shell actually said.
	Output string
}

// EvaluatePublisher runs the required-status publisher's shell for one await
// exit code with `gh` intercepted, and reports every status it tried to
// publish. It is the authoritative check on what the publisher posts: the
// caller asserts on observed argv, so any assignment, arm, quoting, or
// argument spelling that changes the published value shows up whether or not
// anyone anticipated that spelling.
//
// Hermetic by construction. PATH holds one empty directory, so no real `gh`
// and no other binary is reachable through it; HOME and the working directory
// are a private temp dir that is removed afterwards; the GitHub token
// variables are set empty; and the run is bounded by publisherEvalTimeout.
// Nothing here needs a network or a credential.
//
// What it does NOT cover, stated so the boundary is not mistaken for
// completeness:
//
//   - It runs `bash --noprofile --norc -e <script>`. GitHub's default shell
//     for a `run:` step is `bash -e {0}`, so `-e` matches and the two rc
//     flags only remove a developer's local startup files. A publisher that
//     depended on `pipefail`, on `set -u`, or on a login shell would behave
//     differently on the runner than it does here.
//   - The environment is the sentinel set above plus PATH, HOME and BASH_ENV.
//     A publisher reading any other variable -- a `$GITHUB_ENV` write from an
//     earlier step, a runner-provided variable, a step `env:` entry this
//     harness does not know -- sees it empty, so a value that only exists on
//     the runner is not modelled.
//   - Interception is a bash function. A spelling that bypasses functions --
//     `command gh`, `env gh`, a non-bash child process -- is not recorded.
//     That fails CLOSED rather than silently: PATH holds nothing, so the call
//     fails, `-e` ends the script, and the per-code assertions report that
//     nothing was published.
//   - A publisher that never calls `gh` at all, and posts the status some
//     other way, is not observed. It fails closed for the same reason.
//   - It observes ONE step's script. Whether that step is the right step,
//     whether it runs at all, and whether it targets the right head SHA
//     through the workflow's `env:` are YAML-level facts checked separately.
//   - GitHub is not modelled. That the API would accept the argv, and what it
//     would do with a second publish to the same context, is outside this.
func EvaluatePublisher(run string, aggregateCode int) (PublisherRun, error) {
	harness, err := newPublisherHarness(run)
	if err != nil {
		return PublisherRun{}, err
	}
	defer harness.close()
	return harness.evaluate("success", strconv.Itoa(aggregateCode))
}

// publisherHarness is the sandbox one publisher script runs in, reusable
// across exit codes. Only the record file varies per evaluation, so only it
// is reset.
//
// The recorder is a bash FUNCTION injected through BASH_ENV rather than an
// executable named `gh` on PATH. Both intercept the call; the function is far
// cheaper, because spawning the stub executable dominated everything else
// here. Measured on this repository, 30 evaluations: 6.7s with a freshly
// written executable stub per evaluation, 1.7s with one shared executable,
// 0.1s with the function. Same numbers, differently: 224ms, 56ms and 3.2ms
// per evaluation, against 2.8ms for a bash spawn that calls nothing.
type publisherHarness struct {
	bashPath string
	dir      string
	binDir   string
	prelude  string
	script   string
	record   string
}

// newPublisherHarness lays out the temp dir the publisher runs in: the `gh`
// recorder on the only PATH entry it will have, the publisher's own script
// written VERBATIM, and the record file. The script is written with no
// prologue and no wrapper of any kind -- anything prepended would be shell
// this harness invented, and the whole point is that what runs is exactly
// what the runner runs.
func newPublisherHarness(run string) (*publisherHarness, error) {
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		return nil, fmt.Errorf(
			"bash is required to evaluate the terminal status publisher, and the publisher's behaviour "+
				"cannot be proven without running it: %w", err)
	}
	dir, err := os.MkdirTemp("", "eshu-publisher-")
	if err != nil {
		return nil, fmt.Errorf("create publisher evaluation directory: %w", err)
	}
	harness := &publisherHarness{
		bashPath: bashPath,
		dir:      dir,
		binDir:   filepath.Join(dir, "bin"),
		prelude:  filepath.Join(dir, "prelude.sh"),
		script:   filepath.Join(dir, "publisher.sh"),
		record:   filepath.Join(dir, "gh-calls"),
	}
	if err := harness.write(run); err != nil {
		harness.close()
		return nil, err
	}
	return harness, nil
}

// write creates the recorder prelude, the empty PATH directory, and the
// publisher's script.
//
// `export -f gh` so a nested bash inherits the recorder too, and the PATH
// directory is created but left EMPTY: any spelling that skips the function
// -- `command gh`, `env gh`, a non-bash child -- then finds no `gh` at all
// and the publisher dies under `-e` with nothing recorded, which the per-code
// assertions report as "posts no status at all". Failing closed is the point;
// a real `gh` is never reachable from in here.
func (h *publisherHarness) write(run string) error {
	if err := os.MkdirAll(h.binDir, 0o700); err != nil {
		return fmt.Errorf("create publisher PATH directory: %w", err)
	}
	prelude := "gh() {\n" +
		"  printf '%s\\037' \"$@\" >> \"${ESHU_PUBLISHER_RECORD}\"\n" +
		"  printf '\\036' >> \"${ESHU_PUBLISHER_RECORD}\"\n" +
		"  return 0\n" +
		"}\n" +
		"export -f gh\n"
	if err := os.WriteFile(h.prelude, []byte(prelude), 0o600); err != nil {
		return fmt.Errorf("write gh recorder: %w", err)
	}
	if err := os.WriteFile(h.script, []byte(run), 0o600); err != nil {
		return fmt.Errorf("write publisher script: %w", err)
	}
	return nil
}

// close removes the sandbox.
func (h *publisherHarness) close() {
	_ = os.RemoveAll(h.dir)
}

// evaluate runs the publisher once and reports what it posted.
func (h *publisherHarness) evaluate(pendingOutcome, aggregateCode string) (PublisherRun, error) {
	if err := os.WriteFile(h.record, nil, 0o600); err != nil {
		return PublisherRun{}, fmt.Errorf("reset publisher call record: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), publisherEvalTimeout)
	defer cancel()
	// #nosec G204 -- bashPath comes from LookPath and every argument is a
	// path this harness created under its own temp dir. The script is the
	// committed workflow's own shell, which is the subject under test:
	// running it is the point, and PATH/HOME/env are stripped to the sentinel
	// set above precisely so running it is safe.
	cmd := exec.CommandContext(ctx, h.bashPath, "--noprofile", "--norc", "-e", h.script)
	cmd.Dir = h.dir
	cmd.Env = []string{
		"PATH=" + h.binDir,
		"HOME=" + h.dir,
		"BASH_ENV=" + h.prelude,
		"ESHU_PUBLISHER_RECORD=" + h.record,
		"PENDING_OUTCOME=" + pendingOutcome,
		"AGGREGATE_CODE=" + aggregateCode,
		"GITHUB_REPOSITORY=" + publisherProbeRepository,
		"HEAD_SHA=" + publisherProbeHeadSHA,
		"TARGET_URL=" + publisherProbeTargetURL,
		"GH_TOKEN=",
		"GITHUB_TOKEN=",
	}
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	runErr := cmd.Run()
	if ctx.Err() != nil {
		return PublisherRun{}, fmt.Errorf(
			"terminal status publisher did not finish within %s for AGGREGATE_CODE=%s; output so far: %s",
			publisherEvalTimeout, aggregateCode, combined.String())
	}
	result := PublisherRun{Output: combined.String()}
	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &exitErr):
		// Expected for every non-passing outcome: the publisher's own closing
		// `[[ "${state}" == "success" ]]` is what fails the step.
		result.ExitCode = exitErr.ExitCode()
	default:
		return PublisherRun{}, fmt.Errorf(
			"execute terminal status publisher for AGGREGATE_CODE=%s: %w; output: %s",
			aggregateCode, runErr, combined.String())
	}
	raw, err := os.ReadFile(h.record) // #nosec G304 -- path created by this harness under its own temp dir
	if err != nil {
		return PublisherRun{}, fmt.Errorf("read intercepted publisher calls: %w", err)
	}
	result.Publishes = parsePublisherCalls(string(raw))
	return result, nil
}

// parsePublisherCalls turns the recorder's framed argv into the status
// publishes it represents. A recorded call that names no `/statuses/` path is
// some other use of `gh` and is not a publish.
func parsePublisherCalls(raw string) []PublishedStatus {
	var published []PublishedStatus
	for _, record := range strings.Split(raw, publisherRecordSeparator) {
		if record == "" {
			continue
		}
		fields := strings.Split(record, publisherFieldSeparator)
		if n := len(fields); n > 0 && fields[n-1] == "" {
			fields = fields[:n-1]
		}
		if status, ok := statusPublishFromArgs(fields); ok {
			published = append(published, status)
		}
	}
	return published
}

// statusPublishFromArgs reads one recorded argv as a commit-status publish.
// The fields are read positionally the way `gh api` reads them, so the value
// reported is the post-expansion argument, never the text in the workflow.
func statusPublishFromArgs(args []string) (PublishedStatus, bool) {
	status := PublishedStatus{Args: args}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") && strings.Contains(arg, "/statuses/") {
			status.URL = arg
			continue
		}
		switch arg {
		case "-f", "-F", "--field", "--raw-field":
		default:
			continue
		}
		if i+1 >= len(args) {
			continue
		}
		i++
		name, value, found := strings.Cut(args[i], "=")
		if !found {
			continue
		}
		switch name {
		case "state":
			status.State = value
		case "context":
			status.Context = value
		case "description":
			status.Description = value
		}
	}
	if status.URL == "" {
		return PublishedStatus{}, false
	}
	return status, true
}

// TerminalPublisherRun returns the shell script of the terminal
// required-status publisher step in the workflow at path, so a caller outside
// this package can evaluate the same step the registry gate validates rather
// than locating it with its own substring search.
//
// The step is identified structurally: a step that posts to a `/statuses/`
// path and is not the job's leading pending publisher, which
// validateTrustedAggregator already requires to be the job's first step.
// Anything other than exactly one such step in the file is an error rather
// than a guess, because guessing here would evaluate a step nobody meant.
func TerminalPublisherRun(path string) (string, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- caller-supplied committed workflow path
	if err != nil {
		return "", fmt.Errorf("read workflow %s: %w", path, err)
	}
	var workflow requiredWorkflowFile
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return "", fmt.Errorf("parse workflow %s: %w", path, err)
	}
	jobNames := make([]string, 0, len(workflow.Jobs))
	for name := range workflow.Jobs {
		jobNames = append(jobNames, name)
	}
	// Sorted rather than map order: Go randomises map iteration, so an
	// unsorted walk would report a different "first" match run to run.
	sort.Strings(jobNames)
	var found []string
	for _, name := range jobNames {
		for _, step := range terminalPublisherSteps(workflow.Jobs[name].Steps) {
			found = append(found, step.Run)
		}
	}
	if len(found) != 1 {
		return "", fmt.Errorf(
			"workflow %s has %d terminal status publisher steps, want exactly 1; the publisher contract moved",
			path, len(found))
	}
	return found[0], nil
}

// terminalPublisherSteps returns every step that posts a commit status and is
// not the job's leading pending publisher.
//
// Selecting on "posts a status, and is not step 0" replaced an earlier
// `strings.Contains(step.Run, "state=failure")`. That spelling made the whole
// terminal-publisher contract conditional on one literal appearing somewhere
// in the step, comments included -- delete it and every check below stopped
// running with nothing to say so. Step 0 is the pending publisher by
// contract; validateTrustedAggregator reports it when it is not.
func terminalPublisherSteps(steps []requiredWorkflowStep) []requiredWorkflowStep {
	var terminal []requiredWorkflowStep
	for index, step := range steps {
		if index == 0 || !publishesCommitStatus(step.Run) {
			continue
		}
		terminal = append(terminal, step)
	}
	return terminal
}

// publishesCommitStatus reports whether a step's shell posts to the commit
// status API at all.
func publishesCommitStatus(run string) bool {
	return strings.Contains(run, "/statuses/")
}
