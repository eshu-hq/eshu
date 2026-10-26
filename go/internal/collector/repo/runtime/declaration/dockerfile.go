// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"sort"
	"strings"

	dockerfileparser "github.com/eshu-hq/eshu/go/internal/parser/dockerfile"
)

// extractDockerfile reads the FROM base-image tags of one Dockerfile body.
// Every stage whose tag names a version is one declaration; the tag is read
// through the Dockerfile parser's ARG substitution, so a global ARG default
// resolves before the FROM line is read and only build-time values stay
// unresolved. rejected counts tags the credential guard dropped and stages
// whose image names no attributable runtime.
func extractDockerfile(body string) ([]declaration, int) {
	metadata := dockerfileparser.RuntimeMetadata(body)
	stages := append([]dockerfileparser.Stage(nil), metadata.Stages...)
	// RuntimeMetadata sorts stages by name; emit in file order so the fact
	// stream is stable and line numbers ascend.
	sort.SliceStable(stages, func(i, j int) bool { return stages[i].LineNumber < stages[j].LineNumber })
	onLine := map[int]int{}
	var out []declaration
	rejected := 0
	for _, stage := range stages {
		tag := strings.TrimSpace(stage.BaseTag)
		if tag == "" {
			// scratch, a digest pin, a bare image and a local stage name
			// declare no tag version: absent, never guessed. A tagless
			// reference that still holds a placeholder is a counted drop
			// instead: a variable is never silently swallowed.
			if strings.Contains(stage.BaseImage, "$") || strings.Contains(stage.BaseImage, "{{") {
				rejected++
			}
			continue
		}
		runtime, tool, ok := dockerImageRuntime(stage.BaseImage)
		if !ok {
			// The image itself is a placeholder, so the tag's version has no
			// runtime to attribute it to. Dropped and counted, never guessed.
			rejected++
			continue
		}
		raw := strings.Clone(tag)
		if !safeSpec(raw) {
			rejected++
			continue
		}
		out = append(out, declaration{
			line:     stage.LineNumber,
			order:    onLine[stage.LineNumber],
			section:  sectionDockerfileFrom,
			runtime:  runtime,
			toolName: tool,
			rawSpec:  raw,
			// An unresolved stage keeps its tag as written; unparsedSpec
			// reports it unresolved when it holds a placeholder. A resolved
			// tag classifies normally.
			whole: len(stage.UnresolvedArgs) > 0,
		})
		onLine[stage.LineNumber]++
	}
	return out, rejected
}

// dockerImageRuntime returns the runtime a FROM image names: the last path
// segment, normalised like a .tool-versions tool, with the verbatim token
// when it differed. ok is false when the image is empty or itself holds an
// unexpanded placeholder, so a tag version is never attributed to a runtime
// the file does not name.
func dockerImageRuntime(image string) (runtime string, tool *string, ok bool) {
	image = strings.TrimSpace(image)
	if image == "" {
		return "", nil, false
	}
	if strings.Contains(image, "$") || strings.Contains(image, "{{") || strings.Contains(image, "}}") {
		return "", nil, false
	}
	name := image
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "@:") {
		return "", nil, false
	}
	runtime, tool = normalizeRuntime(name)
	return runtime, tool, true
}
