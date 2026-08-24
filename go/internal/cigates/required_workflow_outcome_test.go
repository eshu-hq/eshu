// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// What this file asserts, and why it stopped asserting text (#6218 review
// rounds 2-4).
//
// The publisher's job is to put the right verdict on the head SHA for each
// await exit code. Four rounds of review tried to prove that by reading the
// step's shell -- one check per case arm, one for the `-f state=` argument,
// one for the lines between `esac` and the publish -- and three of them were
// defeated by an ordinary prose comment moving a substring anchor. Round 4's
// K-1 needed one comment line to blind two guards at once and publish
// `success` for a genuinely failed gate.
//
// So these tests do not read the publisher. They mutate it, run it, and look
// at what it posted. Every attack below is one of those rounds' seeds; each
// asserts that the observed publish is wrong, never that some phrase is
// missing.

// fixtureCaseBlock is the terminal publisher's case block as the shared
// fixture spells it. The attacks below rebuild it, so they assert it is still
// there rather than silently replacing nothing.
const fixtureCaseBlock = `          case "${AGGREGATE_CODE}" in
            0) state=success; description='All gates passed' ;;
            10) state=failure; description='A required gate failed' ;;
            11) exit 0 ;;
            13) state=error; description='A required gate was cancelled' ;;
            *) state=error; description='Aggregation broke' ;;
          esac
`

// realShapedPublisher rebuilds the fixture's terminal step in the shape the
// real .github/workflows/required-gates.yml actually has: optional leading
// prose, then a PENDING_OUTCOME guard that assigns state=error on its own
// branch, and only then the AGGREGATE_CODE case.
//
// The prose and the guard are what made the round 2-4 attacks reachable. A
// comment is an ordinary input to this step -- the real one is 55 lines of
// which 34 are prose -- and the guard puts a second `state=error` above the
// case block for a decoyed anchor to land on.
func realShapedPublisher(t *testing.T, lead, arm string) string {
	t.Helper()
	if !strings.Contains(trustedRequiredWorkflow, fixtureCaseBlock) {
		t.Fatal("the fixture's case block moved; these attacks would be replacing nothing")
	}
	replacement := lead + `          if [[ "${PENDING_OUTCOME}" != "success" ]]; then
            state=error
            description='Required gate aggregation could not start'
          else
            case "${AGGREGATE_CODE}" in
              0) state=success; description='All gates passed' ;;
              10) state=failure; description='A required gate failed' ;;
              11) exit 0 ;;
` + arm + `              *) state=error; description='Aggregation broke' ;;
            esac
          fi
`
	body := strings.Replace(trustedRequiredWorkflow, fixtureCaseBlock, replacement, 1)
	if body == trustedRequiredWorkflow {
		t.Fatal("the replacement did not change the fixture")
	}
	return body
}

// cancelledArm is the arm the real workflow ships, in realShapedPublisher's
// indentation.
const cancelledArm = "              13) state=error; description='A required gate was cancelled' ;;\n"

// injectBeforePublish puts lines between the publisher's branch and its `gh`
// call -- the gap round 3 closed with a textual anchor and round 4 walked
// straight through. It asserts the anchor is present exactly once, so an
// attack cannot pass by injecting nothing.
func injectBeforePublish(t *testing.T, body, injected string) string {
	t.Helper()
	const anchor = "          fi\n          gh api -X POST"
	if got := strings.Count(body, anchor); got != 1 {
		t.Fatalf("publisher's fi/gh api pair appears %d times, want exactly 1; the injection point moved", got)
	}
	return strings.Replace(body, anchor, "          fi\n"+injected+"          gh api -X POST", 1)
}

// requireErrorContaining fails unless some error names the given text.
func requireErrorContaining(t *testing.T, errs []error, want string) {
	t.Helper()
	for _, err := range errs {
		if strings.Contains(err.Error(), want) {
			return
		}
	}
	t.Fatalf("expected an error containing %q, got %v", want, errs)
}

// TestCheckRequiredStatusWorkflows_AcceptsTheRealShapedPublisher is the
// positive control for every attack below. Without it a validator that
// rejected everything would pass all of them and prove nothing.
func TestCheckRequiredStatusWorkflows_AcceptsTheRealShapedPublisher(t *testing.T) {
	t.Parallel()

	body := realShapedPublisher(t, "", cancelledArm)
	if errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry()); len(errs) != 0 {
		t.Fatalf("the real-shaped publisher must be accepted, got %v", errs)
	}
}

