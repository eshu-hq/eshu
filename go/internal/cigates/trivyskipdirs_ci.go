// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// trivyStepOutputExpressionRE matches a GitHub Actions expression that is
// EXACTLY a reference to a step's output -- `${{ steps.<id>.outputs.<name> }}`
// -- and nothing else. Anchored full-string (^...$) so a value that merely
// CONTAINS the expression, e.g. `${{ steps.skipdirs.outputs.dirs }},node_modules`
// or a hard-coded literal, does not match: the append-at-the-call-site bypass
// this package's trivy checks have twice had to close (#5925) is killed for
// free by exact string equality instead of needing its own bypass-specific
// check.
var trivyStepOutputExpressionRE = regexp.MustCompile(
	`^\$\{\{\s*steps\.([A-Za-z0-9_-]+)\.outputs\.([A-Za-z0-9_-]+)\s*\}\}$`,
)

// trivyCIStep is the minimal shape needed to both find the trivy-action step
// in the trivy-fs job (Uses, With.SkipDirs) and, when its skip-dirs input
// references a step output, locate the producing step by ID and inspect its
// Run and Shell fields. Other `with` keys are intentionally not mapped:
// several of them are YAML booleans (e.g. `ignore-unfixed: true`), and
// decoding those into a string field would fail, not merely be ignored.
type trivyCIStep struct {
	ID    string `yaml:"id"`
	Uses  string `yaml:"uses"`
	Run   string `yaml:"run"`
	Shell string `yaml:"shell"`
	With  struct {
		SkipDirs string `yaml:"skip-dirs"`
	} `yaml:"with"`
}

// trivyCIWorkflowFile is the minimal shape needed to reach the trivy-fs job's
// steps in security-scan.yml.
type trivyCIWorkflowFile struct {
	Jobs map[string]struct {
		Steps []trivyCIStep `yaml:"steps"`
	} `yaml:"jobs"`
}

