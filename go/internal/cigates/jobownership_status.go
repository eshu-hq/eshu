// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"regexp"
	"strings"
)

// statusFunctionRE matches the status check functions that always disable the
// implicit success() of a job `if:`. GitHub expression function names are
// case-insensitive.
var statusFunctionRE = regexp.MustCompile(`(?i)\b(always|cancelled|failure)\s*\(`)

// successFunctionRE matches an explicit success() call.
var successFunctionRE = regexp.MustCompile(`(?i)\bsuccess\s*\(`)

// negationRE matches a logical not, `!`, that is not part of `!=`.
var negationRE = regexp.MustCompile(`!([^=]|$)`)

// overridesStatus reports whether a job-level `if:` runs the job after a failed
// or skipped dependency, which is the case when it disables the implicit
// success().
//
// GitHub applies a default success() to a job `if:` "unless you include one of
// these functions" (docs.github.com, Contexts and expressions, "Status check
// functions": success, always, cancelled, failure). So:
//   - always(), cancelled() and failure() override it, alone or combined, and
//     so does the documented `!cancelled()` alternative to always();
//   - an explicit success() replaces the implicit one, so `success() && x`
//     keeps the dependency gate, but `success() || x`, `success() || failure()`
//     and `!success()` run after a failed dependency;
//   - an `if:` with no status function gets success() prepended, whatever else
//     it holds.
//
// The check errs toward "override": an explicit success() next to any `||` or
// `!` counts as an override even where the logic would still require success.
// Over-reporting only asks the author for an owner row or an explicit result
// read; under-reporting would hide an unblocked job.
func overridesStatus(ifExpr string) bool {
	if statusFunctionRE.MatchString(ifExpr) {
		return true
	}
	if !successFunctionRE.MatchString(ifExpr) {
		return false
	}
	return strings.Contains(ifExpr, "||") || negationRE.MatchString(ifExpr)
}

// coverage is the walk state of the covered-job computation.
type coverage struct {
	jobs    map[string]ownershipJob
	covered map[string]bool
	visited map[string]bool
}

func newCoverage(jobs map[string]ownershipJob) *coverage {
	return &coverage{
		jobs:    jobs,
		covered: make(map[string]bool, len(jobs)),
		visited: make(map[string]bool, len(jobs)),
	}
}

// own marks key covered because a gate row owns it, then follows its needs.
func (c *coverage) own(key string) {
	c.covered[key] = true
	c.walk(key)
}

// walk follows the needs of key and marks the ones it covers.
//
// A dependent with the implicit success() is skipped when a dependency fails,
// and a skip publishes failure, so every dependency stays covered and the walk
// continues through it.
//
// A dependent whose `if:` overrides the status (overridesStatus) runs anyway,
// so a dependency is covered only when the job reads its result or outcome. The
// walk stops there and does not follow the dependency's own needs: a failure
// further back makes the dependency `skipped`, and an aggregator that accepts
// `skipped` (the docs-only skip pattern) goes green. The `changes` dependency
// that .github/workflows/test.yml adds to go-race-complete exists for this
// reason. A dependency of a dependency is covered only when something else
// covers it, such as the aggregator reading it directly.
//
// A dependency with a job-level continue-on-error is never covered through an
// edge: a failing continue-on-error job passes, so neither the dependent nor
// the run turns red. A gate row owning it records the decision but cannot make
// it block (frontend.yml `format` is the deliberate case). Its own dependencies
// stay covered through an implicit-success edge, because their failure skips it
// and so skips the dependent.
//
// A continue-on-error job that overrides the status covers nothing through its
// result reads: it runs after a failed dependency, and its own failure passes.
func (c *coverage) walk(key string) {
	if c.visited[key] {
		return
	}
	c.visited[key] = true
	job := c.jobs[key]
	override := overridesStatus(job.If)
	for _, need := range jobNeeds(job.Needs) {
		dep, ok := c.jobs[need]
		if !ok {
			continue
		}
		covers := !toleratesFailure(dep)
		if !override {
			if covers {
				c.covered[need] = true
			}
			c.walk(need)
			continue
		}
		if covers && !toleratesFailure(job) && readsNeedResult(job.text, need) {
			c.covered[need] = true
		}
	}
}

// toleratesFailure reports whether a failure of job can leave the run green:
// the job sets continue-on-error to anything but a literal false, including an
// expression the check cannot evaluate.
func toleratesFailure(job ownershipJob) bool {
	value := strings.TrimSpace(job.ContinueOnError)
	return value != "" && !strings.EqualFold(value, "false")
}

// readsNeedResult reports whether text references needs.<need>.result or
// needs.<need>.outcome, the expressions an aggregator uses to fail on a
// dependency.
func readsNeedResult(text, need string) bool {
	return regexp.MustCompile(`needs\.` + regexp.QuoteMeta(need) + `\.(result|outcome)\b`).MatchString(text)
}
