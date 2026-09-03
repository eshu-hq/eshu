// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// dockerfileScratchBaseImage is Docker's reserved empty base. A scratch stage
// has no ancestor image, so it can never anchor a DERIVED_FROM edge.
const dockerfileScratchBaseImage = "scratch"

// dockerfileRuntimeBaseImageRef resolves the effective base image reference of
// the runtime image a Dockerfile produces, from the dockerfile_stages payload
// bucket the Dockerfile parser emits (go/internal/parser/dockerfile/metadata.go).
//
// Only the FINAL stage's base is returned (#5460). A multi-stage build's
// intermediate builder stages do not contribute their base OS to the runtime
// image -- only the artifacts an explicit COPY --from names cross the stage
// boundary -- so projecting a builder stage's base as runtime lineage would
// claim the runtime image inherits CVEs it does not actually ship.
//
// A final stage whose FROM names an earlier stage's alias resolves transitively
// to that stage's own base, matching Docker's own resolution order: a bare FROM
// name is matched against earlier stage aliases first and otherwise falls back
// to an implicit registry image. A tagged or digested reference is never a
// stage alias, because Docker stage names carry neither.
//
// It reports false when no concrete ancestor exists: an empty stage list, a
// scratch base, or a reference the parser could not resolve to a literal image
// (an ARG-parameterized FROM is stored unexpanded, e.g. "${BASE_IMAGE}"). Those
// stay unresolved rather than becoming a guessed edge -- the #5460 acceptance
// contract that ambiguous input stays ambiguous.
func dockerfileRuntimeBaseImageRef(stages []map[string]any) (string, bool) {
	if len(stages) == 0 {
		return "", false
	}

	byAlias := make(map[string]map[string]any, len(stages))
	final := stages[0]
	for _, stage := range stages {
		if alias := dockerfileStageAlias(stage); alias != "" {
			byAlias[alias] = stage
		}
		if dockerfileStageIndex(stage) >= dockerfileStageIndex(final) {
			final = stage
		}
	}

	// Bounded by the stage count: each hop consumes one distinct alias, and a
	// stage alias may only name an earlier stage, so the chain cannot revisit a
	// stage. The visited set is defense in depth against a malformed payload
	// (hand-authored fixture, future parser change) rather than a reachable
	// Dockerfile shape.
	visited := make(map[string]struct{}, len(stages))
	for stage := final; stage != nil; {
		ref, ok := dockerfileStageBaseRef(stage)
		if !ok {
			return "", false
		}
		next, isAlias := byAlias[ref]
		if !isAlias {
			return ref, true
		}
		if _, seen := visited[ref]; seen {
			return "", false
		}
		visited[ref] = struct{}{}
		stage = next
	}
	return "", false
}

// dockerfileStageBaseRef rejoins one stage's parsed base_image and base_tag
// into the reference form the container image identity classifier consumes.
// The parser splits a tag off into base_tag but leaves a digest reference whole
// in base_image (splitImageTag), so an empty base_tag is not a missing tag --
// it is either a digest reference, a bare stage alias, or an unqualified image.
//
// It reports false for a base the parser could not resolve to a literal image
// reference: an empty base, the reserved scratch base, or an unexpanded build
// argument (a FROM whose image is "${BASE_IMAGE}" or "$BASE_IMAGE" is stored
// verbatim, since the parser does not evaluate ARG defaults).
func dockerfileStageBaseRef(stage map[string]any) (string, bool) {
	image := strings.TrimSpace(payloadcore.PayloadStr(stage, "base_image"))
	if image == "" || strings.Contains(image, "$") {
		return "", false
	}
	if strings.EqualFold(image, dockerfileScratchBaseImage) {
		return "", false
	}
	if tag := strings.TrimSpace(payloadcore.PayloadStr(stage, "base_tag")); tag != "" {
		return image + ":" + tag, true
	}
	return image, true
}

// dockerfileStageAlias returns a stage's AS alias, or an empty string when the
// stage is unnamed.
func dockerfileStageAlias(stage map[string]any) string {
	return strings.TrimSpace(payloadcore.PayloadStr(stage, "alias"))
}

// dockerfileStageIndex returns a stage's zero-based position in its Dockerfile.
// The payload carries it as a JSON number when the envelope round-tripped
// through storage and as a Go int when it did not, so both are accepted; an
// absent or non-numeric index sorts first and loses the final-stage selection
// to any stage that does carry one.
func dockerfileStageIndex(stage map[string]any) int {
	switch typed := stage["stage_index"].(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	}
	return -1
}
