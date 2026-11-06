// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// workflowExpressionRE splits a job name on its ${{ }} expressions so a
// template compiles to a matcher for any concrete rendering.
var workflowExpressionRE = regexp.MustCompile(`\$\{\{.*?\}\}`)

// nameResolver decides which job produces each declared check name, the way
// GitHub reports names and `ci-gates await` (exact match) waits for them.
type nameResolver struct {
	declared []string
	// claims maps a name a job produces on its own to that job's key: a literal
	// `name:`, a nameless job's key, and the key of a templated job (its
	// umbrella name). A literal name wins over a templated job's umbrella key,
	// because GitHub reports the literal job under that name.
	claims map[string]string
	// templated maps the name of a declared cell (not claimed above) to the one
	// templated job that produces it, or to "" when the owner is unknown.
	templated map[string]string
}

// newNameResolver resolves every declared name against the jobs of a workflow.
//
// A declared name no job claims is a matrix cell or a dispatch display. It is
// assigned to a templated job only when that job is the single possible
// producer:
//  1. a dispatch job's template fits it and the display is named in an
//     append_gate call (appendGateDisplayRE) in the steps of that job or of a
//     job it needs, which is where static-contract-gates.yml builds its
//     matrix; if two jobs qualify, the name is ambiguous;
//  2. otherwise exactly one template fits the name.
//
// When two or more templates fit and none names it statically, the name owns
// no job. `ci-gates await` waits for exactly one real check of that name, and
// ranking the templates (by literal length or otherwise) can hand it to a job
// that never produces it, which would leave that job's real cells unblocked
// and read as owned. Failing closed reports the jobs instead.
func newNameResolver(jobs map[string]ownershipJob, declared []string) *nameResolver {
	r := &nameResolver{
		declared:  declared,
		claims:    make(map[string]string, len(jobs)),
		templated: make(map[string]string),
	}
	for key, job := range jobs {
		if isTemplatedName(job.Name) {
			r.claims[key] = key
		}
	}
	for key, job := range jobs {
		switch {
		case job.Name == "":
			r.claims[key] = key
		case !isTemplatedName(job.Name):
			r.claims[job.Name] = key
		}
	}
	for _, name := range declared {
		if _, claimed := r.claims[name]; claimed {
			continue
		}
		r.templated[name] = singleProducer(jobs, name)
	}
	return r
}

// singleProducer returns the key of the one templated job that produces name,
// or "" when none does or more than one could.
func singleProducer(jobs map[string]ownershipJob, name string) string {
	var fitting, displaying []string
	for key, job := range jobs {
		if !isTemplatedName(job.Name) || !templateMatcher(job.Name).MatchString(name) {
			continue
		}
		fitting = append(fitting, key)
		if namesAppendGateDisplay(jobs, key, name) {
			displaying = append(displaying, key)
		}
	}
	if len(displaying) > 0 {
		fitting = displaying
	}
	if len(fitting) != 1 {
		return ""
	}
	return fitting[0]
}

// namesAppendGateDisplay reports whether an append_gate call whose display
// argument is name sits in the steps of job key or of a job it directly needs.
func namesAppendGateDisplay(jobs map[string]ownershipJob, key, name string) bool {
	sources := append([]string{key}, jobNeeds(jobs[key].Needs)...)
	for _, source := range sources {
		for _, m := range appendGateDisplayRE.FindAllStringSubmatch(jobs[source].text, -1) {
			if m[1] == name {
				return true
			}
		}
	}
	return false
}

// owns reports whether any declared check name is produced by job.
func (r *nameResolver) owns(key string, job ownershipJob) bool {
	if job.Name == "" {
		return containsString(r.declared, key)
	}
	if !isTemplatedName(job.Name) {
		return containsString(r.declared, job.Name)
	}
	if r.claims[key] == key && containsString(r.declared, key) {
		return true
	}
	for _, name := range r.declared {
		if r.templated[name] == key {
			return true
		}
	}
	return false
}

func isTemplatedName(name string) bool {
	return strings.Contains(name, "${{")
}

// templateMatcher compiles a job name holding ${{ }} expressions into a regexp
// where each expression matches any non-empty text.
func templateMatcher(name string) *regexp.Regexp {
	parts := workflowExpressionRE.Split(name, -1)
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}
	return regexp.MustCompile("^" + strings.Join(parts, ".+") + "$")
}

// jobOffCondition matches a job-level `if` that can never be true. A guard such
// as `${{ false && x }}` is deliberately not matched: it is only disabled by
// expression reading, which this check does not attempt.
var jobOffCondition = regexp.MustCompile(`^(?:\$\{\{\s*false\s*\}\}|false)$`)

// WorkflowJobDisabled reports whether every workflow job a gate's ci.job names
// is permanently off (`if: ${{ false }}` or `if: false`). It returns an error
// when the workflow cannot be read or no job matches ci.job, so a stale name is
// never read as "disabled". The job is matched by key, literal name, or a name
// template, the same ways the drift checks resolve ci.job.
func WorkflowJobDisabled(repoRoot string, gate Gate) (bool, error) {
	if gate.CI.Workflow == "" || gate.CI.Job == "" {
		return false, fmt.Errorf("gate %q has no ci.workflow and ci.job to check", gate.ID)
	}
	// filepath.Base confines the read to the workflows directory; the file name
	// comes from the committed registry.
	path := filepath.Join(repoRoot, ".github", "workflows", filepath.Base(gate.CI.Workflow))
	raw, err := os.ReadFile(path) // #nosec G304 -- workflows-dir-confined path; name is registry-controlled
	if err != nil {
		return false, fmt.Errorf("gate %q: read workflow: %w", gate.ID, err)
	}
	jobs, err := parseOwnershipJobs(raw)
	if err != nil {
		return false, fmt.Errorf("gate %q: %w", gate.ID, err)
	}
	keys := make([]string, 0, len(jobs))
	for key := range jobs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	matched := 0
	for _, key := range keys {
		job := jobs[key]
		if !jobNameMatches(key, job.Name, gate.CI.Job) {
			continue
		}
		matched++
		if !jobOffCondition.MatchString(strings.TrimSpace(job.If)) {
			return false, nil
		}
	}
	if matched == 0 {
		return false, fmt.Errorf("gate %q: no job named %q in workflow %q", gate.ID, gate.CI.Job, gate.CI.Workflow)
	}
	return true, nil
}

// jobNameMatches reports whether a workflow job (by key and name:) is the one a
// registry ci.job refers to. A name with ${{ }} expressions matches as a
// template.
func jobNameMatches(key, name, wanted string) bool {
	if wanted == key || (name != "" && wanted == name) {
		return true
	}
	return isTemplatedName(name) && templateMatcher(name).MatchString(wanted)
}

// checkDisabledMarkers is drift check 16: a gate whose row carries a disabled
// marker must name a workflow job that is really disabled. Without it a row
// could claim "off" while the job still runs, which hides a live gate from
// every consumer that trusts the marker.
func checkDisabledMarkers(repoRoot string, reg *Registry) []error {
	var errs []error
	for _, gate := range reg.Gates {
		if gate.Disabled == nil {
			continue
		}
		off, err := WorkflowJobDisabled(repoRoot, gate)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("drift: gate %q has a disabled marker but its job cannot be checked: %w", gate.ID, err))
		case !off:
			errs = append(errs, fmt.Errorf(
				"drift: gate %q has a disabled marker but job %q in workflow %q can still run; "+
					"disable the job with `if: ${{ false }}` or remove the disabled marker",
				gate.ID, gate.CI.Job, gate.CI.Workflow))
		}
	}
	return errs
}
