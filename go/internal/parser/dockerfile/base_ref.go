// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import (
	"slices"
	"strings"
)

// predefinedBuildArgs are the platform ARGs the builder supplies to every
// build. Their value depends on the build host and --platform, so a Dockerfile
// default for one of them is only a default, never the effective value.
var predefinedBuildArgs = map[string]bool{
	"TARGETPLATFORM": true, "TARGETOS": true, "TARGETARCH": true, "TARGETVARIANT": true,
	"BUILDPLATFORM": true, "BUILDOS": true, "BUILDARCH": true, "BUILDVARIANT": true,
}

// maxExpandedLen bounds one expansion. A Dockerfile is untrusted repository
// content, and ARG values that each repeat the previous one would otherwise
// grow exponentially. A value past the bound is treated as not knowable.
const maxExpandedLen = 4096

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
	r.globals[decl.name] = globalArg{
		value:      expanded.out,
		known:      len(expanded.unresolved) == 0 && !expanded.truncated && !predefinedBuildArgs[decl.name],
		rawDefault: decl.value,
		hasDefault: true,
	}
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
	// defaults holds the ARG defaults seen for unresolved variables.
	defaults map[string]string
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
			unresolved = []string{ref}
		}
	}
	if len(unresolved) == 0 {
		return baseRef{ref: expanded.out, template: ref}
	}
	unresolved = sortedUnique(unresolved)
	var defaults map[string]string
	for _, name := range unresolved {
		if global, ok := r.globals[name]; ok && global.hasDefault {
			if defaults == nil {
				defaults = map[string]string{}
			}
			defaults[name] = global.rawDefault
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
	for _, decl := range decls {
		if !decl.hasDefault || !slices.Contains(stage.UnresolvedArgs, decl.name) {
			continue
		}
		if _, seen := stage.ArgDefaults[decl.name]; seen {
			continue
		}
		if stage.ArgDefaults == nil {
			stage.ArgDefaults = map[string]string{}
		}
		stage.ArgDefaults[decl.name] = decl.value
	}
}

type expansion struct {
	out string
	// truncated is set when the expansion passed maxExpandedLen and stopped.
	truncated  bool
	unresolved []string
	referenced []string
}

type varState int

const (
	varKnown   varState = iota // value is known from a global ARG default
	varUnset                   // not declared: Dockerfile treats it as empty
	varUnknown                 // declared or predefined but supplied at build time
)

func (r *baseRefResolver) lookup(name string) (string, varState) {
	if global, ok := r.globals[name]; ok {
		if global.known {
			return global.value, varKnown
		}
		return "", varUnknown
	}
	if predefinedBuildArgs[name] {
		return "", varUnknown
	}
	return "", varUnset
}

// expand substitutes ${VAR}, $VAR, ${VAR:-word}, ${VAR-word}, ${VAR:+word} and
// ${VAR+word}. Every other form (pattern, replace or required expressions,
// command substitution, an unterminated brace) is reported as unresolved.
func (r *baseRefResolver) expand(s string) expansion {
	var result expansion
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if out.Len() > maxExpandedLen {
			result.truncated = true
			break
		}
		if s[i] != '$' {
			out.WriteByte(s[i])
			continue
		}
		i += r.expandVariable(s[i:], &result, &out) - 1
	}
	result.out = out.String()
	return result
}

// expandVariable handles the "$..." construct at the start of s and returns the
// number of bytes it consumed.
func (r *baseRefResolver) expandVariable(s string, result *expansion, out *strings.Builder) int {
	if len(s) > 1 && s[1] == '{' {
		end := matchingBrace(s)
		if end < 0 {
			result.unresolved = append(result.unresolved, s)
			out.WriteString(s)
			return len(s)
		}
		r.expandBraced(s[:end+1], s[2:end], result, out)
		return end + 1
	}
	name := leadingName(s[1:])
	if name != "" {
		r.expandNamed(name, "", "", s[:1+len(name)], result, out)
		return 1 + len(name)
	}
	text := "$"
	if len(s) > 1 && s[1] == '(' {
		if close := strings.IndexByte(s, ')'); close >= 0 {
			text = s[:close+1]
		} else {
			text = s
		}
	}
	result.unresolved = append(result.unresolved, text)
	out.WriteString(text)
	return len(text)
}

func (r *baseRefResolver) expandBraced(whole, body string, result *expansion, out *strings.Builder) {
	name := leadingName(body)
	rest := body[len(name):]
	if name == "" {
		result.unresolved = append(result.unresolved, whole)
		out.WriteString(whole)
		return
	}
	if rest == "" {
		r.expandNamed(name, "", "", whole, result, out)
		return
	}
	for _, op := range []string{":-", ":+", "-", "+"} {
		if strings.HasPrefix(rest, op) {
			r.expandNamed(name, op, rest[len(op):], whole, result, out)
			return
		}
	}
	result.unresolved = append(result.unresolved, whole)
	out.WriteString(whole)
}

func (r *baseRefResolver) expandNamed(name, op, word, whole string, result *expansion, out *strings.Builder) {
	result.referenced = append(result.referenced, name)
	value, state := r.lookup(name)
	if state == varUnknown {
		result.unresolved = append(result.unresolved, name)
		out.WriteString(whole)
		return
	}
	set := state == varKnown
	nonEmpty := set && value != ""
	useWord := func() {
		inner := r.expand(word)
		out.WriteString(inner.out)
		result.truncated = result.truncated || inner.truncated
		result.unresolved = append(result.unresolved, inner.unresolved...)
		result.referenced = append(result.referenced, inner.referenced...)
	}
	switch op {
	case "":
		if !set {
			result.unresolved = append(result.unresolved, name)
			out.WriteString(whole)
			return
		}
		out.WriteString(value)
	case ":-":
		if nonEmpty {
			out.WriteString(value)
		} else {
			useWord()
		}
	case "-":
		if set {
			out.WriteString(value)
		} else {
			useWord()
		}
	case ":+":
		if nonEmpty {
			useWord()
		}
	case "+":
		if set {
			useWord()
		}
	}
}

// matchingBrace returns the index of the "}" closing the "${" at s[0:2], or -1.
func matchingBrace(s string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '$' && i+1 < len(s) && s[i+1] == '{':
			depth++
			i++
		case s[i] == '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// leadingName returns the identifier at the start of s, or "".
func leadingName(s string) string {
	end := 0
	for end < len(s) {
		c := s[end]
		isLetter := c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
		if !isLetter && (end == 0 || c < '0' || c > '9') {
			break
		}
		end++
	}
	return s[:end]
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
}
