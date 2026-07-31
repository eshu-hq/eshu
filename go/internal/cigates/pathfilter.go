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

// appendGateKeyDisplayRE captures the dorny filter key (2nd positional arg)
// and the display name (3rd positional arg) of an append_gate call in a
// matrix-dispatch workflow such as static-contract-gates.yml:
// append_gate "<selected>" "<key>" "<display>" ...
var appendGateKeyDisplayRE = regexp.MustCompile(`append_gate\s+"[^"]*"\s+"([^"]*)"\s+"([^"]*)"`)

// pathFilterStep is the minimal shape of a workflow step needed to find a
// dorny/paths-filter step and read its `with.filters` block-scalar value.
type pathFilterStep struct {
	Uses string                 `yaml:"uses"`
	With map[string]interface{} `yaml:"with"`
}

type pathFilterJob struct {
	Name  string           `yaml:"name"`
	If    string           `yaml:"if"`
	Steps []pathFilterStep `yaml:"steps"`
}

type pathFilterWorkflowFile struct {
	Jobs map[string]pathFilterJob `yaml:"jobs"`
}

// dornyFilters returns the parsed "key -> path glob list" map from the first
// dorny/paths-filter step found in raw, or nil if no such step is present or
// its filters block does not parse. A workflow with no such step (true for
// most workflows) returns nil so callers skip it rather than false-erroring.
func dornyFilters(raw []byte) (map[string][]string, bool, string) {
	var wf pathFilterWorkflowFile
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		return nil, false, ""
	}
	for jobKey, job := range wf.Jobs {
		for _, step := range job.Steps {
			if !strings.Contains(step.Uses, "dorny/paths-filter") {
				continue
			}
			rawFilters, ok := step.With["filters"]
			if !ok {
				continue
			}
			s, ok := rawFilters.(string)
			if !ok {
				continue
			}
			var filters map[string][]string
			if err := yaml.Unmarshal([]byte(s), &filters); err != nil {
				continue
			}
			if len(filters) > 0 {
				q, _ := step.With["predicate-quantifier"].(string)
				return filters, q == "every", jobKey
			}
		}
	}
	return nil, false, ""
}

// appendGateKeysByDisplay returns two maps parsed from every append_gate call
// found in raw: display name -> dorny filter key, for every display name
// that resolves to exactly one key, and display name -> the distinct keys
// seen, for every display name two or more append_gate calls name with
// DIFFERENT keys. A plain map[display]key assignment silently keeps only the
// last-seen key when two append_gate calls share a display name but pass
// different filter keys, so every registry gate naming that display would be
// validated against whichever call happened to appear last in the workflow
// file -- an ambiguous mapping, not a real signal, that can wrongly pass or
// wrongly fail a trigger check purely depending on append_gate call order
// (#5855 review). checkPathFilterCoverage treats an ambiguous display as
// unmapped for glob-matching purposes (consistent with this file's existing
// "skip rather than guess" convention for an unresolved ci.job) but reports
// the ambiguity as a drift finding rather than silently picking one key.
func appendGateKeysByDisplay(raw []byte) (keys map[string]string, ambiguous map[string][]string) {
	seen := make(map[string]map[string]struct{})
	for _, m := range appendGateKeyDisplayRE.FindAllSubmatch(raw, -1) {
		key := string(m[1])
		display := string(m[2])
		if key == "" || display == "" {
			continue
		}
		if seen[display] == nil {
			seen[display] = make(map[string]struct{})
		}
		seen[display][key] = struct{}{}
	}

	keys = make(map[string]string)
	ambiguous = make(map[string][]string)
	for display, keySet := range seen {
		if len(keySet) > 1 {
			ks := make([]string, 0, len(keySet))
			for k := range keySet {
				ks = append(ks, k)
			}
			sort.Strings(ks)
			ambiguous[display] = ks
			continue
		}
		for k := range keySet {
			keys[display] = k
		}
	}
	return keys, ambiguous
}

