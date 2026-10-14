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

// coverage is the walk state of the covered-job computation. repoRoot is the
// repository root the cross-file script-body check resolves scripts/ callees
// against.
type coverage struct {
	repoRoot string
	jobs     map[string]ownershipJob
	covered  map[string]bool
	visited  map[string]bool
}

func newCoverage(repoRoot string, jobs map[string]ownershipJob) *coverage {
	return &coverage{
		repoRoot: repoRoot,
		jobs:     jobs,
		covered:  make(map[string]bool, len(jobs)),
		visited:  make(map[string]bool, len(jobs)),
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
		if covers && !toleratesFailure(job) && readsNeedResult(job.If, job.text, need, c.repoRoot) {
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
// or needs.<need>.outcome: the reference sits in a non-tautological
// job-level `if:`, or in a value a later shell test branches on (#7882, #8014).
//
// A bare text match is not enough: `echo "${{ needs.dep.result }}"` or a grep
// pattern mentions the reference without gating, and the dependency would read
// as covered while nothing fails when it does. Each leg below is the narrowest
// shape that still covers the real aggregators:
//
//   - the job-level if: mentions the reference, bare or in ${{ }}, and no
//     top-level `||` disjunct beside the reference reads true on its own (a
//     false job if: skips the job, and a skip publishes failure). Anchoring
//     this leg to ${{ }} would turn the legal bare spelling
//     docker-publish.yml uses (`needs.build-and-push-image.result ==
//     'success'`) into a false red.
//   - a shell line opens with test, [, or [[ and holds the reference
//     (`run: test "${{ needs.dep.result }}" = success`). The keyword must open
//     the line: `echo "test: ${{ needs.dep.result }}"` names a test without
//     running one. A failing test fails the step, so no body is needed.
//   - a shell line opens with if or case and holds the reference, and a
//     failing body (exit with a nonzero literal, or false) follows within a
//     bounded lookahead to the closing fi/esac (test.yml go-race-complete:
//     `if [ "${result}" != "success" ]; then ... exit 1; fi`). An `if` whose
//     body only echoes cannot fail the job, so it does not gate.
//   - a shell assignment captures the reference and a later shell-test line
//     branches on the variable (test.yml go-race-complete:
//     `result="${{ needs.go-race.result }}"`, then
//     `if [ "${result}" != "success" ]` with an exit 1 body).
//   - a YAML env value holds the reference and a later shell-test line
//     branches on the variable (live-backend-tests.yml: `SHARD_RESULT` plus
//     `case "$RUN_LIVE:$SHARD_RESULT" in` with a failing arm). The env key
//     and value are separate scalars, so the name line must immediately
//     precede a bare ${{ }} value line; only UPPER_SNAKE names bind, which
//     keeps the `if`, `run`, and `name` mapping keys from binding.
//   - a repo-script invocation takes the reference as an argument and the
//     resolved scripts/ callee gates on its positional slot
//     (golden-corpus-gate.yml differential passes
//     `"${{ needs.differential-capture.result }}"` to
//     scripts/lib/stage-differential-capture.sh, which carries $5 into
//     capture_result and exits 1 unless it is success). The line must open
//     with an interpreter or a path and name a scripts/ callee, so an echo
//     of a script path does not count.
//
// Narrowings, all fail-closed: a test chained after `;`, `&&`, or `||` on one
// line, a branch on a lower-case env name, a callee spelled outside scripts/,
// a callee that is missing, non-shell, or unresolvable, a script that takes
// the reference and ignores it, a tautological job if:, an if/case without a
// failing body, and the inverted step-if-plus-exit idiom each read as
// uncovered, so the author restructures or adds an owner row. A step-level
// if: never counts: skipping one step leaves the job green.
func readsNeedResult(ifExpr, text, need, repoRoot string) bool {
	if mentionsNeedResult(ifExpr, need) {
		return true
	}
	return gatesNeedResult(text, need, repoRoot)
}

// mentionsNeedResult reports whether the job-level if: references
// needs.<need>.result or needs.<need>.outcome, bare or in ${{ }}, without a
// tautological escape: when a top-level `||` disjunct beside the reference
// reads true under a failed-dependency status (always(), failure(),
// `!cancelled()`), the job runs whatever the reference holds, so the mention
// does not gate (#8014).
func mentionsNeedResult(ifExpr, need string) bool {
	refRE := regexp.MustCompile(`needs\.` + regexp.QuoteMeta(need) + `\.(result|outcome)\b`)
	if !refRE.MatchString(ifExpr) {
		return false
	}
	return !tautologicalWithoutRef(ifExpr, refRE)
}

// tautologicalWithoutRef splits the expression on top-level `||` and reports
// whether a disjunct that does not hold the reference reads true under a
// failed-dependency status. Anything but a provably false disjunct
// (success(), cancelled(), a false literal) fails closed: an unevaluable
// condition beside the reference may run the job, so the reference does not
// gate it.
func tautologicalWithoutRef(ifExpr string, refRE *regexp.Regexp) bool {
	expr := strings.TrimSpace(ifExpr)
	if strings.HasPrefix(expr, "${{") && strings.HasSuffix(expr, "}}") {
		expr = strings.TrimSpace(expr[len("${{") : len(expr)-len("}}")])
	}
	for _, disjunct := range splitTopLevelOr(expr) {
		if refRE.MatchString(disjunct) {
			continue
		}
		if !provablyFalseUnderFailure(disjunct) {
			return true
		}
	}
	return false
}

// splitTopLevelOr splits an expression on `||` outside parentheses and
// quotes. Line-based only: no expression grammar beyond depth counting.
func splitTopLevelOr(expr string) []string {
	var parts []string
	depth := 0
	start := 0
	inSingle := false
	inDouble := false
	for i := 0; i < len(expr); i++ {
		c := expr[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case inSingle || inDouble:
		case c == '(':
			depth++
		case c == ')' && depth > 0:
			depth--
		case c == '|' && depth == 0 && i+1 < len(expr) && expr[i+1] == '|':
			parts = append(parts, expr[start:i])
			i++
			start = i + 1
		}
	}
	return append(parts, expr[start:])
}

// successCallRE and cancelledCallRE match the status calls that read false
// under a failed-dependency status: the dependency neither succeeded nor was
// cancelled.
var (
	successCallRE   = regexp.MustCompile(`(?i)^success\s*\(\s*\)$`)
	cancelledCallRE = regexp.MustCompile(`(?i)^cancelled\s*\(\s*\)$`)
)

// provablyFalseUnderFailure reports whether a non-reference disjunct reads
// false under a failed-dependency status, so the job still needs the
// reference to run. always(), failure(), and `!cancelled()` read true there;
// anything else the check cannot evaluate fails closed.
func provablyFalseUnderFailure(disjunct string) bool {
	s := stripOuterParens(disjunct)
	if strings.EqualFold(s, "false") {
		return true
	}
	return successCallRE.MatchString(s) || cancelledCallRE.MatchString(s)
}

// stripOuterParens removes balanced wrapping parentheses, so `(success())`
// evaluates like `success()` while `(a) && (b)` is left alone.
func stripOuterParens(s string) string {
	for {
		s = strings.TrimSpace(s)
		if len(s) < 2 || s[0] != '(' || s[len(s)-1] != ')' {
			return s
		}
		depth := 0
		wraps := false
		for i := 0; i < len(s); i++ {
			switch s[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					wraps = i == len(s)-1
				}
			}
			if depth == 0 {
				break
			}
		}
		if !wraps {
			return s
		}
		s = s[1 : len(s)-1]
	}
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
// to a later shell-test line with a failing body, or a repo-script invocation
// whose resolved callee gates on its positional slot.
func gatesNeedResult(text, need, repoRoot string) bool {
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
			if isBranchLead(line) {
				if hasFailingBody(lines, i) {
					return true
				}
				continue
			}
			return true
		}
		if isScriptGateLine(line) {
			if scriptGatesOnArg(repoRoot, line) {
				return true
			}
			continue
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
		for j, later := range lines[i+1:] {
			if shellGatingLeadRE.MatchString(later) && refRE.MatchString(later) {
				if isBranchLead(later) {
					if hasFailingBody(lines, i+1+j) {
						return true
					}
					continue
				}
				return true
			}
		}
	}
	return false
}
