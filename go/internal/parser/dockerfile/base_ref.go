// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// predefinedBuildArgs are the platform ARGs the builder supplies to every
// build. Their value depends on the build host and --platform. The parser
// cannot know the target platform, so it reports these as unresolved on
// purpose rather than guess.
var predefinedBuildArgs = map[string]bool{
	"TARGETPLATFORM": true, "TARGETOS": true, "TARGETARCH": true, "TARGETVARIANT": true,
	"BUILDPLATFORM": true, "BUILDOS": true, "BUILDARCH": true, "BUILDVARIANT": true,
}

// maxExpandedLen bounds one expansion. A Dockerfile is untrusted repository
// content, and ARG values that each repeat the previous one would otherwise
// grow exponentially or, when each default pads the previous value with
// literal text, linearly per ARG line. An expansion that produces more than
// this many bytes is not knowable: it is reported as unresolved, and an ARG
// default that expands past it is never stored as a known value.
const maxExpandedLen = 4096

// maxReferenceLen bounds the reference text handed to expansion (a reference
// over this many bytes is unresolved), and
// maxBraceDepth bounds how deeply "${" may nest inside it. maxExpandedLen only
// bounds the output, so without these a hostile line of nested "${A:-" openers
// costs quadratic scan work before any output is produced.
const (
	maxReferenceLen = maxExpandedLen
	maxBraceDepth   = 64
)

// The ARG default evidence on a stage is bounded in both dimensions. A raw
// default is repeated on every FROM stage that reads it, so without a bound the
// payload grows with ARGs times FROM lines: 200 ARGs of 4 KB each read by 1000
// FROM lines marshal to 800 MB from 1.7 MB of input. maxArgDefaultLen bounds one
// stored default and maxArgDefaultsPerStage bounds how many a stage carries.
// Both only trim evidence: the value a FROM resolves to never comes from the
// stored copy.
const (
	maxArgDefaultLen       = 256
	maxArgDefaultsPerStage = 16
)

// argDefaultEvidence collects the bounded ARG default evidence of one stage.
type argDefaultEvidence struct {
	values map[string]string
	// truncated names the stored defaults that were cut to maxArgDefaultLen.
	truncated []string
	// omitted is set when a default was left out because the stage already
	// held maxArgDefaultsPerStage.
	omitted bool
}

// add stores the first default seen for name, cutting it at maxArgDefaultLen on
// a rune boundary, and records the cut or the omission so the evidence says
// what it leaves out.
func (e *argDefaultEvidence) add(name, value string) {
	if _, seen := e.values[name]; seen {
		return
	}
	if len(e.values) >= maxArgDefaultsPerStage {
		e.omitted = true
		return
	}
	if len(value) > maxArgDefaultLen {
		cut := maxArgDefaultLen
		for cut > 0 && !utf8.RuneStart(value[cut]) {
			cut--
		}
		value = value[:cut]
		e.truncated = append(e.truncated, name)
	}
	if e.values == nil {
		e.values = map[string]string{}
	}
	e.values[name] = value
}

// argDecl is one name[=default] pair of an ARG instruction.
type argDecl struct {
	name       string
	value      string
	hasDefault bool
}

// globalArg is an ARG declared before the first FROM, the only scope a FROM
// reference can read.
type globalArg struct {
	value      string // effective value, valid only when known
	known      bool
	rawDefault string
	hasDefault bool
}

// baseRefResolver expands ARG references in FROM image references following
// the Dockerfile rule that only ARGs declared before the first FROM are
// visible to a FROM line. An ARG declared inside a stage is never visible to
// that stage's own FROM or to any later FROM.
type baseRefResolver struct {
	globals map[string]globalArg
}

func newBaseRefResolver() *baseRefResolver {
	return &baseRefResolver{globals: map[string]globalArg{}}
}

// declare records one global ARG, expanding its default against the ARGs
// declared before it.
func (r *baseRefResolver) declare(decl argDecl) {
	if !decl.hasDefault {
		if _, exists := r.globals[decl.name]; exists {
			return
		}
		r.globals[decl.name] = globalArg{}
		return
	}
	expanded := r.expand(decl.value)
	known := len(expanded.unresolved) == 0 && !expanded.truncated && !predefinedBuildArgs[decl.name]
	global := globalArg{known: known, rawDefault: decl.value, hasDefault: true}
	if known {
		// An unknown value is never read, so only a known one is kept.
		global.value = expanded.out
	}
	r.globals[decl.name] = global
}

