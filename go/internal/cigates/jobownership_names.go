// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"regexp"
	"strings"
)

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