// TestCheckRequiredStatusWorkflows_AcceptsAnEpilogueComment is the second
// positive control, and the one that matters for a check built on execution:
// a comment is what the real publisher is mostly made of, and a validator
// that reddened on an added comment line would be unusable.
func TestCheckRequiredStatusWorkflows_AcceptsAnEpilogueComment(t *testing.T) {
	t.Parallel()

	body := injectBeforePublish(t, realShapedPublisher(t, "", cancelledArm),
		"          # the gh api -X POST call below publishes what the branch decided\n")
	if errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry()); len(errs) != 0 {
		t.Fatalf("a comment before the publish must be accepted, got %v", errs)
	}
}

// TestCheckRequiredStatusWorkflows_RejectsAStateAssignmentBehindADecoyComment
// is round 4's K-1, the finding this whole mechanism exists for.
//
// Round 3 closed the gap between `esac` and the publish by anchoring two new
// guards on the substring `gh api -X POST`. Both took the FIRST occurrence, so
// one comment line carrying that text moved both anchors above the injection
// point and the assignment below became invisible to both. The registry gate
// and the Go suite were green while `AGGREGATE_CODE=10` -- a gate that
// genuinely concluded failure -- published `success` on the head SHA, passed
// the step's own closing `[[ "${state}" == "success" ]]`, and so turned green
// the one status this repository's ruleset requires on `main`.
//
// Nothing here looks for the comment, the assignment, or their order. The
// publisher is run and the posted state is read.
func TestCheckRequiredStatusWorkflows_RejectsAStateAssignmentBehindADecoyComment(t *testing.T) {
	t.Parallel()

	body := injectBeforePublish(t, realShapedPublisher(t, "", cancelledArm),
		"          # the gh api -X POST call below publishes the status\n          state=success\n")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a state assignment after the branch must be rejected even when a comment names the publish " +
			"command above it; it makes every arm decorative and publishes success for a failed gate")
	}
	requireErrorContaining(t, errs, `posts state="success" for AGGREGATE_CODE=10`)
}

// TestCheckRequiredStatusWorkflows_RejectsAStateAssignmentBeforeThePublish is
// round 3's H-1, the same door without the decoy. Kept so the plain shape
// cannot regress while attention is on the decoyed one.
func TestCheckRequiredStatusWorkflows_RejectsAStateAssignmentBeforeThePublish(t *testing.T) {
	t.Parallel()

	body := injectBeforePublish(t, realShapedPublisher(t, "", cancelledArm), "          state=success\n")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a state assignment between the branch and the publish must be rejected")
	}
	requireErrorContaining(t, errs, `posts state="success" for AGGREGATE_CODE=10`)
}

// TestCheckRequiredStatusWorkflows_RejectsAnExportedStateAssignment covers the
// spelling that walked around the arm reader's leading-assignment rule. Under
// execution it is not a new case to model: `export state=success` sets state,
// so the publish carries success, so it is caught by the same assertion.
func TestCheckRequiredStatusWorkflows_RejectsAnExportedStateAssignment(t *testing.T) {
	t.Parallel()

	body := injectBeforePublish(t, realShapedPublisher(t, "", cancelledArm), "          export state=success\n")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("an exported state assignment before the publish must be rejected")
	}
	requireErrorContaining(t, errs, `posts state="success" for AGGREGATE_CODE=10`)
}

// TestCheckRequiredStatusWorkflows_RejectsACommandSubstitutedState is the
// shape the textual reader refused to judge and reported as unparseable.
// Execution has no such category: the substitution runs, and what it produced
// is what gets asserted.
func TestCheckRequiredStatusWorkflows_RejectsACommandSubstitutedState(t *testing.T) {
	t.Parallel()

	body := injectBeforePublish(t, realShapedPublisher(t, "", cancelledArm), "          state=$(printf success)\n")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a state built by command substitution must be judged by what it produces, not waved through")
	}
	requireErrorContaining(t, errs, `posts state="success" for AGGREGATE_CODE=10`)
}

// TestCheckRequiredStatusWorkflows_RejectsAPublishedStateLiteral is round 2's
// finding: the arms can all be perfect while the `gh api` call hard-codes what
// it posts, so the branch decides nothing.
func TestCheckRequiredStatusWorkflows_RejectsAPublishedStateLiteral(t *testing.T) {
	t.Parallel()

	body := strings.Replace(realShapedPublisher(t, "", cancelledArm),
		`-f state="${state}"`, `-f state=success`, 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a hard-coded -f state= argument must be rejected; every outcome would publish that one status")
	}
	requireErrorContaining(t, errs, `posts state="success" for AGGREGATE_CODE=10`)
}