// baseRef is the outcome of resolving one FROM image reference.
type baseRef struct {
	// ref is the reference to split into image and tag: the expanded form when
	// every variable resolved, otherwise the reference exactly as written.
	ref string
	// template is the reference as written, set only when it holds a variable.
	template string
	// unresolved lists the variables whose value is not known at parse time.
	unresolved []string
	// defaults holds the bounded ARG default evidence for unresolved variables.
	defaults argDefaultEvidence
}

func (r *baseRefResolver) resolve(ref string) baseRef {
	if !strings.Contains(ref, "$") {
		return baseRef{ref: ref}
	}
	expanded := r.expand(ref)
	unresolved := expanded.unresolved
	if len(unresolved) == 0 && (expanded.truncated || strings.TrimSpace(expanded.out) == "") {
		unresolved = expanded.referenced
		if len(unresolved) == 0 {
			// No variable name to list. Never copy the reference text into the
			// list: it is repository-controlled and up to maxReferenceLen bytes.
			unresolved = []string{unresolvedMalformed}
			if expanded.truncated {
				unresolved = []string{unresolvedOverBound}
			}
		}
	}
	if len(unresolved) == 0 {
		return baseRef{ref: expanded.out, template: ref}
	}
	unresolved = sortedUnique(unresolved)
	var defaults argDefaultEvidence
	for _, name := range unresolved {
		if global, ok := r.globals[name]; ok && global.hasDefault {
			defaults.add(name, global.rawDefault)
		}
	}
	return baseRef{ref: ref, template: ref, unresolved: unresolved, defaults: defaults}
}

// noteStageArgDefaults keeps the default of a stage-local ARG as evidence when
// the stage's own FROM referenced that name and could not see it.
func noteStageArgDefaults(stage *Stage, decls []argDecl) {
	if stage == nil {
		return
	}
	evidence := argDefaultEvidence{
		values:    stage.ArgDefaults,
		truncated: stage.ArgDefaultsTruncated,
		omitted:   stage.ArgDefaultsOmitted,
	}
	for _, decl := range decls {
		if !decl.hasDefault || !slices.Contains(stage.UnresolvedArgs, decl.name) {
			continue
		}
		evidence.add(decl.name, decl.value)
	}
	stage.ArgDefaults = evidence.values
	stage.ArgDefaultsTruncated = evidence.truncated
	stage.ArgDefaultsOmitted = evidence.omitted
}

func sortedUnique(values []string) []string {
	out := slices.Clone(values)
	slices.Sort(out)
	return slices.Compact(out)
}

// splitImageTag splits an image reference into repository and tag. A digest
// reference stays whole, a registry port is not a tag, and a ${...} block that
// could not be expanded is opaque so its operators are never read as a tag.
func splitImageTag(image string) (string, string) {
	if strings.Contains(image, "@") {
		return image, ""
	}
	depth, tagIndex, slashIndex := 0, -1, -1
	for i := 0; i < len(image); i++ {
		switch {
		case image[i] == '$' && i+1 < len(image) && image[i+1] == '{':
			depth++
			i++
		case image[i] == '}' && depth > 0:
			depth--
		case depth == 0 && image[i] == ':':
			tagIndex = i
		case depth == 0 && image[i] == '/':
			slashIndex = i
		}
	}
	if tagIndex < 0 || tagIndex < slashIndex {
		return image, ""
	}
	return image[:tagIndex], image[tagIndex+1:]
}

// addBaseRefEvidence adds the ARG substitution evidence to a stage payload row.
// The keys appear only when the FROM reference held a variable, so Dockerfiles
// without one keep their existing payload bytes.
func addBaseRefEvidence(row map[string]any, stage Stage) {
	addOptional(row, "base_ref_template", stage.BaseRefTemplate)
	if len(stage.UnresolvedArgs) > 0 {
		names := make([]any, 0, len(stage.UnresolvedArgs))
		for _, name := range stage.UnresolvedArgs {
			names = append(names, name)
		}
		row["base_ref_unresolved_args"] = names
	}
	if len(stage.ArgDefaults) > 0 {
		defaults := make(map[string]any, len(stage.ArgDefaults))
		for name, value := range stage.ArgDefaults {
			defaults[name] = value
		}
		row["base_ref_arg_defaults"] = defaults
	}
	if len(stage.ArgDefaultsTruncated) > 0 {
		names := make([]any, 0, len(stage.ArgDefaultsTruncated))
		for _, name := range stage.ArgDefaultsTruncated {
			names = append(names, name)
		}
		row["base_ref_arg_defaults_truncated"] = names
	}
	if stage.ArgDefaultsOmitted {
		row["base_ref_arg_defaults_omitted"] = true
	}
}
