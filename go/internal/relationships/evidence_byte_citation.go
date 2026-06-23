package relationships

import "strings"

// byteCitation computes the byte-level citation fields for one regex match
// located at [start, end) bytes inside content. It returns a Details fragment
// containing start_line, end_line, byte_offset, and byte_length using 1-based
// inclusive line numbers. Callers must merge the returned map into the Details
// map they pass to matchCatalog via extraDetails.
//
// start and end are byte indices as returned by regexp.(*Regexp).FindAllIndex /
// FindAllStringIndex. start is inclusive, end is exclusive.
//
// When start < 0 or end <= start the function returns nil so callers that lack
// a known byte window can pass -1 without fabricating a citation.
func byteCitation(content string, start, end int) map[string]any {
	if start < 0 || end <= start || end > len(content) {
		return nil
	}
	startLine := 1 + strings.Count(content[:start], "\n")
	endLine := startLine + strings.Count(content[start:end], "\n")
	return map[string]any{
		"start_line":  startLine,
		"end_line":    endLine,
		"byte_offset": start,
		"byte_length": end - start,
	}
}

// mergeCommitSHA adds commit_sha to the given map when sha is non-empty. The
// map is returned for chaining. If m is nil a new map is allocated.
//
// mergeCommitSHA is the single point that writes commit_sha into a Details
// fragment so no caller fabricates a value.
func mergeCommitSHA(m map[string]any, sha string) map[string]any {
	if sha == "" {
		return m
	}
	if m == nil {
		m = make(map[string]any)
	}
	m["commit_sha"] = sha
	return m
}

// envelopeCommitSHA returns the commit_sha payload field from an evidence
// envelope, or an empty string when the field is absent or blank. This is the
// single extraction point so all discover helpers use the same key name.
func envelopeCommitSHA(payload map[string]any) string {
	sha, _ := payload["commit_sha"].(string)
	return strings.TrimSpace(sha)
}