// TestCheckRequiredStatusWorkflows_RejectsAPublishedDescriptionLiteral covers
// the operator-facing half of the same finding. A hard-coded description is
// the #6189 overclaim in a different argument: "A required gate failed" on a
// head where nothing failed. Observation catches it as sameness -- one
// sentence for outcomes that mean different things.
func TestCheckRequiredStatusWorkflows_RejectsAPublishedDescriptionLiteral(t *testing.T) {
	t.Parallel()

	body := strings.Replace(realShapedPublisher(t, "", cancelledArm),
		`-f description="${description}"`, `-f description='A required gate failed'`, 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a hard-coded -f description= argument must be rejected")
	}
	requireErrorContaining(t, errs, "describes await exit codes")
}

// TestCheckRequiredStatusWorkflows_RejectsADroppedDescription pins the
// tightening this mechanism required. A cancelled gate and a broken
// aggregation both publish state=error, so with no description nothing
// separates them and deleting the cancelled arm becomes invisible.
func TestCheckRequiredStatusWorkflows_RejectsADroppedDescription(t *testing.T) {
	t.Parallel()

	body := strings.Replace(realShapedPublisher(t, "", cancelledArm),
		` -f description="${description}"`, ``, 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a publisher that posts no description must be rejected; the description is what separates " +
			"a cancelled gate from a broken aggregation, since both publish state=error")
	}
	requireErrorContaining(t, errs, "posts no description for await exit code")
}

// TestCheckRequiredStatusWorkflows_RejectsCancelledGateMappedToFailure is
// #6189 itself: a cancelled dependency reported as "A required gate failed"
// when no gate failed.
func TestCheckRequiredStatusWorkflows_RejectsCancelledGateMappedToFailure(t *testing.T) {
	t.Parallel()

	body := realShapedPublisher(t, "",
		"              13) state=failure; description='A required gate failed' ;;\n")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("mapping a cancelled gate to state=failure must be rejected; a cancelled gate is not a failed gate")
	}
	requireErrorContaining(t, errs, `posts state="failure" for AGGREGATE_CODE=13`)
}

// TestCheckRequiredStatusWorkflows_RejectsAFailureArmBehindAComment is round
// 3's H-1 seed for the arm locator: a comment carrying "(13)" moved a
// substring anchor onto itself, and the region it then read already held the
// PENDING_OUTCOME guard's `state=error`, so the assertion passed on the wrong
// assignment. Execution never reads a region.
func TestCheckRequiredStatusWorkflows_RejectsAFailureArmBehindAComment(t *testing.T) {
	t.Parallel()

	body := realShapedPublisher(t,
		"          # the cancelled-gate outcome (13) is not a gate failure\n",
		"              13) state=failure; description='A required gate failed' ;;\n")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a cancelled arm mapped to failure must be rejected even when a comment above it names the code")
	}
	requireErrorContaining(t, errs, `posts state="failure" for AGGREGATE_CODE=13`)
}

// TestCheckRequiredStatusWorkflows_RejectsADeletedArmBehindAComment is round
// 3's deletion seed. With the arm gone the code falls through to `*)`, which
// publishes the same state=error -- so state alone cannot see it. What
// catches it is that the cancelled outcome now describes itself exactly as
// the unclassified one does, which is what "the arm is gone" looks like from
// outside. The operator cost is real: that description blames the aggregator,
// sending someone hunting a broken aggregator at 3 AM when the repair is
// `gh run rerun`.
func TestCheckRequiredStatusWorkflows_RejectsADeletedArmBehindAComment(t *testing.T) {
	t.Parallel()

	body := realShapedPublisher(t,
		"          # a cancelled gate (13) publishes error, not failure\n", "")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("deleting the cancelled-gate arm must be rejected even when a comment above it names the code")
	}
	requireErrorContaining(t, errs, "describes await exit codes [13 97] identically")
}

