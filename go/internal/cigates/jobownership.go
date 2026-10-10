// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ownershipJob is the slice of a workflow job the ownership check reads.
type ownershipJob struct {
	Name  string    `yaml:"name"`
	If    string    `yaml:"if"`
	Needs yaml.Node `yaml:"needs"`
	// ContinueOnError is the raw job-level continue-on-error value ("" when
	// absent). Anything but a literal false can keep the job's failure from
	// failing the run.
	ContinueOnError string `yaml:"continue-on-error"`
	// text is every scalar of the job, joined, so an always() dependent can be
	// checked for a reference to a dependency's result. Set by parseOwnershipJobs.
	text string
}

// checkJobOwnership is drift check 13: in every workflow that at least one
// BLOCKING gate names in ci.workflow, every job must be owned or covered.
//
// Why: `ci-gates await` (go/cmd/ci-gates/await.go matchingChecks) waits only
// for checks whose NAME a registry row declares (ci.job, or each check_names
// entry). A job in a blocking-gate workflow that no row declares, and that no
// declared job needs, can fail and nothing blocks the merge. PR #7807 merged
// with the Ifa Determinism Gate "static mirror" job red in its merge group for
// exactly this reason.
//
// A job is OWNED when some gate row (blocking or not: an advisory row is an
// explicit, reasoned decision not to block) or a required_status_checks entry
// declares its check name. The declared names follow await: a row's check_names
// list when it is non-empty, else its ci.job. A job is COVERED when an owned job
// transitively needs it: if it fails, the owned job is skipped and a skip
// publishes failure (see requiredworkflow_mergegroup_if.go). A needs edge from
// a dependent whose `if:` disables the implicit success() (always(),
// cancelled(), failure(), `!cancelled()`, `success() || failure()`; see
// overridesStatus) covers only a dependency whose result the dependent reads
// (`needs.<job>.result` or `.outcome`), and does not continue through it: the
// dependent runs anyway, so only an explicit result check turns the owned check
// red, and a failure further back leaves the dependency `skipped`. This is the
// go-race-complete aggregator shape. A dependency with a job-level
// continue-on-error is never covered through an edge (a failing one passes), so
// it needs its own row, which records the decision but cannot make it block.
//
// Check-name resolution mirrors what GitHub reports and what await matches:
//   - a job with a literal `name:` is reported under that name, so the job key
//     does not own it;
//   - a job with no `name:` is reported under its key;
//   - a job whose `name:` holds a ${{ }} expression (matrix cells, append_gate
//     dispatch jobs) is owned by its key as the umbrella name, which is the
//     convention checkJobNamesResolve already accepts, or by a declared name it
//     produces. A declared name no other job claims (a literal name, a nameless
//     key, an umbrella key) is produced by a templated job only when that job
//     is the single possible producer: an append_gate call in the job or in a
//     job it needs names the display, or exactly one template fits the name.
//     When two or more templates fit and none is named statically, the name
//     owns none of them (fail closed), so a new matrix job can never take over
//     the cells of another job, and a fully templated job reads as owned only
//     through a name no other template fits.
//
// Not resolved: ownership is per job, not per matrix cell. A gate whose
// check_names list covers only some cells of a matrix job owns the whole job
// for this check, so an unlisted cell is not flagged. Reusable-workflow jobs
// (`uses:`) are treated like any other job by key. A workflow no blocking gate
// names is out of scope and is not read.
func checkJobOwnership(repoRoot string, reg *Registry) []error {
	scoped := make(map[string]struct{})
	for _, g := range reg.Gates {
		if g.Blocking && g.CI.Workflow != "" {
			scoped[g.CI.Workflow] = struct{}{}
		}
	}
	workflows := make([]string, 0, len(scoped))
	for w := range scoped {
		workflows = append(workflows, w)
	}
	sort.Strings(workflows)

	var errs []error
	for _, workflow := range workflows {
		errs = append(errs, checkWorkflowJobOwnership(repoRoot, reg, workflow)...)
	}
	return errs
}

func checkWorkflowJobOwnership(repoRoot string, reg *Registry, workflow string) []error {
	path := filepath.Join(repoRoot, ".github", "workflows", filepath.Base(workflow))
	raw, err := os.ReadFile(path) // #nosec G304 -- basename-confined workflow from the committed registry
	if err != nil {
		// A missing workflow is reported by Validate (it runs before DriftCheck).
		// An unreadable one is skipped here: no job list exists to check.
		return nil
	}
	jobs, err := parseOwnershipJobs(raw)
	if err != nil {
		return []error{fmt.Errorf(
			"drift: job ownership: workflow %q cannot be parsed (%v); an unverifiable workflow must not read as fully owned",
			workflow, err,
		)}
	}

	declared := declaredCheckNames(reg, workflow)
	names := newNameResolver(jobs, declared)
	cov := newCoverage(jobs)
	for key, job := range jobs {
		if names.owns(key, job) {
			cov.own(key)
		}
	}
	covered := cov.covered

	keys := make([]string, 0, len(jobs))
	for key := range jobs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var errs []error
	for _, key := range keys {
		if covered[key] {
			continue
		}
		checkName := ownershipCheckName(key, jobs[key])
		errs = append(errs, fmt.Errorf(
			"drift: job ownership: job %q (check name %q) in workflow %q is owned by no gate and is not "+
				"needed by an owned job, so its failure would not block a merge; add a registry row with "+
				"ci.workflow %s and ci.job %q (set blocking: false with a reason only for a deliberate advisory job)",
			key, checkName, workflow, workflow, checkName,
		))
	}
	return errs
}

// parseOwnershipJobs decodes the jobs of a workflow file. Each job is decoded
// from its own node so the scalar text of the whole job stays available.
func parseOwnershipJobs(raw []byte) (map[string]ownershipJob, error) {
	var wf struct {
		Jobs map[string]yaml.Node `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		return nil, fmt.Errorf("decode workflow jobs: %w", err)
	}
	jobs := make(map[string]ownershipJob, len(wf.Jobs))
	for key, node := range wf.Jobs {
		var job ownershipJob
		if err := node.Decode(&job); err != nil {
			return nil, fmt.Errorf("job %q: %w", key, err)
		}
		var text strings.Builder
		collectScalars(&node, &text)
		job.text = text.String()
		jobs[key] = job
	}
	return jobs, nil
}

func collectScalars(node *yaml.Node, out *strings.Builder) {
	if node.Kind == yaml.ScalarNode {
		out.WriteString(node.Value)
		out.WriteByte('\n')
	}
	for _, child := range node.Content {
		collectScalars(child, out)
	}
}

// declaredCheckNames returns every check name a gate row or a required status
// context declares for workflow, with the precedence `ci-gates await` applies
// (go/cmd/ci-gates/await.go resolveRequiredGateWorkflows): a row with a
// non-empty check_names list waits for those names only and ignores ci.job.
func declaredCheckNames(reg *Registry, workflow string) []string {
	var names []string
	for _, g := range reg.Gates {
		if g.CI.Workflow != workflow {
			continue
		}
		if len(g.CI.CheckNames) > 0 {
			names = append(names, g.CI.CheckNames...)
		} else if g.CI.Job != "" {
			names = append(names, g.CI.Job)
		}
	}
	for _, check := range reg.RequiredStatusChecks {
		if check.Workflow == workflow && check.Job != "" {
			names = append(names, check.Job)
		}
	}
	return names
}

// ownershipCheckName is the name a finding tells the author to put in ci.job.
func ownershipCheckName(key string, job ownershipJob) string {
	if job.Name == "" || strings.Contains(job.Name, "${{") {
		return key
	}
	return job.Name
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
