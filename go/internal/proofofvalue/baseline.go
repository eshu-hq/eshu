// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package proofofvalue

import (
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/iacreachability"
)

// BaselineReachability models the verdict an agent reaches with plain text
// search ("grep") and no graph: an artifact is "used" when its name appears as
// a token in any corpus file other than the files that define the artifact
// itself, and "unused" otherwise.
//
// This is a faithful baseline, not a strawman. Grep can only match literal
// text, so it cannot tell a real reference from a name that appears inside a
// templated or interpolated path, and it has no notion of an "ambiguous"
// dynamic reference. The returned verdict is therefore always "used" or
// "unused"; the cases where that disagrees with ground truth are exactly the
// semantic references and dynamic references that text search cannot resolve.
//
// The baseline only searches IaC-relevant files (the same content surface the
// analyzer models references from, per iacreachability.RelevantFile) so the two
// strategies are compared apples-to-apples. Searching unrelated files such as
// README.md would only give the baseline extra chances to find a name and call
// an artifact "used", biasing the comparison in the baseline's favor.
//
// artifactName is the bare artifact name (for example "orphan-cache").
// definingFiles is the set of relative paths, per repo, that constitute the
// artifact definition and must be excluded from the search so that an
// artifact's own files never count as a reference to itself.
func BaselineReachability(filesByRepo map[string][]iacreachability.File, artifactName string, definingFiles map[string]map[string]struct{}) string {
	pattern := tokenPattern(artifactName)
	if pattern == nil {
		return "unused"
	}
	for repoID, files := range filesByRepo {
		excluded := definingFiles[repoID]
		for _, file := range files {
			if !iacreachability.RelevantFile(file.RelativePath) {
				continue
			}
			if _, isDefiner := excluded[file.RelativePath]; isDefiner {
				continue
			}
			if pattern.MatchString(file.Content) {
				return "used"
			}
		}
	}
	return "unused"
}

// tokenPattern compiles a whole-token matcher for an artifact name. Names are
// matched on word-ish boundaries so that "api" does not match inside
// "rapidapi", mirroring how a careful grep with word boundaries behaves. It
// returns nil for an empty name.
func tokenPattern(name string) *regexp.Regexp {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	// Allow common separators around the token: start/end of string or any
	// character that is not a letter, digit, underscore, or hyphen.
	return regexp.MustCompile(`(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(name) + `([^A-Za-z0-9_-]|$)`)
}
