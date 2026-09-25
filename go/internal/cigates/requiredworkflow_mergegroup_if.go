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

	"gopkg.in/yaml.v3"
)

// mergeGroupJob is the slice of a workflow job the merge_group run check reads.
type mergeGroupJob struct {
	Name  string    `yaml:"name"`
	If    string    `yaml:"if"`
	Needs yaml.Node `yaml:"needs"`
}

// validateBlockingJobsRunOnMergeGroup reports every blocking gate whose CI job
// can be skipped on a merge_group run.
//
// A queue entry whose compare reaches the API's 300-file cap selects EVERY
// blocking gate (see ci-gates await). A selected job GitHub reports SKIPPED,
// in a run that was not cancelled, publishes `failure` -- correctly, for a
// pull request, where a skip the workflow chose is a disagreement with the
// registry. On the queue it is a false red for a gate that simply does not
// apply. So every blocking job must provably run on merge_group: its own
// `if:` must evaluate true with github.event_name == 'merge_group' (any other
// context is unknown and never assumed true), and, unless that `if:` calls
// always() or cancelled(), every job it needs must provably run too. A job
// that only sometimes has work to do runs anyway and decides per step.
func validateBlockingJobsRunOnMergeGroup(repoRoot string, check RequiredStatusCheck, reg *Registry) []error {
	wfDir := filepath.Join(repoRoot, ".github", "workflows")
	cache := make(map[string]map[string]mergeGroupJob)
	type skippable struct {
		reason string
		gates  []string
	}
	found := make(map[string]*skippable) // "workflow/job" -> reason and gate IDs
	for _, gate := range reg.Gates {
		if !gate.Blocking || gate.CI.Workflow == "" || gate.CI.Job == "" {
			continue
		}
		jobs, cached := cache[gate.CI.Workflow]
		if !cached {
			raw, err := os.ReadFile(filepath.Join(wfDir, filepath.Base(gate.CI.Workflow))) // #nosec G304 -- basename-confined workflow from committed registry
			if err == nil {
				var wf struct {
					Jobs map[string]mergeGroupJob `yaml:"jobs"`
				}
				if yaml.Unmarshal(raw, &wf) == nil {
					jobs = wf.Jobs
				}
			}
			cache[gate.CI.Workflow] = jobs
		}
		if jobs == nil {
			continue // unreadable workflows are reported by the completeness checks
		}
		for _, key := range resolveMergeGroupJobs(jobs, gate.CI.Job) {
			reason := mergeGroupSkipReason(jobs, key, map[string]bool{})
			if reason == "" {
				continue
			}
			id := gate.CI.Workflow + "/" + key
			if found[id] == nil {
				found[id] = &skippable{reason: reason}
			}
			found[id].gates = append(found[id].gates, gate.ID)
		}
	}
	ids := make([]string, 0, len(found))
	for id := range found {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	errs := make([]error, 0, len(ids))
	for _, id := range ids {
		errs = append(errs, fmt.Errorf(
			"required status context %q: blocking job %s can be skipped on merge_group (%s; gates %s); "+
				"a truncated queue selection would publish failure for it -- make the job run on "+
				"merge_group and gate its steps instead",
			check.Context, id, found[id].reason, strings.Join(found[id].gates, ","),
		))
	}
	return errs
}

var workflowExpressionRE = regexp.MustCompile(`\$\{\{.*?\}\}`)

// resolveMergeGroupJobs maps a registry ci.job (a job key, a static job name,
// or a matrix/append_gate display) to the workflow job keys that produce it.
func resolveMergeGroupJobs(jobs map[string]mergeGroupJob, checkName string) []string {
	var direct, templated []string
	for key, job := range jobs {
		switch {
		case key == checkName || (job.Name == checkName && !strings.Contains(job.Name, "${{")):
			direct = append(direct, key)
		case strings.Contains(job.Name, "${{"):
			parts := workflowExpressionRE.Split(job.Name, -1)
			for i := range parts {
				parts[i] = regexp.QuoteMeta(parts[i])
			}
			if regexp.MustCompile("^" + strings.Join(parts, ".+") + "$").MatchString(checkName) {
				templated = append(templated, key)
			}
		}
	}
	if len(direct) > 0 {
		sort.Strings(direct)
		return direct
	}
	sort.Strings(templated)
	return templated
}

var statusOverrideRE = regexp.MustCompile(`\b(always|cancelled)\(\)`)

// mergeGroupSkipReason returns why a job may not run on merge_group, or "".
func mergeGroupSkipReason(jobs map[string]mergeGroupJob, key string, visiting map[string]bool) string {
	if visiting[key] {
		return "needs cycle at " + key
	}
	job, ok := jobs[key]
	if !ok {
		return "needs unknown job " + key
	}
	if !mergeGroupIfIsTrue(job.If) {
		return fmt.Sprintf("if: %q is not always true on merge_group", strings.TrimSpace(job.If))
	}
	if statusOverrideRE.MatchString(job.If) {
		return ""
	}
	visiting[key] = true
	defer delete(visiting, key)
	for _, need := range jobNeeds(job.Needs) {
		if reason := mergeGroupSkipReason(jobs, need, visiting); reason != "" {
			return "needs " + need + ": " + reason
		}
	}
	return ""
}

func jobNeeds(node yaml.Node) []string {
	switch node.Kind {
	case yaml.ScalarNode:
		return []string{node.Value}
	case yaml.SequenceNode:
		var needs []string
		for _, item := range node.Content {
			needs = append(needs, item.Value)
		}
		return needs
	default:
		return nil
	}
}

// mergeGroupIfIsTrue reports whether a GitHub Actions `if:` expression is
// true on every merge_group run. Only github.event_name is known; always()
// is true and cancelled() false; every other context or function is unknown,
// and unknown is never true. An empty expression is the implicit success(),
// which depends only on needs (checked by the caller).
func mergeGroupIfIsTrue(expr string) bool {
	expr = strings.TrimSpace(expr)
	if strings.HasPrefix(expr, "${{") && strings.HasSuffix(expr, "}}") {
		expr = strings.TrimSpace(expr[3 : len(expr)-2])
	}
	if expr == "" {
		return true
	}
	p := &ifParser{tokens: tokenizeIf(expr)}
	v := p.or()
	if p.pos != len(p.tokens) {
		return false // unparsed tail: not provable
	}
	return v.truth() == triTrue
}

type tri int

const (
	triFalse tri = iota
	triUnknown
	triTrue
)

type ifValue struct {
	known bool
	str   *string
	b     tri
}

func (v ifValue) truth() tri {
	if !v.known {
		return triUnknown
	}
	if v.str != nil {
		if *v.str == "" {
			return triFalse
		}
		return triTrue
	}
	return v.b
}

var ifTokenRE = regexp.MustCompile(`\s*('(?:[^']|'')*'|&&|\|\||==|!=|!|\(|\)|,|[A-Za-z_][A-Za-z0-9_.\-*]*|[0-9.]+)`)

func tokenizeIf(expr string) []string {
	var tokens []string
	rest := expr
	for strings.TrimSpace(rest) != "" {
		loc := ifTokenRE.FindStringSubmatchIndex(rest)
		if loc == nil || loc[0] != 0 {
			return append(tokens, "\x00") // unparseable: forces "not provable"
		}
		tokens = append(tokens, rest[loc[2]:loc[3]])
		rest = rest[loc[1]:]
	}
	return tokens
}

type ifParser struct {
	tokens []string
	pos    int
}

func (p *ifParser) peek() string {
	if p.pos < len(p.tokens) {
		return p.tokens[p.pos]
	}
	return ""
}

func (p *ifParser) next() string {
	t := p.peek()
	p.pos++
	return t
}

func boolValue(t tri) ifValue {
	if t == triUnknown {
		return ifValue{}
	}
	return ifValue{known: true, b: t}
}

func (p *ifParser) or() ifValue {
	left := p.and()
	for p.peek() == "||" {
		p.next()
		right := p.and()
		l, r := left.truth(), right.truth()
		switch {
		case l == triTrue || r == triTrue:
			left = boolValue(triTrue)
		case l == triFalse && r == triFalse:
			left = boolValue(triFalse)
		default:
			left = ifValue{}
		}
	}
	return left
}

func (p *ifParser) and() ifValue {
	left := p.unary()
	for p.peek() == "&&" {
		p.next()
		right := p.unary()
		l, r := left.truth(), right.truth()
		switch {
		case l == triFalse || r == triFalse:
			left = boolValue(triFalse)
		case l == triTrue && r == triTrue:
			left = boolValue(triTrue)
		default:
			left = ifValue{}
		}
	}
	return left
}

func (p *ifParser) unary() ifValue {
	if p.peek() == "!" {
		p.next()
		switch p.unary().truth() {
		case triTrue:
			return boolValue(triFalse)
		case triFalse:
			return boolValue(triTrue)
		default:
			return ifValue{}
		}
	}
	return p.comparison()
}

func (p *ifParser) comparison() ifValue {
	left := p.primary()
	if op := p.peek(); op == "==" || op == "!=" {
		p.next()
		right := p.primary()
		if left.known && right.known && left.str != nil && right.str != nil {
			equal := strings.EqualFold(*left.str, *right.str)
			if equal == (op == "==") {
				return boolValue(triTrue)
			}
			return boolValue(triFalse)
		}
		return ifValue{}
	}
	return left
}

func (p *ifParser) primary() ifValue {
	tok := p.next()
	switch {
	case tok == "(":
		v := p.or()
		if p.next() != ")" {
			p.pos = len(p.tokens) + 1 // force "not provable"
		}
		return v
	case strings.HasPrefix(tok, "'"):
		s := strings.ReplaceAll(tok[1:len(tok)-1], "''", "'")
		return ifValue{known: true, str: &s}
	case tok == "true":
		return boolValue(triTrue)
	case tok == "false":
		return boolValue(triFalse)
	case tok == "github.event_name":
		s := "merge_group"
		return ifValue{known: true, str: &s}
	}
	if p.peek() == "(" { // function call: skip its arguments
		p.next()
		for p.peek() != ")" && p.peek() != "" {
			p.or()
			if p.peek() == "," {
				p.next()
			}
		}
		p.next()
		switch tok {
		case "always":
			return boolValue(triTrue)
		case "cancelled":
			return boolValue(triFalse)
		}
	}
	return ifValue{}
}