// checkCIWorkflowSkipDirsFromHelper validates that security-scan.yml's
// trivy-fs job's trivy-action step's skip-dirs input is exactly a
// `${{ steps.<id>.outputs.<name> }}` expression, and that the step with that
// id both `source`s scripts/lib/trivy-skip-dirs.sh -- the shared derivation
// helper -- and calls the function it defines, trivy_skip_dirs_csv, in its
// run: block, over whole-line-comment-stripped content (checkSourcedAndCalled,
// #5927 review: requiring both halves of a real invocation, not merely a
// mention of the helper's path, closes the false green where a producer step
// mentioned the helper in a string or an echo while a hard-coded value
// actually governed the emitted skip-dirs -- the shape #5925 F3 only
// partially addressed with its whole-line-comment-stripped strings.Contains
// check). It also validates that the producing step declares `shell: bash`
// -- without it, an unreadable specs file failing inside
// trivy_skip_dirs_csv's pipeline would not fail the step, the same
// pipefail-dependence security-scan.yml's own comment on this step explains.
//
// RETIRED ASSERTION (#5927 round 5): this check does NOT verify that the
// producer step's run: block writes the EXACT output key
// (outputs.<name>) the trivy-action step's skip-dirs expression
// references into $GITHUB_OUTPUT. A prior version tried to prove that with
// trivyOutputAssignmentPattern, a line-regex requiring an `echo`/`printf`
// assignment shape. Five review rounds each closed one instance of the same
// class in that one function, and round 5 proved the premise false rather
// than finding a sixth instance: the doc comment's positive-alphabet
// argument -- that `[ \t"']` closes the LEFT boundary because "a real key
// assignment can only begin where a new shell word begins" -- only holds in
// UNQUOTED shell text, but every assignment this pattern was ever asked to
// match lives INSIDE a quoted argument (`echo "dirs=${d}"`), where whitespace
// does not start a shell word. `[ \t"']` is exactly the complement
// `[^ \t"']` round 4's denylist was, rewritten from the other side, and it
// was defeated the same way: proven against real bash and end-to-end through
// this package's own drift check, `echo "my dirs=${d}" >> "$GITHUB_OUTPUT"`
// is accepted with a real key of "my dirs" (outputs.dirs stays empty),
// `echo 'a dirs=x' >> "$GITHUB_OUTPUT"` is accepted with a real key of
// "a dirs", `echo "pre"dirs=x >> "$GITHUB_OUTPUT"` is accepted with a real
// key of "predirs", and `echo "dirs=${d}" >> "${GITHUB_OUTPUT}x"` /
// `echo "dirs=${d}" >> "$GITHUB_OUTPUT.bak"` are both accepted while writing
// to a DIFFERENT file entirely. Answering "which shell word does this `=`
// belong to" needs quote and command-separator tracking -- mid-line bash
// parsing, exactly the unsound bar go/internal/cigates/AGENTS.md documents
// this package's design as existing to avoid. The assertion is retired
// rather than patched a sixth time; see AGENTS.md for the full rationale and
// the consequence this narrows the check to accepting.
//
// The trivy-fs job is read job-scoped via YAML, never regexed file-wide: the
// workflow has multiple jobs, and a whole-file scan cannot tell a skip-dirs
// input or a step id that belongs to trivy-fs from one that belongs to an
// unrelated job (#5925 review). Every failure here is reported, not skipped --
// a parity check that goes quiet when it cannot locate its subject reads as
// proof of agreement it never confirmed.
func checkCIWorkflowSkipDirsFromHelper(ciPath string) error {
	raw, err := os.ReadFile(ciPath) // #nosec G304 -- repo-relative, caller-constructed path
	if err != nil {
		return fmt.Errorf("trivy skip-dirs parity: cannot read %s: %w", trivyCIWorkflowPath, err)
	}
	var wf trivyCIWorkflowFile
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		return fmt.Errorf("trivy skip-dirs parity: cannot parse %s: %w", trivyCIWorkflowPath, err)
	}
	job, ok := wf.Jobs[trivyCIJobKey]
	if !ok {
		return fmt.Errorf(
			"trivy skip-dirs parity: %s has no %q job -- cannot locate the trivy filesystem scan's skip-dirs input",
			trivyCIWorkflowPath, trivyCIJobKey,
		)
	}

	var trivySteps []trivyCIStep
	for _, step := range job.Steps {
		if strings.HasPrefix(step.Uses, trivyActionUsesPrefix) {
			trivySteps = append(trivySteps, step)
		}
	}
	if len(trivySteps) != 1 {
		return fmt.Errorf(
			"trivy skip-dirs parity: found %d %s step(s) in %s's %q job, want exactly 1 -- "+
				"the parity check cannot tell which one governs",
			len(trivySteps), trivyActionUsesPrefix, trivyCIWorkflowPath, trivyCIJobKey,
		)
	}

	skipDirs := trivySteps[0].With.SkipDirs
	m := trivyStepOutputExpressionRE.FindStringSubmatch(skipDirs)
	if m == nil {
		return fmt.Errorf(
			`trivy skip-dirs parity: %s's %q job's %s step's skip-dirs input %q is not exactly a `+
				`"${{ steps.<id>.outputs.<name> }}" expression -- it must reference a step output alone, `+
				"not a hard-coded value or one with anything appended",
			trivyCIWorkflowPath, trivyCIJobKey, trivyActionUsesPrefix, skipDirs,
		)
	}
	producerID := m[1]

	var producers []trivyCIStep
	for _, step := range job.Steps {
		if step.ID == producerID {
			producers = append(producers, step)
		}
	}
	if len(producers) != 1 {
		return fmt.Errorf(
			"trivy skip-dirs parity: %s's %q job has %d step(s) with id %q, want exactly 1 to produce "+
				"the skip-dirs output the trivy-action step references",
			trivyCIWorkflowPath, trivyCIJobKey, len(producers), producerID,
		)
	}
	producer := producers[0]
	producerLabel := fmt.Sprintf("%s's %q job's step %q", trivyCIWorkflowPath, trivyCIJobKey, producerID)

	strippedRun := stripWholeLineComments([]byte(producer.Run))
	if err := checkSourcedAndCalled(strippedRun, producerLabel); err != nil {
		return err
	}

	// GitHub Actions' default run: shell omits `-o pipefail`
	// (`bash -e {0}`); only an explicit `shell: bash` gets
	// `bash --noprofile --norc -eo pipefail {0}`. Without it, an unreadable
	// specs file failing inside trivy_skip_dirs_csv's `sed | grep | grep |
	// paste` pipeline would not fail the step -- the pipeline's exit status
	// would be paste's 0, so `dirs="$(trivy_skip_dirs_csv .)"` would
	// silently succeed with an empty value instead of failing loudly
	// (security-scan.yml's own comment on this step explains the same
	// mechanism).
	if producer.Shell != "bash" {
		return fmt.Errorf(
			"trivy skip-dirs parity: %s does not declare `shell: bash` -- without it, GitHub Actions' "+
				"default run: shell omits `-o pipefail`, so an unreadable specs file failing inside "+
				"trivy_skip_dirs_csv's pipeline would silently produce an empty skip-dirs value instead of "+
				"failing the step",
			producerLabel,
		)
	}

	// trivyStepOutputExpressionRE also captures the output name the
	// trivy-action step references (m[2]), but this check deliberately does
	// not use it: whether the producer step's run: block writes that EXACT
	// key into $GITHUB_OUTPUT is the retired assertion documented above and
	// in AGENTS.md. Confirming the producer is sourced, called, and
	// shell:bash-declared, and that the trivy-action step references some
	// output of that same step, is as far as this check goes.

	return nil
}