// needsOutputTrueRE builds the matcher for a POSITIVE selection on a specific
// producer job's paths-filter output: `needs.<hostJob>.outputs.<key> == 'true'`.
//
// Both halves matter, and matching the output reference alone gets each wrong
// (#5546 review). Binding the producer job rejects `needs.setup.outputs.code`
// when the dorny step lives in `changes` -- that job is gated on some other
// job's output and is not path-gated by this filter at all. Requiring the
// `== 'true'` comparison rejects `== 'false'`, which selects the job when the
// paths did NOT change, so the filter does not describe when it runs. In either
// case the filter would be compared against a job it does not gate.
func needsOutputTrueRE(hostJob string) *regexp.Regexp {
	return regexp.MustCompile(`needs\.` + regexp.QuoteMeta(hostJob) +
		`\.outputs\.([A-Za-z0-9_-]+)\s*==\s*'true'`)
}

// ifGatedFilterKeys returns "job identity -> dorny filter key" for every job
// positively gated on an output of hostJob, plus the identities that resolve
// ambiguously.
//
// Both the job's YAML key and its `name:` are registered, because a registry
// gate's ci.job names one or the other: test.yml's gates use the YAML key
// ("verify-contracts"), while security-scan.yml's use the display name
// ("gosec (Go static analysis)").
//
// Two jobs sharing a `name:` but resolving to different filter keys would write
// the same map entry, and Go randomises job-map iteration, so which key
// survived would vary run to run -- a nondeterministic pass or failure. Those
// identities are returned as ambiguous instead, matching what
// appendGateKeysByDisplay already does for a duplicated append_gate display
// (#5546 review). A job whose own `if:` names two different filter outputs is
// ambiguous for the same reason.
func ifGatedFilterKeys(raw []byte, filters map[string][]string, hostJob string) (keys map[string]string, ambiguous map[string][]string) {
	keys = make(map[string]string)
	ambiguous = make(map[string][]string)
	if hostJob == "" {
		return keys, ambiguous
	}
	var wf pathFilterWorkflowFile
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		return keys, ambiguous
	}
	re := needsOutputTrueRE(hostJob)

	seen := make(map[string]map[string]struct{})
	note := func(identity, key string) {
		if identity == "" {
			return
		}
		if seen[identity] == nil {
			seen[identity] = make(map[string]struct{})
		}
		seen[identity][key] = struct{}{}
	}
	for jobKey, job := range wf.Jobs {
		if job.If == "" {
			continue
		}
		found := make(map[string]struct{})
		for _, m := range re.FindAllStringSubmatch(job.If, -1) {
			if _, ok := filters[m[1]]; ok {
				found[m[1]] = struct{}{}
			}
		}
		if len(found) != 1 {
			continue
		}
		var key string
		for k := range found {
			key = k
		}
		note(jobKey, key)
		note(job.Name, key)
	}

	for identity, keySet := range seen {
		if len(keySet) > 1 {
			ks := make([]string, 0, len(keySet))
			for k := range keySet {
				ks = append(ks, k)
			}
			sort.Strings(ks)
			ambiguous[identity] = ks
			continue
		}
		for k := range keySet {
			keys[identity] = k
		}
	}
	return keys, ambiguous
}

// matchesDornyPattern reports whether a single dorny/paths-filter pattern
// matches path, honouring a leading `!`.
//
// dorny compiles each pattern separately -- `picomatch(pattern, {dot: true})`
// -- and a file is included when ANY compiled pattern returns true (the
// default `predicate-quantifier`, "some"). picomatch treats a leading `!` as a
// negated matcher, so `!docs/**` is TRUE for a path outside docs/ and FALSE for
// one inside it.
//
// Note this is per-pattern negation, NOT gitignore's "a later ! subtracts an
// earlier match". Under "some" a `!` pattern can only ever ADD matches, never
// remove one, which is why a filter list containing a catch-all `**` makes its
// own exclusions inert (#5896). Reproducing dorny's actual rule rather than the
// intuitive one is the point: this checker must agree with what CI does, not
// with what the YAML appears to intend.
// Scope: MatchGlob implements `*` and `**` only. picomatch additionally
// supports `?`, `[...]` character classes, and `{a,b}` brace expansion, and a
// filter using those would be matched more loosely here than dorny matches it.
// No workflow in this repo uses them today, so the gap is latent rather than
// live -- recorded so a future maintainer does not read this as full picomatch
// parity (#5546 review).
func matchesDornyPattern(pattern, path string) bool {
	if negated, ok := strings.CutPrefix(pattern, "!"); ok {
		return !MatchGlob(negated, path)
	}
	return MatchGlob(pattern, path)
}

