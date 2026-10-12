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
// so a dependency is covered only when the job gates on its result or outcome
// (readsNeedResult). The
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
		if covers && !toleratesFailure(job) && readsNeedResult(job.If, job.text, need) {
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

// readsNeedResult reports whether the dependent gates on needs.<need>.result
// or needs.<need>.outcome: the reference sits in the job-level `if:`, or in a
// value a later shell test branches on (#7882).
//
// A bare text match is not enough: `echo "${{ needs.dep.result }}"` or a grep
// pattern mentions the reference without gating, and the dependency would read
// as covered while nothing fails when it does. Each leg below is the narrowest
// shape that still covers the real aggregators:
//
//   - the job-level if: mentions the reference, bare or in ${{ }} (a false job
//     if: skips the job, and a skip publishes failure). Anchoring this leg to
//     ${{ }} would turn the legal bare spelling docker-publish.yml uses
//     (`needs.build-and-push-image.result == 'success'`) into a false red.
//   - a shell line opens with test, [, [[, if, or case and holds the reference
//     (`run: test "${{ needs.dep.result }}" = success`). The keyword must open
//     the line: `echo "test: ${{ needs.dep.result }}"` names a test without
//     running one.
//   - a shell assignment captures the reference and a later shell-test line
//     branches on the variable (test.yml go-race-complete:
//     `result="${{ needs.go-race.result }}"`, then
//     `if [ "${result}" != "success" ]`).
//   - a YAML env value holds the reference and a later shell-test line
//     branches on the variable (live-backend-tests.yml: `SHARD_RESULT` plus
//     `case "$RUN_LIVE:$SHARD_RESULT" in`). The env key and value are separate
//     scalars, so the name line must immediately precede a bare ${{ }} value
//     line; only UPPER_SNAKE names bind, which keeps the `if`, `run`, and
//     `name` mapping keys from binding.
//   - a repo-script invocation takes the reference as an argument
//     (golden-corpus-gate.yml differential passes
//     `"${{ needs.differential-capture.result }}"` to
//     scripts/lib/stage-differential-capture.sh, which exits 1 unless it is
//     success). The line must open with an interpreter or a path and name a
//     scripts/ callee, so an echo of a script path does not count.
//
// Narrowings, all fail-closed: a test chained after `;`, `&&`, or `||` on one
// line, a branch on a lower-case env name, a callee spelled outside scripts/,
// and the inverted step-if-plus-exit idiom each read as uncovered, so the
// author restructures or adds an owner row. A step-level if: never counts:
// skipping one step leaves the job green.
//
// Three residual gaps stay fail-open and review-visible, the same class as the
// trivy wiring check's downstream-mutation gap: a repo script that takes the
// reference and ignores it still covers, and an `always() || ...` job if:
// that can never be false still covers. An `if` without a failing body
// over-covers for the same reason: deciding that needs the branch body, which
// is shell parsing this check does not do.
func readsNeedResult(ifExpr, text, need string) bool {
	if mentionsNeedResult(ifExpr, need) {
		return true
	}
	return gatesNeedResult(text, need)
}

// mentionsNeedResult reports whether the job-level if: references
// needs.<need>.result or needs.<need>.outcome, bare or in ${{ }}.
func mentionsNeedResult(ifExpr, need string) bool {
	return regexp.MustCompile(`needs\.` + regexp.QuoteMeta(need) + `\.(result|outcome)\b`).MatchString(ifExpr)
}

// shellGatingLeadRE matches a shell line that opens with a branching form:
// test, the [ and [[ forms (each needs the space a real invocation has, so
// arr[0] and ${a[0]} do not match), or an if/case compound.
var shellGatingLeadRE = regexp.MustCompile(`^\s*(test\b|\[\s|\[\[\s|if\b|case\b)`)

// envNameRE matches a YAML env key as collectScalars leaves it: the bare name
// on its own line. Only UPPER_SNAKE binds, so the lowercase mapping keys (if,
// run, name, ...) never do.
var envNameRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// scriptCalleeRE matches a repo-script token: the scripts/ spelling check 8
// derives, optionally ./-anchored.
var scriptCalleeRE = regexp.MustCompile(`(?:^|\s)(?:\./)?scripts/\S+`)

// scriptLeadRE matches a shell line that invokes something: an interpreter or
// a path, after any VAR=value prefixes. `bash -c "echo ..."` opens with an
// interpreter but names no scripts/ callee, so the callee check excludes it.
var scriptLeadRE = regexp.MustCompile(`^\s*(?:[A-Za-z_][A-Za-z0-9_]*=\S+\s+)*(?:bash\b|sh\b|(?:\./)?scripts/|/)`)

// isScriptGateLine reports whether a shell line invokes a repo script: it
// opens with an interpreter or a path and names a scripts/ callee.
func isScriptGateLine(line string) bool {
	return scriptLeadRE.MatchString(line) && scriptCalleeRE.MatchString(line)
}

// gatesNeedResult reports whether the job's scalar text gates on
// needs.<need>.result or needs.<need>.outcome outside the job if: a
// shell-test line holding the reference, a shell or env variable carrying it
// to a later shell-test line, or a repo-script invocation taking it as an
// argument.
func gatesNeedResult(text, need string) bool {
	quoted := regexp.QuoteMeta(need)
	needRE := regexp.MustCompile(`needs\.` + quoted + `\.(result|outcome)\b`)
	// A bare ${{ }} value line, the shape a YAML env value has once
	// collectScalars splits it from its key.
	envValueRE := regexp.MustCompile(`^\s*\$\{\{[^}]*needs\.` + quoted + `\.(result|outcome)\b[^}]*\}\}\s*$`)
	assignRE := regexp.MustCompile(`^\s*(?:export\s+|local\s+)?([A-Za-z_][A-Za-z0-9_]*)=.*needs\.` + quoted + `\.(result|outcome)\b`)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if !needRE.MatchString(line) {
			continue
		}
		if shellGatingLeadRE.MatchString(line) {
			return true
		}
		if isScriptGateLine(line) {
			return true
		}
		varName := ""
		if m := assignRE.FindStringSubmatch(line); m != nil {
			varName = m[1]
		} else if envValueRE.MatchString(line) && i > 0 && envNameRE.MatchString(strings.TrimSpace(lines[i-1])) {
			varName = strings.TrimSpace(lines[i-1])
		}
		if varName == "" {
			continue
		}
		refRE := regexp.MustCompile(`\$` + varName + `\b|\$\{` + varName + `[}:/]`)
		for _, later := range lines[i+1:] {
			if shellGatingLeadRE.MatchString(later) && refRE.MatchString(later) {
				return true
			}
		}
	}
	return false
}
