package skillgen

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// byteCitationPrefix is the comment-line marker for the byte-citation block
// that S2 stamps at the top of every generated skill. The format is stable;
// S3 reads and verifies it.
const byteCitationPrefix = "<!-- eshu:byte-citation "

// ErrInvalidByteCitation is returned by NormalizeByteCitation when the input
// cannot be parsed into a path#start-end anchor.
var ErrInvalidByteCitation = errors.New("invalid byte citation")

// FormatCommentBlock returns the top-of-file comment block S2 stamps into
// every generated skill, one line per citation, sorted and deduplicated
// for deterministic byte output. The block does not end with a trailing
// newline; callers add the trailing newline before writing the file.
//
// Citations are normalized to longhand (path#start-end) before formatting so
// the comment block always carries a fully qualified path.
func FormatCommentBlock(citations []string) string {
	normalized := make([]string, 0, len(citations))
	seen := make(map[string]bool, len(citations))
	for _, c := range citations {
		// Skip empty entries defensively; a fragment that contributes an
		// empty citation is a fragment-authoring bug caught at load time,
		// but the comment-block formatter must not panic on it.
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		normalized = append(normalized, c)
	}
	sort.Strings(normalized)
	lines := make([]string, 0, len(normalized))
	for _, c := range normalized {
		lines = append(lines, byteCitationPrefix+c+" -->")
	}
	return strings.Join(lines, "\n")
}

// NormalizeByteCitation converts a byte-citation value into longhand
// (path#start-end). The shorthand form (#start-end) is resolved against
// fragmentSourcePath; the longhand form is returned unchanged after
// validation.
//
// Returns ErrInvalidByteCitation if the citation is empty, the anchor is
// missing, the line range is malformed, or shorthand cannot be resolved
// because fragmentSourcePath is empty.
func NormalizeByteCitation(citation, fragmentSourcePath string) (string, error) {
	citation = strings.TrimSpace(citation)
	if citation == "" {
		return "", fmt.Errorf("%w: empty", ErrInvalidByteCitation)
	}
	anchorIdx := strings.LastIndex(citation, "#")
	if anchorIdx < 0 {
		return "", fmt.Errorf("%w: missing # in %q", ErrInvalidByteCitation, citation)
	}
	path := strings.TrimSpace(citation[:anchorIdx])
	anchor := strings.TrimSpace(citation[anchorIdx+1:])
	if path == "" {
		// Shorthand #start-end. Resolve against the fragment's own path.
		if strings.TrimSpace(fragmentSourcePath) == "" {
			return "", fmt.Errorf("%w: shorthand without fragment path", ErrInvalidByteCitation)
		}
		path = fragmentSourcePath
	}
	if err := validateAnchor(anchor); err != nil {
		return "", fmt.Errorf("%w: %v in %q", ErrInvalidByteCitation, err, citation)
	}
	return path + "#" + anchor, nil
}

// validateAnchor checks the anchor. Two forms are accepted per the S1
// contract:
//
//   - "N" — a single line, normalized internally to "N-N".
//   - "start-end" — a 1-based inclusive line range; start must be <= end.
//
// Both forms are returned as "start-end" by NormalizeByteCitation so the
// emitted comment block is uniform across fragments.
func validateAnchor(anchor string) error {
	dashIdx := strings.Index(anchor, "-")
	if dashIdx < 0 {
		// Single-line anchor: "N" is equivalent to "N-N".
		if _, err := parsePositiveInt(anchor); err != nil {
			return fmt.Errorf("malformed anchor %q: %w", anchor, err)
		}
		return nil
	}
	if dashIdx == 0 || dashIdx == len(anchor)-1 {
		return fmt.Errorf("malformed anchor %q", anchor)
	}
	startStr := anchor[:dashIdx]
	endStr := anchor[dashIdx+1:]
	start, err := parsePositiveInt(startStr)
	if err != nil {
		return fmt.Errorf("start %q: %w", startStr, err)
	}
	end, err := parsePositiveInt(endStr)
	if err != nil {
		return fmt.Errorf("end %q: %w", endStr, err)
	}
	if start > end {
		return fmt.Errorf("range %d-%d is descending", start, end)
	}
	return nil
}

func parsePositiveInt(s string) (int, error) {
	if s == "" {
		return 0, errors.New("empty integer")
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a positive integer: %q", s)
		}
		n = n*10 + int(r-'0')
	}
	if n == 0 {
		return 0, errors.New("zero is not a positive line number")
	}
	return n, nil
}