// matchesDornyFilter reports whether path is selected by a filter key's whole
// pattern list, under dorny's predicate quantifier. Default ("some") includes
// the file when ANY pattern matches; `predicate-quantifier: 'every'` requires
// ALL of them. An empty pattern list selects nothing under either mode.
func matchesDornyFilter(patterns []string, path string, every bool) bool {
	if len(patterns) == 0 {
		return false
	}
	for _, p := range patterns {
		if matchesDornyPattern(p, path) {
			if !every {
				return true
			}
			continue
		}
		if every {
			return false
		}
	}
	return every
}

// isLiteralTrigger reports whether a registry trigger names a single
// concrete file path rather than a glob (no `*` anywhere). Only a literal
// trigger has one real path to test against a CI path-filter glob with
// MatchGlob; a glob-form trigger (e.g. "go/internal/**") has no single
// concrete path, so checkPathFilterCoverage skips it rather than guessing at
// a glob-vs-glob equivalence.
func isLiteralTrigger(trigger string) bool {
	return !strings.Contains(trigger, "*")
}

// checkPathFilterCoverage validates that every LITERAL (non-glob) registry
// trigger of a gate whose CI workflow uses a dorny/paths-filter is actually
// selected by that workflow's corresponding filter key, using the semantics
// dorny itself applies (see matchesDornyFilter: per-pattern compilation, `!`
// negates one pattern, predicate-quantifier chooses ANY vs EVERY, and a single
// `*` does not cross `/`).
//
// This is the registry-vs-CI-filter cross-check for the drift class #5855's
// third review round found: specs/ci-gates.v1.yaml's triggers and the
// workflows' hand-duplicated dorny filters are two independently-edited lists
// with no prior machine link between them, so a trigger can be added to one and
// never the other, leaving CI silently unable to select the gate for a PR that
// touches only that file.
//
// A gate's filter key resolves either from an append_gate call in a
// matrix-dispatch workflow (static-contract-gates.yml) or from a job whose
// `if:` is gated on a paths-filter output, needs.<job>.outputs.<key>, which is
// the shape test.yml, security-scan.yml, and mcp-schema-drift.yml use (#5546).
// Covering only the first left 30 gates -- including the 11 blocking gates
// behind test.yml's verify-contracts -- silently unexamined.
//
// A gate whose mapping does not resolve either way, or whose workflow has no
// dorny step or does not parse, is skipped rather than false-errored: this
// check only fires where the mapping from gate to filter key is unambiguous.
func checkPathFilterCoverage(repoRoot string, reg *Registry) []error {
	wfDir := filepath.Join(repoRoot, ".github", "workflows")
	rawCache := make(map[string][]byte)
	filtersCache := make(map[string]map[string][]string)
	everyCache := make(map[string]bool)
	keysCache := make(map[string]map[string]string)
	ambiguousCache := make(map[string]map[string][]string)
	ifKeysCache := make(map[string]map[string]string)
	ifAmbiguousCache := make(map[string]map[string][]string)
	hostJobCache := make(map[string]string)

	var errs []error
	for _, g := range reg.Gates {
		if g.CI.Workflow == "" || g.CI.Job == "" {
			continue
		}
		raw, cached := rawCache[g.CI.Workflow]
		if !cached {
			// filepath.Base strips any path component so the read is confined to
			// wfDir; the filename comes from the committed ci-gates.v1.yaml
			// registry, not runtime input.
			p := filepath.Join(wfDir, filepath.Base(g.CI.Workflow))
			r, err := os.ReadFile(p) // #nosec G304 -- wfDir-confined path; filename is registry-controlled, not runtime input
			if err != nil {
				r = nil
			}
			raw = r
			rawCache[g.CI.Workflow] = raw
		}
		if raw == nil {
			// Missing/unreadable workflow is reported by checkWorkflowCompleteness;
			// do not double-report here.
			continue
		}

		filters, cached := filtersCache[g.CI.Workflow]
		every := everyCache[g.CI.Workflow]
		hostJob := hostJobCache[g.CI.Workflow]
		if !cached {
			filters, every, hostJob = dornyFilters(raw)
			filtersCache[g.CI.Workflow] = filters
			everyCache[g.CI.Workflow] = every
			hostJobCache[g.CI.Workflow] = hostJob
		}
		if filters == nil {
			// Workflow does not use the dorny/paths-filter matrix pattern.
			continue
		}

		keys, keysCached := keysCache[g.CI.Workflow]
		ambiguous, ambiguousCached := ambiguousCache[g.CI.Workflow]
		if !keysCached || !ambiguousCached {
			keys, ambiguous = appendGateKeysByDisplay(raw)
			keysCache[g.CI.Workflow] = keys
			ambiguousCache[g.CI.Workflow] = ambiguous
		}

		if ambiguousKeys, isAmbiguous := ambiguous[g.CI.Job]; isAmbiguous {
			// Two or more append_gate calls in this workflow share g.CI.Job as
			// their display name but pass DIFFERENT dorny filter keys. Report
			// the ambiguity as a drift finding rather than silently checking
			// this gate's triggers against whichever key a plain map
			// assignment happened to keep last -- that would be a guess, not
			// a signal, and could wrongly pass or wrongly fail depending on
			// append_gate call order.
			errs = append(errs, fmt.Errorf(
				"drift: gate %q's ci.job %q matches multiple append_gate calls in %q with different dorny filter keys (%s) -- give each append_gate call a distinct display name, or reconcile the keys, so the gate-to-filter-key mapping is unambiguous",
				g.ID, g.CI.Job, g.CI.Workflow, strings.Join(ambiguousKeys, ", "),
			))
			continue
		}

		key, ok := keys[g.CI.Job]
		if !ok {
			// No append_gate call names this gate's ci.job. Before skipping,
			// try the non-matrix shape: a job selected directly by an `if:` on
			// a paths-filter output (#5546). Without this, every gate in
			// test.yml / security-scan.yml / mcp-schema-drift.yml went
			// unchecked.
			ifKeys, cachedIf := ifKeysCache[g.CI.Workflow]
			ifAmbiguous := ifAmbiguousCache[g.CI.Workflow]
			if !cachedIf {
				ifKeys, ifAmbiguous = ifGatedFilterKeys(raw, filters, hostJob)
				ifKeysCache[g.CI.Workflow] = ifKeys
				ifAmbiguousCache[g.CI.Workflow] = ifAmbiguous
			}
			if ambiguousKeys, isAmbiguous := ifAmbiguous[g.CI.Job]; isAmbiguous {
				errs = append(errs, fmt.Errorf(
					"drift: gate %q's ci.job %q matches multiple if-gated jobs in %q resolving to different dorny filter keys (%s) -- give each job a distinct name, or reconcile the keys, so the gate-to-filter-key mapping is unambiguous",
					g.ID, g.CI.Job, g.CI.Workflow, strings.Join(ambiguousKeys, ", "),
				))
				continue
			}
			key, ok = ifKeys[g.CI.Job]
		}
		if !ok {
			// The mapping from gate to filter key is unresolved, so skip
			// rather than guess.
			continue
		}

		patterns, ok := filters[key]
		if !ok {
			errs = append(errs, fmt.Errorf(
				"drift: gate %q resolves to dorny filter key %q via append_gate, but %q's filters block has no %q entry",
				g.ID, key, g.CI.Workflow, key,
			))
			continue
		}

		for _, trig := range g.Triggers {
			if !isLiteralTrigger(trig) {
				continue
			}
			if !matchesDornyFilter(patterns, trig, every) {
				errs = append(errs, fmt.Errorf(
					"drift: gate %q trigger %q is not matched by any pattern in %q's %q dorny path filter "+
						"(gitignore-style glob: a single '*' does not cross '/') -- a PR touching only that file "+
						"would not select this gate's CI check; add a matching pattern to the filter or fix the trigger",
					g.ID, trig, g.CI.Workflow, key,
				))
			}
		}
	}
	return errs
}