// TestCheckRequiredStatusWorkflows_RejectsACancelledArmOverwrittenAfterAssignment
// is the codex reviewer's seed from round 2: the arm assigns the right state
// and then overwrites it before `;;`. The last assignment is what bash keeps,
// and the published value is what this reads.
func TestCheckRequiredStatusWorkflows_RejectsACancelledArmOverwrittenAfterAssignment(t *testing.T) {
	t.Parallel()

	body := realShapedPublisher(t, "",
		"              13) state=error; state=success; description='A required gate was cancelled' ;;\n")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("an arm that reassigns state after the correct assignment must be rejected")
	}
	requireErrorContaining(t, errs, `posts state="success" for AGGREGATE_CODE=13`)
}

// TestCheckRequiredStatusWorkflows_RejectsACancelledArmFailureBehindAQuotedDescription
// is round 3's quoting seed: a description quoting the code and a `;` keeps a
// textual scanner busy while the arm underneath maps a cancellation to red.
func TestCheckRequiredStatusWorkflows_RejectsACancelledArmFailureBehindAQuotedDescription(t *testing.T) {
	t.Parallel()

	body := realShapedPublisher(t, "",
		"              13) description='cancelled (13); re-run it #6218 now'; state=failure ;;\n")
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("a cancelled arm mapped to failure behind a quoted description must be rejected")
	}
	requireErrorContaining(t, errs, `posts state="failure" for AGGREGATE_CODE=13`)
}

// TestCheckRequiredStatusWorkflows_RejectsAStillRunningArmThatPublishes is
// #6075's core: gates that have not finished must publish nothing at all.
// Another aggregate run for this head is already queued behind the workflows
// still going, and that one is authoritative.
func TestCheckRequiredStatusWorkflows_RejectsAStillRunningArmThatPublishes(t *testing.T) {
	t.Parallel()

	body := strings.Replace(realShapedPublisher(t, "", cancelledArm),
		"              11) exit 0 ;;\n",
		"              11) state=failure; description='A required gate failed' ;;\n", 1)
	errs := checkRequiredStatusWorkflows(writeRequiredWorkflowFixture(t, body), requiredWorkflowRegistry())
	if len(errs) == 0 {
		t.Fatal("an unfinished run must not publish a terminal status")
	}
	requireErrorContaining(t, errs, `posts state="failure" for AGGREGATE_CODE=11`)
}

// TestCheckRequiredStatusWorkflows_AcceptsTheRepositorysOwnPublisher runs the
// contract against the workflow this repository actually ships, not a
// fixture. It is the reason the registry gate catches a workflow-only edit:
// `.github/workflows/**` is one of the ci-gates-registry gate's triggers, so
// a change to required-gates.yml selects this check even when no Go file
// moved.
func TestCheckRequiredStatusWorkflows_AcceptsTheRepositorysOwnPublisher(t *testing.T) {
	t.Parallel()

	run, err := TerminalPublisherRun(repositoryRequiredGatesWorkflow(t))
	if err != nil {
		t.Fatalf("locate the repository's terminal publisher: %v", err)
	}
	step := requiredWorkflowStep{Run: run}
	check := RequiredStatusCheck{Context: "required-gates-complete"}
	if errs := validatePublishedOutcomes(step, check); len(errs) != 0 {
		t.Fatalf("the repository's own publisher must satisfy its published-status contract, got %v", errs)
	}
}

// TestPublisherProbeCodeIsNotARealAwaitExitCode keeps the unclassified probe
// unclassified. If it ever collided with a real await exit code, the `*)` arm
// would stop being exercised and the arm an operator meets when something
// unanticipated happens would go unchecked.
func TestPublisherProbeCodeIsNotARealAwaitExitCode(t *testing.T) {
	t.Parallel()

	for _, code := range []int{
		awaitExitPassedCode,
		awaitExitGateFailedCode,
		awaitExitStillRunningCode,
		awaitExitBrokenCode,
		awaitExitGateCancelledCode,
	} {
		if publisherUnclassifiedProbeCode == code {
			t.Fatalf("publisherUnclassifiedProbeCode %d collides with a real await exit code",
				publisherUnclassifiedProbeCode)
		}
	}
}

// repositoryRoot returns the working tree root, so a test can read a
// committed file without depending on where `go test` was invoked from.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse --show-toplevel: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// writeWorkflowFile writes a workflow body under root and returns its path.
func writeWorkflowFile(t *testing.T, root, body string) string {
	t.Helper()
	dir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "required-gates.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// repositoryRequiredGatesWorkflow returns the committed required-gates
// workflow path.
func repositoryRequiredGatesWorkflow(t *testing.T) string {
	t.Helper()
	return filepath.Join(repositoryRoot(t), ".github", "workflows", "required-gates.yml")
}
